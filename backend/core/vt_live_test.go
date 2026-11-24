package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cryptoEngine/core/domain"
	"github.com/cryptoEngine/core/usecase/trading"
	vt "github.com/cryptoEngine/core/usecase/trading/vt"
)

func mkLiveCand(sym string, conf float64) vtLiveCandidate {
	return vtLiveCandidate{
		symbol: sym,
		sig:    &vt.Signal{Symbol: sym, Type: vt.Buy, Price: 100, Confidence: conf, PercentB: 0.8},
		price:  100,
		qty:    1,
	}
}

// TestPickTopKOrder は効率順の複数選択を検証する。
func TestPickTopKOrder(t *testing.T) {
	pending := []vtLiveCandidate{mkLiveCand("LOW", 0.55), mkLiveCand("HIGH", 0.65), mkLiveCand("MID", 0.60)}
	picks, reason := pickTopK(pending, 5, map[string]bool{}, 3, 0, 0, 2000)
	if reason != "" || len(picks) != 3 {
		t.Fatalf("expected 3 picks, got %d (%s)", len(picks), reason)
	}
	if picks[0].symbol != "HIGH" || picks[1].symbol != "MID" || picks[2].symbol != "LOW" {
		t.Fatalf("bad order: %s %s %s", picks[0].symbol, picks[1].symbol, picks[2].symbol)
	}
	// 空き1枠では最上位のみ。
	picks, _ = pickTopK(pending, 5, map[string]bool{"X": true}, 2, 1, 0, 2000)
	if len(picks) != 1 || picks[0].symbol != "HIGH" {
		t.Fatalf("expected HIGH only, got %+v", picks)
	}
	// 建玉済み・重複銘柄は飛ばす。
	dupes := []vtLiveCandidate{mkLiveCand("A", 0.65), mkLiveCand("A", 0.64), mkLiveCand("B", 0.60)}
	picks, _ = pickTopK(dupes, 5, map[string]bool{"B": true}, 3, 1, 0, 2000)
	if len(picks) != 1 || picks[0].symbol != "A" {
		t.Fatalf("expected A only, got %+v", picks)
	}
}

// TestPickTopKCaps は上限・損切りの裁定を検証する。
func TestPickTopKCaps(t *testing.T) {
	pending := []vtLiveCandidate{mkLiveCand("A", 0.6)}
	if _, reason := pickTopK(pending, 5, map[string]bool{}, 1, 1, 0, 2000); reason != "max positions" {
		t.Fatalf("max positions must reject, got %s", reason)
	}
	if _, reason := pickTopK(pending, 5, map[string]bool{}, 3, 0, -2500, 2000); reason != "daily loss limit" {
		t.Fatalf("daily loss must reject, got %s", reason)
	}
	if _, reason := pickTopK(nil, 5, map[string]bool{}, 3, 0, 0, 2000); reason != "no candidates" {
		t.Fatalf("empty must reject, got %s", reason)
	}
	if _, reason := pickTopK(pending, 5, map[string]bool{"A": true}, 3, 1, 0, 2000); reason != "all candidates already open" {
		t.Fatalf("all-open must reject, got %s", reason)
	}
}

// TestPickTopKMomentum は方向一致モメンタムの上乗せを検証する。
// 確信度は低いが下降率の高いショートが、確信度のみの候補を上回る。
func TestPickTopKMomentum(t *testing.T) {
	mk := func(sym string, conf, pb, hist float64) vtLiveCandidate {
		return vtLiveCandidate{
			symbol: sym,
			sig:    &vt.Signal{Symbol: sym, Type: vt.Sell, Price: 100, Confidence: conf, PercentB: pb, MACDHist: hist},
			price:  100, qty: 1,
		}
	}
	// A: conf高・モメンタムなし vs B: conf低・下降強。Bが勝つ。
	pending := []vtLiveCandidate{mk("A", 0.60, 0.5, 0), mk("B", 0.58, 0.5, -8)}
	picks, reason := pickTopK(pending, 5, map[string]bool{}, 3, 0, 0, 2000)
	if reason != "" || len(picks) != 2 || picks[0].symbol != "B" {
		t.Fatalf("momentum must outrank: %+v (%s)", picks, reason)
	}
	// 逆行モメンタムは上乗せなし。
	pending = []vtLiveCandidate{mk("A", 0.60, 0.5, 0), mk("C", 0.58, 0.5, 8)}
	picks, _ = pickTopK(pending, 5, map[string]bool{}, 3, 0, 0, 2000)
	if len(picks) != 2 || picks[0].symbol != "A" {
		t.Fatalf("counter momentum must not boost: %+v", picks)
	}
}

// TestVtLiveEnabled は --live 単独指定の有効化を検証する（回帰）。
// parseKeyValues は --k=v 形式のみ扱うため、素の --live は見落とされていた。
func TestVtLiveEnabled(t *testing.T) {
	if !vtLiveEnabled([]string{"--live"}, map[string]string{}) {
		t.Fatal("bare --live must enable")
	}
	if !vtLiveEnabled([]string{"--live=true"}, map[string]string{"live": "true"}) {
		t.Fatal("--live=true must enable")
	}
	if vtLiveEnabled([]string{"--live=false"}, map[string]string{"live": "false"}) {
		t.Fatal("--live=false must not enable")
	}
	if vtLiveEnabled([]string{"--seconds=60"}, map[string]string{}) {
		t.Fatal("absent flag must not enable")
	}
}

func TestParseBlockSymbols(t *testing.T) {
	got := parseBlockSymbols("sui, WILD,,BTC")
	if len(got) != 3 || !got["SUI"] || !got["WILD"] || !got["BTC"] {
		t.Fatalf("bad parse: %v", got)
	}
	if len(parseBlockSymbols("")) != 0 {
		t.Fatal("empty must be empty")
	}
}

func TestLoserBlocklist(t *testing.T) {
	sum := vtScoreSummary{BySymbol: map[string]vtScoreStat{
		"SUI":  {Pairs: 13, Wins: 2, PnL: -508},
		"WILD": {Pairs: 7, Wins: 1, PnL: -753},
		"ADA":  {Pairs: 25, Wins: 16, PnL: 152},
		"THIN": {Pairs: 2, Wins: 0, PnL: -50},
	}}
	got := loserBlocklist(sum, 5)
	if !got["SUI"] || !got["WILD"] {
		t.Fatalf("losers must block: %v", got)
	}
	if got["ADA"] || got["THIN"] {
		t.Fatalf("winner/thin must not block: %v", got)
	}
}

func writeBlockRecords(t *testing.T, dir, sym string, n int, entry, exit float64, base int64) {
	t.Helper()
	var lines []string
	for i := 0; i < n; i++ {
		ts := base + int64(i*2)*60000
		in, _ := json.Marshal(vtSignalRecord{Timestamp: ts, Symbol: sym, Type: "buy",
			Price: entry, Pattern: "morning_star", Regime: "trend_up", Confidence: 0.65, Qty: 1, BarsSec: 300})
		out, _ := json.Marshal(vtSignalRecord{Timestamp: ts + 60000, Symbol: sym, Type: "sell",
			Price: exit, Pattern: "exit", Reason: "signal", Qty: 1, BarsSec: 300})
		lines = append(lines, string(in), string(out))
	}
	if err := os.WriteFile(filepath.Join(dir, "vt_signals_"+sym+"_300_20260923.jsonl"),
		[]byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshBlocklist(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-time.Hour).UnixMilli()
	writeBlockRecords(t, dir, "LOSER", 5, 100, 90, base)
	writeBlockRecords(t, dir, "WINNER", 5, 100, 110, base)
	writeBlockRecords(t, dir, "THIN", 2, 100, 90, base)
	got := refreshBlocklist(dir, 7, 5, parseBlockSymbols("manual"), parseBlockSymbols(""))
	if !got["LOSER"] || !got["MANUAL"] {
		t.Fatalf("loser+manual must block: %v", got)
	}
	if got["WINNER"] || got["THIN"] {
		t.Fatalf("winner/thin must not block: %v", got)
	}
	// 許可は除外に勝つ。
	allowed := refreshBlocklist(dir, 7, 5, parseBlockSymbols(""), parseBlockSymbols("loser"))
	if allowed["LOSER"] {
		t.Fatalf("allow must beat auto-block: %v", allowed)
	}
	// 記録なしでは手動分のみ。
	empty := refreshBlocklist(t.TempDir(), 7, 5, parseBlockSymbols(""), parseBlockSymbols(""))
	if len(empty) != 0 {
		t.Fatalf("empty dir must block nothing: %v", empty)
	}
}

func TestMergeDailyPnl(t *testing.T) { // 5銘柄ファイルが同じ全体値でも1倍に統合される（5倍加算の回帰）。
	daily := map[string]float64{}
	for i := 0; i < 5; i++ {
		mergeDailyPnl(daily, "20260923", -2.2)
	}
	if daily["20260923"] != -2.2 {
		t.Fatalf("must not multiply: %v", daily)
	}
	mergeDailyPnl(daily, "20260923", -5.0)
	if daily["20260923"] != -2.2 {
		t.Fatalf("must keep max: %v", daily)
	}
}

func TestScaleDailyLoss(t *testing.T) {
	if got := scaleDailyLoss(5000, 0.1, 500); got != 500 {
		t.Fatalf("5000*0.1 = %v, want 500", got)
	}
	if got := scaleDailyLoss(10000, 0.1, 500); got != 1000 {
		t.Fatalf("10000*0.1 = %v, want 1000", got)
	}
	if got := scaleDailyLoss(5000, 0, 500); got != 500 {
		t.Fatalf("ratio 0 must keep fallback, got %v", got)
	}
	if got := scaleDailyLoss(0, 0.1, 500); got != 500 {
		t.Fatalf("zero balance must keep fallback, got %v", got)
	}
}

func TestVtMarginEnabled(t *testing.T) {
	if !vtMarginEnabled([]string{"--margin"}, map[string]string{}) {
		t.Fatal("bare --margin must enable")
	}
	if !vtMarginEnabled([]string{}, map[string]string{"margin": "true"}) {
		t.Fatal("--margin=true must enable")
	}
	if vtMarginEnabled([]string{}, map[string]string{"margin": "false"}) {
		t.Fatal("--margin=false must not enable")
	}
	if vtMarginEnabled([]string{"--seconds=60"}, map[string]string{}) {
		t.Fatal("absent flag must not enable")
	}
}

func TestBalanceChanged(t *testing.T) {
	if !balanceChanged(5000, 15000) {
		t.Fatal("deposit must trigger")
	}
	if !balanceChanged(5000, 4000) {
		t.Fatal("withdrawal must trigger")
	}
	if balanceChanged(5000, 5049) {
		t.Fatal("sub-1% drift must not trigger")
	}
	if !balanceChanged(5000, 5050) {
		t.Fatal("exactly 1% must trigger")
	}
	if !balanceChanged(0, 100) {
		t.Fatal("zero to positive must trigger")
	}
	if balanceChanged(0, 0) {
		t.Fatal("zero to zero must not trigger")
	}
}

func TestMaintenanceWindows(t *testing.T) {
	jst := time.FixedZone("JST", 9*60*60)
	at := func(month time.Month, day, h, m int) time.Time {
		// 2026-09-26 は土曜。日付をずらして曜日を変える。
		return time.Date(2026, month, day, h, m, 0, 0, jst)
	}
	sat := at(time.September, 26, 0, 0)
	if sat.Weekday() != time.Saturday {
		t.Fatalf("test premise broken: %v", sat.Weekday())
	}
	cases := []struct {
		name       string
		t          time.Time
		pre, in    bool
		blocked30  bool
		blockedOff bool
	}{
		{"sat 08:29", at(time.September, 26, 8, 29), false, false, false, false},
		{"sat 08:30", at(time.September, 26, 8, 30), true, false, true, false},
		{"sat 08:59", at(time.September, 26, 8, 59), true, false, true, false},
		{"sat 09:00", at(time.September, 26, 9, 0), false, true, true, true},
		{"sat 10:59", at(time.September, 26, 10, 59), false, true, true, true},
		{"sat 11:00", at(time.September, 26, 11, 0), false, false, false, false},
		{"fri 08:45", at(time.September, 25, 8, 45), false, false, false, false},
		{"sun 10:00", at(time.September, 27, 10, 0), false, false, false, false},
	}
	for _, c := range cases {
		if got := inMaintPreWindow(c.t, 30); got != c.pre {
			t.Errorf("%s: pre=%v, want %v", c.name, got, c.pre)
		}
		if got := inMaintWindowT(c.t); got != c.in {
			t.Errorf("%s: in=%v, want %v", c.name, got, c.in)
		}
		if got := maintBlocked(c.t, 30); got != c.blocked30 {
			t.Errorf("%s: blocked30=%v, want %v", c.name, got, c.blocked30)
		}
		if got := maintBlocked(c.t, 0); got != c.blockedOff {
			t.Errorf("%s: blockedOff=%v, want %v", c.name, got, c.blockedOff)
		}
	}
	// UTC 指定でも JST 判定になること（土曜 00:00 UTC = 土曜 09:00 JST）。
	if !inMaintWindowT(time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("00:00 UTC Saturday must be in window (09:00 JST)")
	}
}

func TestComputeBlocked(t *testing.T) {
	long := map[string]*patDirStat{
		"morning_star": {pairs: 6, wins: 0, pnl: -12},
		"bb_bounce":    {pairs: 6, wins: 4, pnl: 20},
		"thin_pat":     {pairs: 3, wins: 0, pnl: -5},
	}
	if got := computeBlocked(long, 5); len(got) != 1 || got[0] != "morning_star" {
		t.Fatalf("must block only proven loser, got %v", got)
	}
	short := map[string]*patDirStat{
		"evening_star": {pairs: 8, wins: 3, pnl: -7},
		"bb_breakdown": {pairs: 8, wins: 5, pnl: 30},
	}
	if got := computeBlocked(short, 5); len(got) != 1 || got[0] != "evening_star" {
		t.Fatalf("must block evening_star, got %v", got)
	}
	// 標本不足・勝ち越しは残す。
	if got := computeBlocked(map[string]*patDirStat{"x": {pairs: 2, wins: 0, pnl: -1}}, 5); len(got) != 0 {
		t.Fatalf("thin series must stay, got %v", got)
	}
}

func TestPairDirectedStats(t *testing.T) {
	recs := []vtSignalRecord{
		{Timestamp: 1, Symbol: "TST", Type: "buy", Price: 100, Pattern: "morning_star", Regime: "trend_up", Qty: 1, BarsSec: 300},
		{Timestamp: 2, Symbol: "TST", Type: "sell", Price: 90, Reason: "signal", Pattern: "exit", Qty: 1, BarsSec: 300},
		{Timestamp: 3, Symbol: "TST", Type: "sell", Price: 100, Pattern: "bb_breakdown", Regime: "trend_down", Qty: 1, BarsSec: 300},
		{Timestamp: 4, Symbol: "TST", Type: "buy", Price: 90, Reason: "signal", Pattern: "exit", Qty: 1, BarsSec: 300},
	}
	stats := pairDirectedStats(recs, 0)
	if s := stats["buy\x00morning_star\x00trend_up"]; s == nil || s.pairs != 1 || s.wins != 0 {
		t.Fatalf("bad long stat: %+v", s)
	}
	if s := stats["sell\x00bb_breakdown\x00trend_down"]; s == nil || s.pairs != 1 || s.wins != 1 {
		t.Fatalf("bad short stat: %+v", s)
	}
	long, short := splitDirected(stats)
	if len(long) != 1 || len(short) != 1 {
		t.Fatalf("split must separate directions: %d/%d", len(long), len(short))
	}
}

func TestComputeAllowed(t *testing.T) {
	// 手数料込みPF>1.2・標本十分のセルのみ許可する。
	stats := map[string]*patDirStat{
		"buy\x00pullback_to_bbmid\x00trend_up": {pairs: 6, wins: 4, pnl: 100, grossWin: 150, grossLoss: 50},
		"buy\x00bb_breakout_up\x00trend_up":    {pairs: 10, wins: 3, pnl: -50, grossWin: 40, grossLoss: 90},
		"buy\x00thin_pat\x00trend_up":          {pairs: 2, wins: 2, pnl: 50, grossWin: 50, grossLoss: 0},
	}
	got := computeAllowed(stats, 5, 1.2)
	if len(got) != 1 || got[0].Pattern != "pullback_to_bbmid" || got[0].Regime != "trend_up" || got[0].Side != "buy" {
		t.Fatalf("must allow only proven cell, got %+v", got)
	}
}

func TestLoadPatternDeny(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vt_pattern_allow.json")
	doc := `{"date":"20260926","days":14,"bars_sec":300,"min_pairs":5,"blocked_long":["morning_star"],"blocked_short":[]}`
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	ld, sd, err := loadPatternDeny(path)
	if err != nil || len(ld) != 1 || ld[0] != "morning_star" || len(sd) != 0 {
		t.Fatalf("bad load: %v %v %v", ld, sd, err)
	}
	if _, _, err := loadPatternDeny(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("missing file must error")
	}
}

func TestUnionPatterns(t *testing.T) {
	// 週次＋日次の和集合。重複・空を除きソートする。
	got := unionPatterns([]string{"bb_bounce", "morning_star"}, []string{"morning_star", "", "bb_breakout_up"})
	if len(got) != 3 || got[0] != "bb_bounce" || got[1] != "bb_breakout_up" || got[2] != "morning_star" {
		t.Fatalf("bad union: %v", got)
	}
	if got := unionPatterns(nil, nil); len(got) != 0 {
		t.Fatalf("empty union must stay empty: %v", got)
	}
}

func TestPickCloseSide(t *testing.T) {
	mkpos := func(id int64, side, size, price string) trading.GMOOpenPosition {
		return trading.GMOOpenPosition{PositionID: id, Symbol: "BCH_JPY", Side: side, Size: size, Price: price}
	}
	sells := []trading.GMOOpenPosition{
		mkpos(1, "SELL", "0.1", "53000"),
		mkpos(2, "SELL", "0.1", "53010"),
		mkpos(3, "SELL", "0.1", "53020"),
	}
	// 自動判定＋全量＋VWAP（浮動小数点のため誤差許容で比較）。
	side, qty, vwap, err := pickCloseSide(sells, "", 0)
	if err != nil || side != domain.TradeSell || qty < 0.299 || qty > 0.301 {
		t.Fatalf("auto all: %v %v %v", side, qty, err)
	}
	if vwap < 53009.99 || vwap > 53010.01 {
		t.Fatalf("bad vwap: %v", vwap)
	}
	// 数量指定は切り詰め。
	if _, qty, _, err := pickCloseSide(sells, "", 0.2); err != nil || qty < 0.199 || qty > 0.201 {
		t.Fatalf("capped qty: %v %v", qty, err)
	}
	// 両建ては明示を要求。
	both := append(append([]trading.GMOOpenPosition{}, sells...),
		mkpos(9, "BUY", "0.1", "53000"))
	if _, _, _, err := pickCloseSide(both, "", 0); err == nil {
		t.Fatal("both sides must require explicit side")
	}
	// 建玉なしはエラー。
	if _, _, _, err := pickCloseSide(nil, "", 0); err == nil {
		t.Fatal("empty must fail")
	}
	// 存在しない side はエラー。
	if _, _, _, err := pickCloseSide(sells, "buy", 0); err == nil {
		t.Fatal("missing side must fail")
	}
}

func TestEntryAllowed(t *testing.T) {
	mk := func(typ vt.Side, conf, rsi float64) *vt.Signal {
		return &vt.Signal{Symbol: "TST", Type: typ, Price: 100, Confidence: conf, RSI: rsi}
	}
	// 無効時は全て通す（従来動作）。
	if ok, _ := entryAllowed(mk(vt.Buy, 0.5, 50), 0, 0); !ok {
		t.Fatal("disabled gates must pass")
	}
	if ok, _ := entryAllowed(mk(vt.Sell, 0.5, 20), 0, 0); !ok {
		t.Fatal("disabled gates must pass sells")
	}
	// 確信度フロア：未満は落とし、境界は通す。
	if ok, r := entryAllowed(mk(vt.Buy, 0.57, 60), 0.58, 0); ok {
		t.Fatalf("low confidence must hold, got %q", r)
	}
	if ok, _ := entryAllowed(mk(vt.Buy, 0.58, 60), 0.58, 0); !ok {
		t.Fatal("boundary confidence must pass")
	}
	if ok, _ := entryAllowed(mk(vt.Sell, 0.7, 60), 0.58, 35); !ok {
		t.Fatal("high confidence sell must pass")
	}
	// 売られ過ぎショート：下限未満は落とし、境界は通す。ロングは無視。
	if ok, r := entryAllowed(mk(vt.Sell, 0.7, 34.9), 0, 35); ok {
		t.Fatalf("oversold short must hold, got %q", r)
	}
	if ok, _ := entryAllowed(mk(vt.Sell, 0.7, 35.0), 0, 35); !ok {
		t.Fatal("boundary RSI must pass")
	}
	if ok, _ := entryAllowed(mk(vt.Buy, 0.7, 10), 0, 35); !ok {
		t.Fatal("long must ignore short RSI floor")
	}
	if ok, _ := entryAllowed(nil, 0.58, 35); ok {
		t.Fatal("nil must not pass")
	}
}

func TestTobiitaBlocked(t *testing.T) {
	// 飛び板注意はロング・ショート対称：通常スプレッドは通し、異常拡大のみ止める。
	// LINK実測16bps前後では発火しないこと。
	if tobiitaBlocked(0) {
		t.Fatal("zero spread must pass (fail-open)")
	}
	if tobiitaBlocked(-0.001) {
		t.Fatal("negative spread must pass (fail-open)")
	}
	if tobiitaBlocked(0.0016) {
		t.Fatal("normal 16bps must pass")
	}
	if tobiitaBlocked(0.01) {
		t.Fatal("boundary 100bps must pass")
	}
	if !tobiitaBlocked(0.010001) {
		t.Fatal("extreme spread must block")
	}
	if !tobiitaBlocked(0.02) {
		t.Fatal("tobiita 200bps must block both sides")
	}
}

func TestOrphanHelpers(t *testing.T) {
	// ゾンビ終端は対称・狭義：実績文言のみ検出し、12回連続でのみクリアする。
	if !isOrphanNoPosition("gmo no open SELL position for XRP_JPY to close") {
		t.Fatal("proven margin message must match")
	}
	if !isOrphanNoPosition("GMO No Open Buy Position") {
		t.Fatal("case-insensitive match must work")
	}
	if isOrphanNoPosition("") {
		t.Fatal("empty must not match (fail-closed)")
	}
	if isOrphanNoPosition("connection timeout") {
		t.Fatal("transient error must not match")
	}
	if isOrphanNoPosition("insufficient balance") {
		t.Fatal("spot shortage must not match (margin-only trigger)")
	}
	if orphanShouldClear(11) {
		t.Fatal("11 fails must hold (retry)")
	}
	if !orphanShouldClear(12) {
		t.Fatal("12 fails must clear")
	}
	if !orphanShouldClear(100) {
		t.Fatal("many fails must clear")
	}
}

func TestTopWallImb(t *testing.T) {
	// 上位3段の板厚（売り厚で負・買い厚で正）。順序不正・不足・空への耐性も対称に。
	bidPx := []float64{100.4, 100.3, 100.2, 100.1}
	bidVol := []float64{5, 5, 5, 100}
	askPx := []float64{100.6, 100.7}
	askVol := []float64{70, 70}
	if w, ok := topWallImb(bidPx, bidVol, askPx, askVol); !ok || w >= -0.5 {
		t.Fatalf("top3 sell wall must read firmly negative, got %v %v", w, ok)
	}
	// 順序を崩しても価格順に選び直す（4段目の深い買いは数えない）。
	if w, ok := topWallImb(
		[]float64{100.1, 100.4, 100.2, 100.3}, []float64{100, 5, 5, 5},
		[]float64{100.7, 100.6}, []float64{70, 70}); !ok || w >= -0.5 {
		t.Fatalf("unordered books must still read sell wall, got %v %v", w, ok)
	}
	if w, ok := topWallImb(
		[]float64{100.4, 100.3, 100.2}, []float64{60, 60, 60},
		[]float64{100.6, 100.7}, []float64{7, 7}); !ok || w <= 0.5 {
		t.Fatalf("top3 buy wall must read firmly positive, got %v %v", w, ok)
	}
	if _, ok := topWallImb(nil, nil, nil, nil); ok {
		t.Fatal("empty books must be unknown")
	}
	if _, ok := topWallImb([]float64{100}, []float64{0}, []float64{101}, []float64{0}); ok {
		t.Fatal("zero volumes must be unknown")
	}
}

func TestSlipBps(t *testing.T) {
	// Buy は高約定、Sell は安約定が不利（正値）。0.1% = 10bps。
	if got := slipBps(100.1, 100, vt.Buy); got < 9.99 || got > 10.01 {
		t.Fatalf("buy adverse = %v, want ~10bps", got)
	}
	if got := slipBps(99.9, 100, vt.Buy); got > -9.99 || got < -10.01 {
		t.Fatalf("buy favorable = %v, want ~-10bps", got)
	}
	if got := slipBps(99.9, 100, vt.Sell); got < 9.99 || got > 10.01 {
		t.Fatalf("sell adverse = %v, want ~10bps", got)
	}
	if got := slipBps(100.1, 100, vt.Sell); got > -9.99 || got < -10.01 {
		t.Fatalf("sell favorable = %v, want ~-10bps", got)
	}
	if got := slipBps(100, 100, vt.Buy); got != 0 {
		t.Fatalf("flat must be 0, got %v", got)
	}
	if got := slipBps(0, 100, vt.Buy); got != 0 {
		t.Fatalf("zero fill must be 0, got %v", got)
	}
	if got := slipBps(100, 0, vt.Sell); got != 0 {
		t.Fatalf("zero ref must be 0, got %v", got)
	}
}

func TestTickStale(t *testing.T) {
	now := time.Now()
	if !tickStale(time.Time{}, now, 120) {
		t.Fatal("never-received must be stale")
	}
	if !tickStale(now.Add(-300*time.Second), now, 120) {
		t.Fatal("300s-old must be stale")
	}
	if tickStale(now.Add(-10*time.Second), now, 120) {
		t.Fatal("fresh must not be stale")
	}
	if tickStale(now.Add(-300*time.Second), now, 0) {
		t.Fatal("disabled threshold must never fire")
	}
}

func TestMakerLimitPrice(t *testing.T) {
	// 指値は必ず板に残る（買いbid・売りask）。不明時は5bps内側。
	if got := makerLimitPrice(100, 0.002, vt.Buy); got != 99.9 {
		t.Fatalf("buy limit = %v, want 99.9", got)
	}
	if got := makerLimitPrice(100, 0.002, vt.Sell); got != 100.1 {
		t.Fatalf("sell limit = %v, want 100.1", got)
	}
	if got := makerLimitPrice(100, 0, vt.Buy); got != 99.95 {
		t.Fatalf("unknown spread buy = %v, want 99.95", got)
	}
	// 非市場性の確認：買い指値<mid<売り指値。
	b, s := makerLimitPrice(100, 0.01, vt.Buy), makerLimitPrice(100, 0.01, vt.Sell)
	if !(b < 100 && 100 < s) {
		t.Fatalf("limits must rest outside mid: %v %v", b, s)
	}
}

func TestMakerTPLimit(t *testing.T) {
	// 利確指値は建玉方向に応じて entry×(1±tp)。無効値は0。
	if got := makerTPLimit(100, 0.02, vt.Buy); got != 102 {
		t.Fatalf("buy tp limit = %v, want 102", got)
	}
	if got := makerTPLimit(100, 0.02, vt.Sell); got != 98 {
		t.Fatalf("sell tp limit = %v, want 98", got)
	}
	if got := makerTPLimit(0, 0.02, vt.Buy); got != 0 {
		t.Fatalf("zero entry must yield 0, got %v", got)
	}
	if got := makerTPLimit(100, 0, vt.Buy); got != 0 {
		t.Fatalf("zero tp must yield 0, got %v", got)
	}
}

func TestVtLiveStateMakerExitRoundTrip(t *testing.T) {
	// Maker出口の退避と復元：建玉と同時に保持できる（入口待機とは別枠）。
	st := vtLiveState{Date: "2026-10-18", MakerExit: &makerExit{
		OrderID: "o1", Limit: 102.5, Qty: 3, Placed: 123, Reason: "take_profit"}}
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var back vtLiveState
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.MakerExit == nil || back.MakerExit.Limit != 102.5 ||
		back.MakerExit.OrderID != "o1" || back.MakerExit.Qty != 3 {
		t.Fatalf("maker exit not preserved: %+v", back.MakerExit)
	}
}
