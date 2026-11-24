package vt

import (
	"testing"
)

// gridRangeBase はレンジ横ばい＋押し目反発の土台を返す。
// 上位足は横ばい固定（range Regime を安定化）、入口足は100本の±0.4往復
// ＋2.5押し＋小休止＋二段陽線復帰（下限圏に残る・MACD/RCI底打ち付き）。
// flowBid/flowAsk で板向きを指定する。
func gridRangeBase(flowBid, flowAsk float64) *Engine {
	cfg := DefaultConfig()
	cfg.TrendTFSec = 3600
	eng := NewEngine(cfg)
	tp := 100.0
	for i := 0; i < 40; i++ {
		c := tp + 0.05
		if i%2 == 1 {
			c = tp - 0.05
		}
		eng.trendFor("TST").done = append(eng.trendFor("TST").done,
			Bar{Start: int64(i) * 3600000, O: tp, H: c + 0.02, L: c - 0.02, C: c, V: 1})
		tp = c
	}
	q := 100.0
	push := func(o, h, l, c float64) {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done,
			Bar{Start: int64(len(eng.builderFor("TST").Bars())) * 300000, O: o, H: h, L: l, C: c, V: 1})
	}
	for i := 0; i < 100; i++ {
		c := q + 0.4
		if i%2 == 1 {
			c = q - 0.4
		}
		o := q
		hi, lo := c+0.03, o-0.03
		if c < o {
			hi, lo = o+0.03, c-0.03
		}
		push(o, hi, lo, c)
		q = c
	}
	o := q
	c := o - 2.5
	push(o, o+0.03, c-0.03, c)
	p := c
	o = p
	c = o + 0.1
	push(o, c+0.03, o-0.03, c)
	p = c
	o = p
	c = o + 0.4
	push(o, c+0.03, o-0.03, c)
	p = c
	o = p
	c = o + 0.9
	push(o, c+0.03, o-0.03, c)
	for i := 0; i < 10; i++ {
		tot := flowBid + flowAsk
		imb := 0.0
		if tot > 0 {
			imb = (flowBid - flowAsk) / tot
		}
		eng.imbs["TST"] = append(eng.imbs["TST"], imb)
	}
	return eng
}

func TestGridEntryFiresInRange(t *testing.T) {
	eng := gridRangeBase(9, 1)
	bars := eng.builderFor("TST").Bars()
	closes := Closes(bars)
	_, upper, lower := Bollinger(closes, 20, 2.0)
	last := closes[len(closes)-1]
	rsi := RSI(closes, 14)
	minRSI5 := rsi
	for k := 1; k < 5 && len(closes)-k >= 15; k++ {
		if v := RSI(closes[:len(closes)-k], 14); v < minRSI5 {
			minRSI5 = v
		}
	}
	_, _, hist := MACD(closes, 12, 26, 9)
	_, _, prevHist := MACD(closes[:len(closes)-1], 12, 26, 9)
	cur := bars[len(bars)-1]
	t.Logf("grid probe: pb=%.3f rsi=%.1f minRSI5=%.1f hist=%.4f prevHist=%.4f bullish=%v body=%.3f range=%.3f bw=%.4f",
		PercentB(last, upper, lower), rsi, minRSI5, hist, prevHist, IsBullish(cur), Body(cur), Range(cur), Bandwidth(closes, 20, 2.0))
	sig := eng.GridEntry("TST", 9999999, 0.0002)
	if sig.Type != Buy || sig.Pattern != GridPattern {
		t.Fatalf("range dip must fire grid buy, got %s %s (%s)", sig.Type, sig.Pattern, sig.Reason)
	}
	if sig.Confidence != gridConfidence {
		t.Fatalf("grid confidence = %v, want %v", sig.Confidence, gridConfidence)
	}
	if sig.BarH <= 0 || sig.BarV <= 0 {
		t.Fatalf("entry bar shape must be recorded: %+v", sig)
	}
}

func TestGridEntryHolds(t *testing.T) {
	// トレンド域では出ない（二刀流の分担）。
	up := NewEngine(DefaultConfig())
	for _, b := range risingBars(100, 0.5) {
		up.builderFor("TST").done = append(up.builderFor("TST").done, b)
	}
	if sig := up.EvalEntry("TST", 9999999); sig.Type == Hold {
		_ = sig
	}
	// 上昇一途では range にならないため grid も Hold。
	if sig := up.GridEntry("TST", 9999999, 0); sig.Type != Hold || sig.Reason != "grid need range" {
		t.Fatalf("uptrend must hold grid, got %s (%s)", sig.Type, sig.Reason)
	}
	// 売り圧下の押し目は見送る（対称思想の veto）。
	sell := gridRangeBase(1, 9)
	if sig := sell.GridEntry("TST", 9999999, 0.0002); sig.Type != Hold || sig.Reason != "against orderflow" {
		t.Fatalf("sell pressure must veto grid, got %s (%s)", sig.Type, sig.Reason)
	}
	// 死んだレンジ（帯域不足）は手数料負けのため出ない。
	dead := NewEngine(DefaultConfig())
	q := 100.0
	for i := 0; i < 100; i++ {
		c := q + 0.02
		if i%2 == 1 {
			c = q - 0.02
		}
		dead.builderFor("TST").done = append(dead.builderFor("TST").done,
			Bar{Start: int64(i) * 300000, O: q, H: c + 0.01, L: c - 0.01, C: c, V: 1})
		q = c
	}
	if sig := dead.GridEntry("TST", 9999999, 0); sig.Type != Hold {
		t.Fatalf("dead range must hold grid, got %s %s (%s)", sig.Type, sig.Pattern, sig.Reason)
	}
	// 厚いスプレッドでは不発火（fee edge）。
	wide := gridRangeBase(9, 1)
	if sig := wide.GridEntry("TST", 9999999, 0.02); sig.Type != Hold || sig.Reason != "grid edge too thin" {
		t.Fatalf("wide spread must hold grid, got %s (%s)", sig.Type, sig.Reason)
	}
}

func TestGridExit(t *testing.T) {
	eng := gridRangeBase(9, 1)
	pos := OpenPos{Side: Buy, EntryPrice: 99.0, EntryTime: 1, Volume: 1}
	bars := eng.builderFor("TST").Bars()
	lastC := bars[len(bars)-1].C
	// 中央付近の現在値では利確する。
	midBB := SMA(Closes(bars), 20)
	if ok, r := eng.GridExit("TST", pos, midBB+1.0, 99999); !ok || r != "grid_tp" {
		t.Fatalf("mid must take grid profit, got %v %q", ok, r)
	}
	// 帯割れでは無効化する（trend へ明け渡し）。
	if ok, r := eng.GridExit("TST", pos, lastC-5.0, 99999); !ok || r != "grid_invalidate" {
		t.Fatalf("band break must invalidate grid, got %v %q", ok, r)
	}
	// 中間では保持する。
	if ok, r := eng.GridExit("TST", pos, lastC, 99999); ok {
		t.Fatalf("mid-range must hold grid, got exit %q", r)
	}
	// 売り玉・不正値は対象外。
	sell := OpenPos{Side: Sell, EntryPrice: 99.0, EntryTime: 1, Volume: 1}
	if ok, _ := eng.GridExit("TST", sell, midBB+1.0, 99999); ok {
		t.Fatal("sell must not grid-exit")
	}
	if ok, _ := eng.GridExit("TST", pos, 0, 99999); ok {
		t.Fatal("zero mid must not grid-exit")
	}
}

func TestIsGridPattern(t *testing.T) {
	if !IsGridPattern("grid_range_buy") {
		t.Fatal("grid prefix must match")
	}
	if IsGridPattern("bb_bounce") || IsGridPattern("") || IsGridPattern("grid") {
		t.Fatal("non-grid must not match")
	}
}
