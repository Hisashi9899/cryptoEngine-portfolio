package main

import (
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	vt "github.com/cryptoEngine/core/usecase/trading/vt"
)

// TestParseOrderbookRow は capture CSV 行の解釈を検証する（出来高復元付き）。
func TestParseOrderbookRow(t *testing.T) {
	ts, mid, bid, ask, wall, wallOK, bid3, ask3, sizeOK := parseOrderbookRow(`1700000000000,100.5,0.1,[{"price":100.4,"volume":5}],[{"price":100.6,"volume":7}]`)
	if ts != 1700000000000 || mid != 100.5 || bid != 5 || ask != 7 {
		t.Fatalf("got %v %v %v %v", ts, mid, bid, ask)
	}
	if !wallOK || wall >= 0 {
		t.Fatalf("ask wall must read negative, got %v %v", wall, wallOK)
	}
	if !sizeOK || bid3 != 5 || ask3 != 7 {
		t.Fatalf("wall sizes must read top3 sums, got %v %v %v", bid3, ask3, sizeOK)
	}
	// ヘッダ行はゼロ。
	if ts, _, _, _, _, _, _, _, _ := parseOrderbookRow(`timestamp,midPrice,spread,bids,asks`); ts != 0 {
		t.Fatalf("header must parse zero, got %v", ts)
	}
	// 空配列はゼロ出来高で足は作る（板厚は不明）。
	ts, mid, bid, ask, _, wallOK, _, _, sizeOK = parseOrderbookRow(`1700000000000,100.5,0.1,[],[]`)
	if ts == 0 || mid == 0 || bid != 0 || ask != 0 {
		t.Fatalf("empty books must keep ts/mid: %v %v %v %v", ts, mid, bid, ask)
	}
	if wallOK || sizeOK {
		t.Fatal("empty books must leave wall unknown")
	}
	// 壊れ行は全ゼロ。
	if ts, mid, _, _, _, _, _, _, _ := parseOrderbookRow(`garbage`); ts != 0 || mid != 0 {
		t.Fatal("garbage must parse zero")
	}
	if got := sumVolumes(`[{"price":1,"volume":2},{"price":1,"volume":3}]`); got != 5 {
		t.Fatalf("sumVolumes = %v, want 5", got)
	}
	// 上位3段の板厚（売り厚で負。4段目の深い買いは数えない）。
	if w, ok := topWallJSON(`[{"price":100.4,"volume":5},{"price":100.3,"volume":5},{"price":100.2,"volume":5},{"price":100.1,"volume":100}]`, `[{"price":100.6,"volume":70},{"price":100.7,"volume":70}]`); !ok || w >= -0.5 {
		t.Fatalf("top3 sell wall must read firmly negative, got %v %v", w, ok)
	}
}

// TestPaperExcursion は MFE/MAE 比率の符号を検証する（売り・買い対称）。
func TestPaperExcursion(t *testing.T) {
	buy := &vt.OpenPos{Side: vt.Buy, EntryPrice: 100}
	if mfe, mae := paperExcursion(buy, 105, 99); mfe != 0.05 || mae != -0.01 {
		t.Fatalf("buy mfe/mae = %v/%v, want 0.05/-0.01", mfe, mae)
	}
	sell := &vt.OpenPos{Side: vt.Sell, EntryPrice: 100}
	if mfe, mae := paperExcursion(sell, 102, 95); mfe != 0.05 || mae != -0.02 {
		t.Fatalf("sell mfe/mae = %v/%v, want 0.05/-0.02", mfe, mae)
	}
	if mfe, mae := paperExcursion(nil, 105, 99); mfe != 0 || mae != 0 {
		t.Fatalf("nil must give zeros, got %v/%v", mfe, mae)
	}
	bad := &vt.OpenPos{Side: vt.Buy, EntryPrice: 0}
	if mfe, mae := paperExcursion(bad, 105, 99); mfe != 0 || mae != 0 {
		t.Fatalf("bad entry must give zeros, got %v/%v", mfe, mae)
	}
}

// TestParseBarsList は --bars の解釈を検証する。
func TestParseBarsList(t *testing.T) {
	if got := parseBarsList(""); len(got) != 1 || got[0] != 300 {
		t.Fatalf("empty must default {300}, got %v", got)
	}
	if got := parseBarsList("300"); len(got) != 1 || got[0] != 300 {
		t.Fatalf("single must work, got %v", got)
	}
	got := parseBarsList("3600,60,300,60,45,abc")
	if len(got) != 3 || got[0] != 60 || got[1] != 300 || got[2] != 3600 {
		t.Fatalf("must dedupe/sort/filter, got %v", got)
	}
	if got := parseBarsList("45,abc"); len(got) != 0 {
		t.Fatalf("all-invalid must be empty, got %v", got)
	}
}

// TestMultiTFEnginesIndependent は同一ティックを食う複数足種エンジンが
// 独立に足を作ることを検証する。
func TestMultiTFEnginesIndependent(t *testing.T) {
	mk := func(bars int64) *vt.Engine {
		cfg := vt.DefaultConfig()
		cfg.BarsSec = bars
		cfg.TrendTFSec = 0
		return vt.NewEngine(cfg)
	}
	e60, e300 := mk(60), mk(300)
	var bars60, bars300 int
	for i := 0; i < 400; i++ {
		ts := int64(1_700_000_000_000 + i*1000)
		if _, bar := e60.Observe("TST", ts, 100, 1, 1); bar != nil {
			bars60++
		}
		if _, bar := e300.Observe("TST", ts, 100, 1, 1); bar != nil {
			bars300++
		}
	}
	// 400秒で1分足は6本、5分足は1本確定する。
	if bars60 != 6 {
		t.Fatalf("60s bars = %d, want 6", bars60)
	}
	if bars300 != 1 {
		t.Fatalf("300s bars = %d, want 1", bars300)
	}
}

// TestVtPaperQtyPositive は paper 数量が正になることの回帰テスト。
// 2026-09-22 実測で riskFraction 二重適用により全銘柄 qty:0 だった。
func TestVtPaperQtyPositive(t *testing.T) {
	cases := []struct {
		symbol string
		price  float64
		conf   float64
	}{
		{"BTC", 12000000, 0.62},
		{"XRP", 220, 0.55},
		{"XLM", 33.5, 0.65},
		{"BCH", 41821, 0.62},
		{"LTC", 9683, 0.55},
	}
	for _, c := range cases {
		if got := vtPaperQty(c.symbol, c.price, c.conf, 100000, 0.2, 0); got <= 0 {
			t.Fatalf("vtPaperQty(%s) = %v, want > 0", c.symbol, got)
		}
	}
	if got := vtPaperQty("UNKNOWN", 100, 0.6, 100000, 0.2, 0); got != 0 {
		t.Fatalf("unknown symbol must be 0, got %v", got)
	}
	if got := vtPaperQty("BTC", 0, 0.6, 100000, 0.2, 0); got != 0 {
		t.Fatalf("zero price must be 0, got %v", got)
	}
}

// TestVtPaperQtySpread はスプレッド悪化で数量が減速することを検証する。
func TestVtPaperQtySpread(t *testing.T) {
	tight := vtPaperQty("XRP", 220, 0.8, 100000, 0.2, 0)
	wide := vtPaperQty("XRP", 220, 0.8, 100000, 0.2, 0.002)
	if tight <= 0 || wide <= 0 {
		t.Fatalf("both must be positive: tight=%v wide=%v", tight, wide)
	}
	if !(wide < tight) {
		t.Fatalf("wide spread must reduce qty: tight=%v wide=%v", tight, wide)
	}
	if got := vtPaperQty("XRP", 220, 0.8, 100000, 0.2, -0.5); got != tight {
		t.Fatalf("negative spread must clamp to 0: got %v want %v", got, tight)
	}
}

// TestVtOpenCarryover は退避→再開の往復を検証する。
func TestVtOpenCarryover(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UnixMilli()
	open := &vt.OpenPos{Side: vt.Buy, EntryPrice: 100, EntryTime: now - 1000, Volume: 0.5,
		EntryRegime: "trend_up", EntryPattern: "morning_star",
		EntryRSI: 40, EntryPB: 0.3, EntryHist: 0.1, EntryConf: 0.65}
	vtSaveOpen(dir, "TST", 300, open)
	got := vtLoadOpen(dir, "TST", 300, now)
	if got == nil {
		t.Fatal("expected resumed open, got nil")
	}
	if got.EntryPrice != 100 || got.Volume != 0.5 || got.EntryRegime != "trend_up" ||
		got.EntryPattern != "morning_star" || got.EntryConf != 0.65 {
		t.Fatalf("resumed open corrupted: %+v", got)
	}
	// 古い建玉は再開しない。
	ancient := &vt.OpenPos{Side: vt.Buy, EntryPrice: 100, EntryTime: now - 8*24*3600*1000, Volume: 0.5}
	vtSaveOpen(dir, "OLD", 300, ancient)
	if got := vtLoadOpen(dir, "OLD", 300, now); got != nil {
		t.Fatalf("stale open must not resume: %+v", got)
	}
	// nil 保存でファイルが消える。
	vtSaveOpen(dir, "TST", 300, nil)
	if _, err := os.Stat(filepath.Join(dir, "vt_open_TST_300.json")); !os.IsNotExist(err) {
		t.Fatal("nil save must remove state file")
	}
	if got := vtLoadOpen(dir, "TST", 300, now); got != nil {
		t.Fatal("removed state must load nil")
	}
}

// emitReplayBar は1本の確定足を4 tick の CSV 行にする（O/H/L/C）。
func emitReplayBar(lines *[]string, startMs int64, o, h, l, c float64) {
	prices := []struct {
		ts int64
		p  float64
	}{
		{startMs, o}, {startMs + 1000, h}, {startMs + 2000, l}, {startMs + 299000, c},
	}
	for _, tp := range prices {
		*lines = append(*lines, fmt.Sprintf("%d,%.4f,0.1,[{\"price\":%.4f,\"volume\":1}],[{\"price\":%.4f,\"volume\":1}]",
			tp.ts, tp.p, tp.p-0.05, tp.p+0.05))
	}
}

// TestReadReplayDay は .csv→.csv.gz フォールバックとテープ結合を検証する。
func TestReadReplayDay(t *testing.T) {
	dir := t.TempDir()
	day := "20260920"
	var lines []string
	lines = append(lines, "timestamp,midPrice,spread,bids,asks")
	for i := 0; i < 3; i++ {
		ts := int64(1758326400000 + i*5000)
		lines = append(lines, fmt.Sprintf("%d,%.1f,0.2,[{\"price\":%.1f,\"volume\":2}],[{\"price\":%.1f,\"volume\":3}]",
			ts, 100+float64(i), 100+float64(i)-0.1, 100+float64(i)+0.1))
	}
	if err := os.WriteFile(filepath.Join(dir, "orderbook_TST_"+day+".csv"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tape := "timestamp,price,qty,side\n1758326401000,100.5,0.5,buy\n1758326402000,100.6,0.3,sell\n"
	if err := os.WriteFile(filepath.Join(dir, "trade_tape_TST_"+day+".csv"), []byte(tape), 0o644); err != nil {
		t.Fatal(err)
	}
	ticks, tapes, ok := readReplayDay(dir, "TST", day)
	if !ok || len(ticks) != 3 || len(tapes) != 2 {
		t.Fatalf("csv+tape: ok=%v ticks=%d tapes=%d", ok, len(ticks), len(tapes))
	}
	if ticks[0].spreadRatio != 0.002 {
		t.Fatalf("spread ratio = %v, want 0.002", ticks[0].spreadRatio)
	}
	// .csv を消すと .gz にフォールバックする。
	gzDay := "20260921"
	var gzLines []string
	gzLines = append(gzLines, "timestamp,midPrice,spread,bids,asks")
	gzLines = append(gzLines, "1758412800000,101.0,0.1,[{\"price\":100.9,\"volume\":1}],[{\"price\":101.1,\"volume\":1}]")
	raw := strings.Join(gzLines, "\n") + "\n"
	gf, err := os.Create(filepath.Join(dir, "orderbook_TST_"+gzDay+".csv.gz"))
	if err != nil {
		t.Fatal(err)
	}
	w := gzip.NewWriter(gf)
	if _, err := w.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gf.Close(); err != nil {
		t.Fatal(err)
	}
	ticks, _, ok = readReplayDay(dir, "TST", gzDay)
	if !ok || len(ticks) != 1 || ticks[0].mid != 101.0 {
		t.Fatalf("gz fallback: ok=%v ticks=%v", ok, ticks)
	}
	if _, _, ok := readReplayDay(dir, "NOSUCH", day); ok {
		t.Fatal("missing day must fail")
	}
}

// TestReplaySymbol は保存板のリプレイで入口・出口が記録されることを検証する。
// 上昇土台→明星→急落の形で、集計まで一気通貫する。
// 急落の第1波では反転離脱（signal）が SL（-2%）より先に発火すること。
// 放置欠陥の修正前はレンジ・許可外ゲートで反転を見逃し stop_loss まで
// 引きずった。修正後は微損のうちに signal で閉じる。
func TestReplaySymbol(t *testing.T) {
	dir := t.TempDir()
	day := "20260920"
	var lines []string
	lines = append(lines, "timestamp,midPrice,spread,bids,asks")
	base := int64(1758326400000)
	slot := base
	p := 100.0
	// テープ供給：買い一辺倒で CVD を買い強にし、肯定投票のテープ側を通す。
	// 価格は足終値に連動させ、恒等価格による吸収誤検出を避ける。
	// 実稼働の replay は trade_tape CSV を併読するため、fixture も同様にする。
	tape := []string{"timestamp,price,qty,side"}
	emit := func(o, h, l, c float64) {
		emitReplayBar(&lines, slot, o, h, l, c)
		tape = append(tape, fmt.Sprintf("%d,%.4f,1.0,buy", slot, c))
		slot += 300000
		p = c
	}
	for i := 0; i < 60; i++ {
		d := 0.6
		if i%3 == 2 {
			d = -0.5
		}
		o := p
		c := o + d
		hi, lo := c+0.05, o-0.05
		if d < 0 {
			hi, lo = o+0.05, c-0.05
		}
		emit(o, hi, lo, c)
	}
	aO := p
	aC := aO - 2.0
	emit(aO, aO+0.05, aC-0.05, aC)
	bO := aC
	bC := bO + 0.1
	emit(bO, bC+0.2, bO-0.2, bC)
	cO := bC - 0.2
	cC := aO + 0.3
	emit(cO, cC+0.05, cO-0.05, cC)
	for i := 0; i < 8; i++ {
		o := p
		c := o - 0.8
		emit(o, o+0.05, c-0.05, c)
	}
	if err := os.WriteFile(filepath.Join(dir, "orderbook_BTC_"+day+".csv"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "trade_tape_BTC_"+day+".csv"), []byte(strings.Join(tape, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	recs, files, err := replaySymbol(dir, "BTC", day, day, 300, 0, 0, 0.015, 0.008, 100000, 0.2, nil, nil, 0, 0, 0.05, true, false, 0, 0, 0, 0, 0, 0, false, 0, 0, 0, 0, 0, nil, nil, nil, 0, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if files != 1 {
		t.Fatalf("files = %d, want 1", files)
	}
	var entry, exit *vtSignalRecord
	for i := range recs {
		r := &recs[i]
		if r.Pattern == "morning_star" && (r.Type == "buy" || r.Type == "sell") {
			entry = r
		}
		// 明星 entry（114.3付近）の後の急落を signal で閉じる想定。
		// entry より後の最初の exit を拾う。
		if entry != nil && exit == nil && r.Pattern == "exit" && r.Timestamp > entry.Timestamp {
			exit = r
		}
	}
	if entry == nil {
		t.Fatalf("expected morning_star entry, got %d recs", len(recs))
	}
	if exit == nil {
		t.Fatalf("expected exit after morning_star, got %+v", recs)
	}
	if exit.Reason != "signal" {
		t.Fatalf("crash must exit by reversal signal, got %q (price=%v)", exit.Reason, exit.Price)
	}
	// MFE/MAE 記録：急落離脱では逆行（MAE）が負に残る。
	if exit.MAE >= 0 {
		t.Fatalf("crash exit must record negative MAE, got %v", exit.MAE)
	}
	if drop := (entry.Price - exit.Price) / entry.Price; drop < 0 || drop >= 0.02 {
		t.Fatalf("reversal exit must precede stop_loss (drop=%.4f)", drop)
	}
	sum := summarizeVtRecords(recs, "replay", 0, files, "none")
	if sum.Total.Pairs < 1 {
		t.Fatalf("expected >=1 pair, got %+v", sum.Total)
	}
}

// TestGridFileSeparation はグリッド記録が trend とファイル分離されることを検証する。
func TestGridFileSeparation(t *testing.T) {
	dir := t.TempDir()
	st := &vtTFState{barsSec: 300}
	if err := st.rotate(dir, "TST", "20260920"); err != nil {
		t.Fatal(err)
	}
	defer st.sigFile.Close()
	defer st.gridFile.Close()
	rec := vtSignalRecord{Timestamp: 1, Symbol: "TST", Type: "buy", Price: 100,
		Pattern: "grid_range_buy", BarsSec: 300}
	if err := st.writeGridRec(rec); err != nil {
		t.Fatal(err)
	}
	if st.sigFile == st.gridFile {
		t.Fatal("grid file must differ from trend file")
	}
	if got := vtSignalsPath(dir, "TST", 300, "20260920"); got == gridSignalsPath(dir, "TST", 300, "20260920") {
		t.Fatal("grid path must differ from trend path")
	}
}
