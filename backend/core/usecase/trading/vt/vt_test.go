package vt

import (
	"testing"
)

func risingBars(n int, step float64) []Bar {
	bars := make([]Bar, 0, n)
	p := 100.0
	for i := 0; i < n; i++ {
		o := p
		c := p + step
		bars = append(bars, Bar{Start: int64(i) * 60000, O: o, H: c + 0.1, L: o - 0.1, C: c, V: 1})
		p = c
	}
	return bars
}

func TestIndicators(t *testing.T) {
	closes := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if got := SMA(closes, 5); got != 8 {
		t.Fatalf("sma = %v, want 8", got)
	}
	if got := SMA(closes, 20); got != 0 {
		t.Fatalf("sma short = %v, want 0", got)
	}
	mid, up, lo := Bollinger(closes, 5, 2.0)
	if !(lo < mid && mid < up) {
		t.Fatalf("bollinger not ordered: %v %v %v", lo, mid, up)
	}
	if got := RSI([]float64{1, 2, 3, 4, 5, 6}, 5); got != 100 {
		t.Fatalf("rsi all-up = %v, want 100", got)
	}
	if got := RSI([]float64{6, 5, 4, 3, 2, 1}, 5); got != 0 {
		t.Fatalf("rsi all-down = %v, want 0", got)
	}
	m, s, h := MACD(closes, 2, 5, 3)
	if m <= 0 || s <= 0 || h == 0 {
		t.Fatalf("macd on rise should be positive: %v %v %v", m, s, h)
	}
}

func TestCandlePatterns(t *testing.T) {
	bullish := Bar{O: 100, H: 112, L: 99, C: 110} // 実体10/全長13
	bearish := Bar{O: 110, H: 111, L: 98, C: 100} // 実体10/全長13
	if !IsBullishEngulfing(bearish, bullish) {
		t.Fatal("expected bullish engulfing")
	}
	if IsBullishEngulfing(bullish, bearish) {
		t.Fatal("reversed engulfing must not fire")
	}
	hammer := Bar{O: 100, H: 100.5, L: 90, C: 102} // 下ヒゲ12・実体2・上0.5…上ヒゲ判定
	_ = hammer
	ham := Bar{O: 100, H: 100.5, L: 94, C: 102} // 下6・実体2・上0.5
	if !IsHammer(ham) {
		t.Fatalf("expected hammer: %+v", ham)
	}
	star := Bar{O: 102, H: 108, L: 101.8, C: 104} // 上4・実体2・下0.2
	if !IsShootingStar(star) {
		t.Fatalf("expected shooting star: %+v", star)
	}
	doji := Bar{O: 100, H: 105, L: 95, C: 100.5} // 実体0.5/全長10
	if !IsDoji(doji) {
		t.Fatalf("expected doji: %+v", doji)
	}
	// 明けの明星：大陰線→小保ち合い→大陽線（50%戻し）。
	a := Bar{O: 110, H: 111, L: 98, C: 100}
	b := Bar{O: 100, H: 101, L: 99, C: 100.5}
	c := Bar{O: 100.5, H: 108, L: 100, C: 107}
	if !IsMorningStar([]Bar{a, b, c}) {
		t.Fatal("expected morning star")
	}
	if IsEveningStar([]Bar{a, b, c}) {
		t.Fatal("morning star must not be evening star")
	}
	// 赤三兵
	s1 := Bar{O: 100, H: 103, L: 99.8, C: 102.5}
	s2 := Bar{O: 102.5, H: 105, L: 102.3, C: 104.5}
	s3 := Bar{O: 104.5, H: 107, L: 104.3, C: 106.5}
	if !IsThreeSoldiers([]Bar{s1, s2, s3}) {
		t.Fatal("expected three soldiers")
	}
}

func TestEngineLongOnMorningStar(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TrendTFSec = 0 // 単一足モードで入口ロジックを検証する
	eng := NewEngine(cfg)
	bars := morningStarEntry()
	for _, b := range bars {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
	}
	sig := eng.EvalEntry("TST", 9999999)
	if sig.Type != Buy {
		t.Fatalf("expected buy, got %s (%s)", sig.Type, sig.Reason)
	}
	if sig.Pattern != "morning_star" {
		t.Fatalf("expected morning_star, got %s", sig.Pattern)
	}
}

// morningStarEntry は揉み合い→大陰線→小陽線→大陽線の明星形を返す。
// RSIは過熱圏外（形状entryの過熱 veto に掛からない水準）に収める。
// 土台は明確な上昇（+0.5,+0.5,-0.5 反復）で、V底でも trend_up を保つ。
func morningStarEntry() []Bar {
	var bars []Bar
	p := 100.0
	for i := 0; i < 39; i++ {
		d := 0.5
		if i%3 == 2 {
			d = -0.5
		}
		o := p
		c := o + d
		hi, lo := c+0.05, o-0.05
		if d < 0 {
			hi, lo = o+0.05, c-0.05
		}
		bars = append(bars, Bar{Start: int64(len(bars)) * 60000, O: o, H: hi, L: lo, C: c, V: 1})
		p = c
	}
	aO := p
	aC := aO - 2.0
	bars = append(bars, Bar{Start: int64(len(bars)) * 60000, O: aO, H: aO + 0.05, L: aC - 0.05, C: aC, V: 1})
	bO := aC
	bC := bO + 0.1
	bars = append(bars, Bar{Start: int64(len(bars)) * 60000, O: bO, H: bC + 0.2, L: bO - 0.2, C: bC, V: 1})
	cO := bC - 0.2
	cC := aO + 0.3
	bars = append(bars, Bar{Start: int64(len(bars)) * 60000, O: cO, H: cC + 0.05, L: cO - 0.05, C: cC, V: 1})
	return bars
}

func TestEngineFlatOnRange(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TrendTFSec = 0 // 単一足モード
	eng := NewEngine(cfg)
	var bars []Bar
	p := 100.0
	for i := 0; i < 40; i++ {
		// 横ばい往復（トレンドなし）。
		c := p
		if i%2 == 0 {
			c = p + 0.1
		} else {
			c = p - 0.1
		}
		bars = append(bars, Bar{Start: int64(i) * 60000, O: p, H: c + 0.2, L: c - 0.2, C: c, V: 1})
		p = c
	}
	for _, b := range bars {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
	}
	sig := eng.EvalEntry("TST", 9999999)
	if sig.Type != Hold {
		t.Fatalf("expected hold on range, got %s (%s)", sig.Type, sig.Reason)
	}
}

func TestEngineExit(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TPRatio, cfg.SLRatio, cfg.MaxHoldSec = 0.02, 0.01, 5*3600
	eng := NewEngine(cfg)
	pos := OpenPos{Side: Buy, EntryPrice: 100, EntryTime: 0, Volume: 1}
	if ok, r := eng.EvalExit("TST", pos, 102.5, 1000); !ok || r != "take_profit" {
		t.Fatalf("expected take_profit, got %v %q", ok, r)
	}
	if ok, r := eng.EvalExit("TST", pos, 98.5, 1000); !ok || r != "stop_loss" {
		t.Fatalf("expected stop_loss, got %v %q", ok, r)
	}
	if ok, _ := eng.EvalExit("TST", pos, 100.5, 1000); ok {
		t.Fatal("expected no exit mid-range")
	}
	if ok, r := eng.EvalExit("TST", pos, 100.5, int64(5*3600*1000)); !ok || r != "max_hold" {
		t.Fatalf("expected max_hold, got %v %q", ok, r)
	}
}

func TestDefaultSafetyNet(t *testing.T) {
	// 既定で SL 2% のセーフティーネットが有効。小幅損失では売らない。
	eng := NewEngine(DefaultConfig())
	pos := OpenPos{Side: Buy, EntryPrice: 100, EntryTime: 0, Volume: 1}
	if ok, r := eng.EvalExit("TST", pos, 97, 1000); !ok || r != "stop_loss" {
		t.Fatalf("expected default stop_loss at -3%%, got %v %q", ok, r)
	}
	if ok, _ := eng.EvalExit("TST", pos, 99, 1000); ok {
		t.Fatal("default must hold at -1%")
	}
}

func TestImbalanceGateAndFlip(t *testing.T) {
	eng := NewEngine(DefaultConfig())
	// 売り圧連続で含み損の買い建玉は imbalance_flip で離脱する。
	for i := 0; i < 6; i++ {
		eng.Observe("TST", int64(i)*5000, 100, 1, 9)
	}
	pos := OpenPos{Side: Buy, EntryPrice: 100, EntryTime: 0, Volume: 1}
	if ok, r := eng.EvalExit("TST", pos, 99, 99999); !ok || r != "imbalance_flip" {
		t.Fatalf("expected imbalance_flip, got %v %q", ok, r)
	}
	// 含み益の玉は板ノイズで降ろさず確定シグナルまで保持する。
	if ok, r := eng.EvalExit("TST", pos, 101, 99999); ok {
		t.Fatalf("winner must ride through noise, got exit %q", r)
	}
	// 買い圧連続なら離脱しない。
	eng2 := NewEngine(DefaultConfig())
	for i := 0; i < 6; i++ {
		eng2.Observe("TST", int64(i)*5000, 100, 9, 1)
	}
	if ok, _ := eng2.EvalExit("TST", pos, 100, 99999); ok {
		t.Fatal("aligned flow must not exit")
	}
	// 売り圧下の long 入口は見送られる（方向が出ても）。
	if eng2.flowDir("TST") != 1 {
		t.Fatalf("expected buy flow, got %d", eng2.flowDir("TST"))
	}
	if eng.flowDir("TST") != -1 {
		t.Fatalf("expected sell flow, got %d", eng.flowDir("TST"))
	}
}

func TestBuilder(t *testing.T) {
	b := NewBuilder(60, 10)
	if fin := b.Add(0, 100, 1); fin != nil {
		t.Fatal("first tick must not finish a bar")
	}
	if fin := b.Add(30000, 101, 1); fin != nil {
		t.Fatal("same slot must not finish a bar")
	}
	fin := b.Add(60000, 102, 1)
	if fin == nil || fin.O != 100 || fin.C != 101 || fin.H != 101 || fin.L != 100 {
		t.Fatalf("bad finished bar: %+v", fin)
	}
	if len(b.Bars()) != 1 {
		t.Fatalf("expected 1 done bar, got %d", len(b.Bars()))
	}
}

func TestTrendGate(t *testing.T) {
	entry := morningStarEntry()
	// 上位足が下降なら明けの明星でも不発火。
	cfg := DefaultConfig()
	cfg.TrendTFSec = 3600
	eng := NewEngine(cfg)
	falling := make([]Bar, 0, 40)
	p := 200.0
	for i := 0; i < 40; i++ {
		o := p
		c := p - 0.5
		falling = append(falling, Bar{Start: int64(i) * 3600000, O: o, H: o + 0.1, L: c - 0.1, C: c, V: 1})
		p = c
	}
	for _, b := range falling {
		eng.trendFor("TST").done = append(eng.trendFor("TST").done, b)
	}
	for _, b := range entry {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
	}
	sig := eng.EvalEntry("TST", 9999999)
	if sig.Type != Hold {
		t.Fatalf("counter-trend morning star must hold, got %s (%s)", sig.Type, sig.Reason)
	}
	// 上位足が上昇なら発火する。
	eng2 := NewEngine(cfg)
	rising := risingBars(40, 0.5)
	for i := range rising {
		rising[i].Start = int64(i) * 3600000
	}
	for _, b := range rising {
		eng2.trendFor("TST").done = append(eng2.trendFor("TST").done, b)
	}
	for _, b := range entry {
		eng2.builderFor("TST").done = append(eng2.builderFor("TST").done, b)
	}
	sig2 := eng2.EvalEntry("TST", 9999999)
	if sig2.Type != Buy {
		t.Fatalf("aligned morning star must fire, got %s (%s)", sig2.Type, sig2.Reason)
	}
}

func TestTapeAlignedAndAbsorbed(t *testing.T) {
	eng := NewEngine(DefaultConfig())
	// 買い食い90%・ほぼ無風なら吸収成立。
	for i := 0; i < 60; i++ {
		eng.AddTape("TST", int64(i)*1000, 100+float64(i%2)*0.01, 9, true)
		eng.AddTape("TST", int64(i)*1000, 100, 1, false)
	}
	if !eng.tapeAligned("TST", 1) {
		t.Fatal("buy-heavy tape must align long")
	}
	if eng.tapeAligned("TST", -1) {
		t.Fatal("buy-heavy tape must not align short")
	}
	if !eng.absorbed("TST") {
		t.Fatal("one-sided flat tape must count as absorption")
	}
	// 件数不足は fail-open（入口側）・不成立（出口側）。
	fresh := NewEngine(DefaultConfig())
	if !fresh.tapeAligned("TST", 1) || !fresh.tapeAligned("TST", -1) {
		t.Fatal("cold tape must fail open")
	}
	if fresh.absorbed("TST") {
		t.Fatal("cold tape must not count as absorption")
	}
	// 吸収は EvalExit で損切り前に離脱させる（含み損の玉のみ）。
	pos := OpenPos{Side: Buy, EntryPrice: 100, EntryTime: 0, Volume: 1}
	if ok, r := eng.EvalExit("TST", pos, 99.5, 99999); !ok || r != "absorption" {
		t.Fatalf("expected absorption exit, got %v %q", ok, r)
	}
	// 含み益の玉は吸収でも降ろさない。
	if ok, r := eng.EvalExit("TST", pos, 100.01, 99999); ok {
		t.Fatalf("winner must ignore absorption, got exit %q", r)
	}
}

func TestMACDHistFlipPrimitive(t *testing.T) {
	// 上昇→下落→再上昇でヒストグラムが負→正に反転すること。
	closes := []float64{}
	p := 100.0
	for i := 0; i < 30; i++ {
		p += 0.5
		closes = append(closes, p)
	}
	for i := 0; i < 4; i++ {
		p -= 1.2
		closes = append(closes, p)
	}
	for i := 0; i < 4; i++ {
		p -= 1.2
		closes = append(closes, p)
	}
	for i := 0; i < 7; i++ {
		p += 1.0
		closes = append(closes, p)
	}
	_, _, prevHist := MACD(closes[:len(closes)-1], 12, 26, 9)
	_, _, hist := MACD(closes, 12, 26, 9)
	if !(prevHist <= 0 && hist > 0) {
		t.Fatalf("expected hist flip (-/+), got prev=%v last=%v", prevHist, hist)
	}
}

func TestTrendSlopeCounterBlocks(t *testing.T) {
	// MA位置は上だが傾きが下向きならレンジ扱い（往復ビンタ防止）。
	closes := []float64{}
	p := 100.0
	for i := 0; i < 25; i++ {
		p += 0.5
		closes = append(closes, p)
	}
	// 最後に急落：MA5はまだMA20の上だが傾きは下向き。
	for i := 0; i < 2; i++ {
		p -= 2.0
		closes = append(closes, p)
	}
	if got := trendOf(closes, 5, 20); got != "range" {
		t.Fatalf("counter-slope must be range, got %s", got)
	}
	// 純粋な上昇は trend_up のまま。
	up := []float64{}
	q := 100.0
	for i := 0; i < 27; i++ {
		q += 0.5
		up = append(up, q)
	}
	if got := trendOf(up, 5, 20); got != "trend_up" {
		t.Fatalf("aligned rise must be trend_up, got %s", got)
	}
}

func TestEngineShortOnBreakdown(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TrendTFSec = 0 // 単一足モードで入口ロジックを検証する
	eng := NewEngine(cfg)
	var bars []Bar
	p := 120.0
	// 下降20本で下降レジームを作る。
	for i := 0; i < 20; i++ {
		o := p
		c := p - 0.5
		bars = append(bars, Bar{Start: int64(i) * 300000, O: o, H: o + 0.05, L: c - 0.05, C: c, V: 1})
		p = c
	}
	// 20本の凪でスクイーズを作る（MA5はMA20の下に残る）。
	for i := 20; i < 40; i++ {
		c := p + float64(i%2)*0.01
		bars = append(bars, Bar{Start: int64(i) * 300000, O: p, H: c + 0.02, L: c - 0.02, C: c, V: 1})
	}
	// 下抜け足（計41本：bb判定の下限）。
	bars = append(bars, Bar{Start: int64(40) * 300000, O: p, H: p + 0.02, L: p - 1.0, C: p - 0.9, V: 1})
	for _, b := range bars {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
	}
	sig := eng.EvalEntry("TST", 9999999)
	if sig.Type != Sell {
		t.Fatalf("expected sell, got %s (%s)", sig.Type, sig.Reason)
	}
	if sig.Pattern != "bb_breakdown" {
		t.Fatalf("expected bb_breakdown, got %s", sig.Pattern)
	}
}

func TestPullbackHelpers(t *testing.T) {
	// MA帯への押し→陽引け復帰。
	prev := Bar{O: 103, H: 103.1, L: 102.4, C: 102.6, V: 1} // 陰線で帯に突っ込む
	cur := Bar{O: 102.7, H: 103.7, L: 102.65, C: 103.6, V: 1}
	if !isPullbackToMA([]Bar{prev, cur}, 103.2, 102.9) {
		t.Fatal("expected pullback_to_ma")
	}
	if isPullbackToMA([]Bar{prev, cur}, 105, 104.8) {
		t.Fatal("far above zone must not be pullback")
	}
	sinking := Bar{O: 102.6, H: 102.7, L: 101.5, C: 101.8, V: 1} // 帯割れのまま陰引け
	if isPullbackToMA([]Bar{prev, sinking}, 103.2, 102.9) {
		t.Fatal("close below zone must not be pullback")
	}
	// BBミッド版。
	if !isPullbackToBBMid([]Bar{prev, cur}, 103.0) {
		t.Fatal("expected pullback_to_bbmid")
	}
	if isPullbackToBBMid([]Bar{prev, cur}, 104.0) {
		t.Fatal("no touch of mid must not fire")
	}
	// 戻り売り対称形。
	rprev := Bar{O: 102.6, H: 103.2, L: 102.5, C: 103.0, V: 1}
	rcur := Bar{O: 102.9, H: 102.95, L: 102.0, C: 102.1, V: 1}
	if !isPullbackToMAShort([]Bar{rprev, rcur}, 102.9, 102.6) {
		t.Fatal("expected pullback_to_ma_down")
	}
	if !isPullbackToBBMidShort([]Bar{rprev, rcur}, 102.5) {
		t.Fatal("expected pullback_to_bbmid_down")
	}
	if isPullbackToMAShort([]Bar{rprev, rcur}, 100, 99.8) {
		t.Fatal("far below zone must not fire short")
	}
}

func choppyRise(n int, p *float64, bars []Bar) []Bar {
	for i := 0; i < n; i++ {
		var d float64
		switch i % 3 {
		case 0, 1:
			d = 0.3
		default:
			d = -0.3
		}
		o := *p
		c := o + d
		hi, lo := c+0.05, o-0.05
		if d < 0 {
			hi, lo = o+0.05, c-0.05
		}
		bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: o, H: hi, L: lo, C: c, V: 1})
		*p = c
	}
	return bars
}

func TestEnginePullbackEntry(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TrendTFSec = 0 // 単一足モード
	eng := NewEngine(cfg)
	var bars []Bar
	p := 100.0
	bars = choppyRise(36, &p, bars)
	// 浅い押し2本→ギャップ気味の陽線復帰（包み・明星・三兵の形は作らない）。
	for _, d := range []float64{-0.5, -0.5} {
		o := p
		c := o + d
		bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: o, H: o + 0.05, L: c - 0.05, C: c, V: 1})
		p = c
	}
	o := p + 0.1
	c := o + 0.9
	bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: o, H: c + 0.05, L: o - 0.05, C: c, V: 1})
	for _, b := range bars {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
	}
	sig := eng.EvalEntry("TST", 9999999)
	if sig.Type != Buy {
		t.Fatalf("expected pullback buy, got %s (%s) rsi=%.1f pb=%.2f hist=%.4f", sig.Type, sig.Reason, sig.RSI, sig.PercentB, sig.MACDHist)
	}
	if sig.Pattern != "pullback_to_ma" && sig.Pattern != "pullback_to_bbmid" {
		t.Fatalf("expected pullback pattern, got %s (%s)", sig.Pattern, sig.Reason)
	}
}

func TestFastTFVetoAndBonus(t *testing.T) {
	entry := morningStarEntry()
	newEntryEngine := func() *Engine {
		cfg := DefaultConfig()
		cfg.TrendTFSec = 0
		cfg.FastTFSec = 60
		eng := NewEngine(cfg)
		for _, b := range entry {
			eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
		}
		return eng
	}
	// 下位足の中立（不足）では発火する。
	if sig := newEntryEngine().EvalEntry("TST", 9999999); sig.Type != Buy {
		t.Fatalf("cold fast must fail open, got %s (%s)", sig.Type, sig.Reason)
	}
	// 下位足が強逆行なら見送り。
	engVeto := newEntryEngine()
	fp := 200.0
	for i := 0; i < 30; i++ {
		o := fp
		c := fp - 0.5
		engVeto.fastFor("TST").done = append(engVeto.fastFor("TST").done,
			Bar{Start: int64(i) * 60000, O: o, H: o + 0.1, L: c - 0.1, C: c, V: 1})
		fp = c
	}
	if sig := engVeto.EvalEntry("TST", 9999999); sig.Type != Hold || sig.Reason != "lower-tf against" {
		t.Fatalf("expected lower-tf veto, got %s (%s)", sig.Type, sig.Reason)
	}
	// 下位足が順行なら確信度が微加算される（0.65→0.67）。
	engAgree := newEntryEngine()
	fp = 100.0
	for i := 0; i < 30; i++ {
		o := fp
		c := fp + 0.5
		engAgree.fastFor("TST").done = append(engAgree.fastFor("TST").done,
			Bar{Start: int64(i) * 60000, O: o, H: c + 0.1, L: o - 0.1, C: c, V: 1})
		fp = c
	}
	sig := engAgree.EvalEntry("TST", 9999999)
	if sig.Type != Buy {
		t.Fatalf("aligned fast must fire, got %s (%s)", sig.Type, sig.Reason)
	}
	if sig.Confidence < 0.669 || sig.Confidence > 0.671 {
		t.Fatalf("expected confidence ~0.67 with bonus, got %v", sig.Confidence)
	}
}

func TestTrailingStop(t *testing.T) {
	eng := NewEngine(DefaultConfig()) // trail 1.5%/0.8% 既定
	buy := OpenPos{Side: Buy, EntryPrice: 100, EntryTime: 1000, Volume: 1}
	if ok, _ := eng.EvalExit("TST", buy, 100.5, 2000); ok {
		t.Fatal("below activation must hold")
	}
	if ok, _ := eng.EvalExit("TST", buy, 102, 3000); ok {
		t.Fatal("at peak must hold")
	}
	if ok, r := eng.EvalExit("TST", buy, 101, 4000); !ok || r != "trailing_stop" {
		t.Fatalf("expected trailing_stop, got %v %q", ok, r)
	}
	// 売り対称形：98 で発動→98.9 の戻しで離脱。
	sell := OpenPos{Side: Sell, EntryPrice: 100, EntryTime: 2000, Volume: 1}
	if ok, _ := eng.EvalExit("TST", sell, 98, 3000); ok {
		t.Fatal("sell at low must hold")
	}
	if ok, r := eng.EvalExit("TST", sell, 98.9, 4000); !ok || r != "trailing_stop" {
		t.Fatalf("expected sell trailing_stop, got %v %q", ok, r)
	}
	// 建玉が入れ替わると高値がリセットされる（前玉の高値102を引き継がない）。
	buy2 := OpenPos{Side: Buy, EntryPrice: 100, EntryTime: 9999, Volume: 1}
	if ok, _ := eng.EvalExit("TST", buy2, 101, 5000); ok {
		t.Fatal("reset trail must hold at +1%")
	}
	// 無効化フラグでは発動しない。
	off := DefaultConfig()
	off.TrailActivateR, off.TrailBackR = 0, 0
	engOff := NewEngine(off)
	if ok, _ := engOff.EvalExit("TST", buy, 102, 3000); ok {
		t.Fatal("disabled trail must hold at peak")
	}
	if ok, _ := engOff.EvalExit("TST", buy, 100.5, 4000); ok {
		t.Fatal("disabled trail must hold after fade")
	}
}

func TestRCI(t *testing.T) {
	// 株の達人の作例：新→旧 [700,750,780,750,800] → -77.5。
	if got := RCI([]float64{800, 750, 780, 750, 700}, 5); got < -77.6 || got > -77.4 {
		t.Fatalf("rci example = %v, want -77.5", got)
	}
	rising := []float64{}
	for i := 0; i < 9; i++ {
		rising = append(rising, float64(100+i))
	}
	if got := RCI(rising, 9); got != 100 {
		t.Fatalf("rci monotonic rise = %v, want 100", got)
	}
	if got := RCI(rising, 20); got != 0 {
		t.Fatalf("rci short = %v, want 0", got)
	}
}

func TestDivergenceLogic(t *testing.T) {
	// 安値切り下げ＋指標切り上げ＝強気ダイバージェンス。
	closes := []float64{100, 98, 99, 97, 99, 96, 98}
	rsi := []float64{50, 40, 42, 30, 35, 32, 45}
	if !BullishDivergence(closes, rsi, 1, 7) {
		t.Fatal("expected bullish divergence")
	}
	if BearishDivergence(closes, rsi, 1, 7) {
		t.Fatal("must not be bearish divergence")
	}
	// 切り上げなしでは不発火。
	flat := []float64{50, 40, 42, 30, 35, 28, 25}
	if BullishDivergence(closes, flat, 1, 7) {
		t.Fatal("lower-low indicator must not diverge")
	}
}

func TestHistFlips(t *testing.T) {
	// V字（下降→上昇）で反転をまたぐ窓はちょうど1反転、収束後は0。
	closes := []float64{}
	p := 150.0
	for i := 0; i < 40; i++ {
		p -= 0.5
		closes = append(closes, p)
	}
	for i := 0; i < 40; i++ {
		p += 0.5
		closes = append(closes, p)
	}
	// 反転をまたぐ窓ではちょうど1反転、収束後の窓では0。
	if got := HistFlips(closes, 12, 26, 9, 45); got != 1 {
		t.Fatalf("v-turn flips = %d, want 1", got)
	}
	if got := HistFlips(closes, 12, 26, 9, 5); got != 0 {
		t.Fatalf("settled uptrend flips = %d, want 0", got)
	}
	// フラットでは中立（0反転）。
	flat := []float64{}
	for i := 0; i < 45; i++ {
		flat = append(flat, 100)
	}
	if got := HistFlips(flat, 12, 26, 9, 5); got != 0 {
		t.Fatalf("flat flips = %d, want 0", got)
	}
}

// uptrendEngine は上昇1時間足＋指定入口足でエンジンを作る（V底テスト用）。
func uptrendEngine(bars []Bar) *Engine {
	cfg := DefaultConfig()
	cfg.TrendTFSec = 3600
	eng := NewEngine(cfg)
	rising := risingBars(40, 0.5)
	for i := range rising {
		rising[i].Start = int64(i) * 3600000
	}
	for _, b := range rising {
		eng.trendFor("TST").done = append(eng.trendFor("TST").done, b)
	}
	for _, b := range bars {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
	}
	return eng
}

func vBottomEntry() []Bar {
	var bars []Bar
	p := 100.0
	bars = choppyRise(36, &p, bars)
	for _, d := range []float64{-2.5, -2.5, -2.5, -2.5, -2.5} {
		o := p
		c := o + d
		bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: o, H: o + 0.05, L: c - 0.05, C: c, V: 1})
		p = c
	}
	return bars
}

func TestEngineBounceEntry(t *testing.T) {
	// 安定化した押し目（MACD/RCI反転＋板買い厚）では bb_bounce が出る。
	// 浅い洗い（-1.5×3）＋陽線復帰で、指標が反転済みの形を狙う。
	var bars []Bar
	p := 100.0
	bars = choppyRise(36, &p, bars)
	for _, d := range []float64{-1.5, -1.5, -1.5} {
		o := p
		c := o + d
		bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: o, H: o + 0.05, L: c - 0.05, C: c, V: 1})
		p = c
	}
	o := p + 0.05
	c := o + 1.2
	bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: o, H: c + 0.05, L: o - 0.05, C: c, V: 1})
	xcloses := Closes(bars)
	_, xup, xlow := Bollinger(xcloses, 20, 2.0)
	t.Logf("bounce setup: pb=%.3f rsi=%.1f", PercentB(xcloses[len(xcloses)-1], xup, xlow), RSI(xcloses, 14))
	eng := uptrendEngine(bars)
	for i := 0; i < 10; i++ {
		eng.imbs["TST"] = append(eng.imbs["TST"], 0.8)
		eng.walls["TST"] = append(eng.walls["TST"], 0.8)
	}
	sig := eng.EvalEntry("TST", 9999999)
	if sig.Type != Buy {
		t.Fatalf("expected bounce buy, got %s (%s) rsi=%.1f pb=%.2f hist=%.4f", sig.Type, sig.Reason, sig.RSI, sig.PercentB, sig.MACDHist)
	}
	// 安定化した押し目は bb_bounce か rsi_recover のいずれかで拾う。
	// いずれも新ゲート（方向・実体・板厚）付きで発火することが要点。
	if sig.Pattern != "bb_bounce" && sig.Pattern != "rsi_recover" {
		t.Fatalf("expected bb_bounce/rsi_recover, got %s (%s)", sig.Pattern, sig.Reason)
	}
}

func TestEngineBounceHoldsInWaterfall(t *testing.T) {
	// ETH23:20型：加速下落の底で反転が安定する前の陽線では bb_bounce を出さない。
	// 板厚があってもMACD/RCIが下向きのままなら見送る（v_bottom/big_white_dipも同様）。
	bars := vBottomEntry()
	// ギャップ気味の陽線復帰（包み線の形は作らない）。
	p := bars[len(bars)-1].C
	o := p + 0.1
	c := o + 1.2
	bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: o, H: c + 0.05, L: o - 0.05, C: c, V: 1})
	eng := uptrendEngine(bars)
	for i := 0; i < 10; i++ {
		eng.imbs["TST"] = append(eng.imbs["TST"], 0.8)
		eng.walls["TST"] = append(eng.walls["TST"], 0.8)
	}
	sig := eng.EvalEntry("TST", 9999999)
	if sig.Type == Buy && (sig.Pattern == "bb_bounce" || sig.Pattern == "v_bottom" || sig.Pattern == "big_white_dip") {
		t.Fatalf("waterfall bounce must not fire wash-catchers, got %s (%s)", sig.Pattern, sig.Reason)
	}
}

func TestEngineRCITurnEntry(t *testing.T) {
	// 荒い揉み合い（帯を広げる）→緩やかな9連続下げ→小幅陽線復帰。
	var bars []Bar
	p := 100.0
	for i := 0; i < 30; i++ {
		d := 0.8
		if i%2 == 1 {
			d = -0.8
		}
		o := p
		c := o + d
		hi, lo := c+0.05, o-0.05
		if d < 0 {
			hi, lo = o+0.05, c-0.05
		}
		bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: o, H: hi, L: lo, C: c, V: 1})
		p = c
	}
	for i := 0; i < 9; i++ {
		o := p
		c := o - 0.25
		bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: o, H: o + 0.05, L: c - 0.05, C: c, V: 1})
		p = c
	}
	o := p + 0.05
	c := o + 0.4
	bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: o, H: c + 0.05, L: o - 0.05, C: c, V: 1})
	sig := uptrendEngine(bars).EvalEntry("TST", 9999999)
	if sig.Type != Buy {
		t.Fatalf("expected rci buy, got %s (%s) rsi=%.1f pb=%.2f hist=%.4f", sig.Type, sig.Reason, sig.RSI, sig.PercentB, sig.MACDHist)
	}
	if sig.Pattern != "rci_turn" {
		t.Fatalf("expected rci_turn, got %s (%s)", sig.Pattern, sig.Reason)
	}
}

func exitEngine(bars []Bar) *Engine {
	cfg := DefaultConfig()
	cfg.TrendTFSec = 0
	eng := NewEngine(cfg)
	for _, b := range bars {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
	}
	return eng
}

func TestExitOverheatAndWeak(t *testing.T) {
	// 過熱利確：逆張り entry の一直線上昇で RSI100・含み益 → rsi_overheat。
	eng := exitEngine(risingBars(40, 0.5))
	pos := OpenPos{Side: Buy, EntryPrice: 100, EntryTime: 1, Volume: 1, EntryRSI: 55, EntryPattern: "bb_bounce"}
	mid := 100 + 40*0.5
	if ok, r := eng.EvalExit("TST", pos, mid, 99999); !ok || r != "rsi_overheat" {
		t.Fatalf("expected rsi_overheat, got %v %q", ok, r)
	}
	// 失速手仕舞い：EntryRSI 60→45 割れ（含み損・指標なし）→ rsi_weak。
	var bars []Bar
	p := 110.0
	for i := 0; i < 40; i++ {
		d := -0.2
		if i%3 == 2 {
			d = 0.1
		}
		o := p
		c := o + d
		hi, lo := c+0.05, o-0.05
		if d < 0 {
			hi, lo = o+0.05, c-0.05
		}
		bars = append(bars, Bar{Start: int64(i) * 300000, O: o, H: hi, L: lo, C: c, V: 1})
		p = c
	}
	eng2 := exitEngine(bars)
	weak := OpenPos{Side: Buy, EntryPrice: 107, EntryTime: 1, Volume: 1, EntryRSI: 60}
	if ok, r := eng2.EvalExit("TST", weak, p, 99999); !ok || r != "rsi_weak" {
		t.Fatalf("expected rsi_weak, got %v %q", ok, r)
	}
}

func TestExitBandwalkSuppress(t *testing.T) {
	// 低RSI揉み合いからの小スパイク：pb>1 riding で売り圧があっても保持。
	var bars []Bar
	p := 100.0
	for i := 0; i < 35; i++ {
		d := 0.3
		if i%2 == 1 {
			d = -0.3
		}
		o := p
		c := o + d
		hi, lo := c+0.03, o-0.03
		if d < 0 {
			hi, lo = o+0.03, c-0.03
		}
		bars = append(bars, Bar{Start: int64(i) * 300000, O: o, H: hi, L: lo, C: c, V: 1})
		p = c
	}
	o := p
	c := o + 1.5
	bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: o, H: c + 0.03, L: o - 0.03, C: c, V: 1})
	eng := exitEngine(bars)
	for i := 0; i < 6; i++ {
		eng.Observe("TST", int64(i)*5000, c, 1, 9)
	}
	pos := OpenPos{Side: Buy, EntryPrice: 100, EntryTime: 1, Volume: 1, EntryRSI: 55, EntryPattern: "bb_breakout_up"}
	if ok, r := eng.EvalExit("TST", pos, c, 99999); ok {
		t.Fatalf("riding must suppress imbalance_flip, got exit %q", r)
	}
}
func TestEngineDojiHolds(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TrendTFSec = 0 // 単一足モード
	eng := NewEngine(cfg)
	bars := risingBars(40, 0.5)
	// 長いヒゲの十字線：実体0.2/全長4 → IsDoji。
	bars = append(bars, Bar{Start: int64(40) * 60000, O: 120, H: 122, L: 118, C: 120.2, V: 1})
	for _, b := range bars {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
	}
	sig := eng.EvalEntry("TST", 9999999)
	if sig.Type != Hold || sig.Reason != "doji indecision" {
		t.Fatalf("doji must hold, got %s (%s)", sig.Type, sig.Reason)
	}
}

func TestBreakoutExemptFromFlowVeto(t *testing.T) {
	// 締まったドリフト＋上抜け：逆フローでもブレイクは撃つ。
	mk := func() *Engine {
		cfg := DefaultConfig()
		cfg.TrendTFSec = 0
		return NewEngine(cfg)
	}
	var bars []Bar
	p := 100.0
	for i := 0; i < 40; i++ {
		o := p
		c := o + 0.1
		bars = append(bars, Bar{Start: int64(i) * 300000, O: o, H: c + 0.05, L: o - 0.05, C: c, V: 1})
		p = c
	}
	o := p
	c := o + 1.2
	bars = append(bars, Bar{Start: int64(40) * 300000, O: o, H: c + 0.05, L: o - 0.05, C: c, V: 1})
	eng := mk()
	for _, b := range bars {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
	}
	eng.imbs["TST"] = []float64{-0.8, -0.8, -0.8, -0.8, -0.8, -0.8, -0.8, -0.8, -0.8, -0.8}
	sig := eng.EvalEntry("TST", 9999999)
	if sig.Type != Buy || sig.Pattern != "bb_breakout_up" {
		t.Fatalf("breakout must ignore adverse flow, got %s %s (%s)", sig.Type, sig.Pattern, sig.Reason)
	}
	// 非免除パターン（明星）は逆フローで見送られる。
	eng2 := mk()
	for _, b := range morningStarEntry() {
		eng2.builderFor("TST").done = append(eng2.builderFor("TST").done, b)
	}
	eng2.imbs["TST"] = []float64{-0.8, -0.8, -0.8, -0.8, -0.8, -0.8, -0.8, -0.8, -0.8, -0.8}
	sig2 := eng2.EvalEntry("TST", 9999999)
	if sig2.Type != Hold || sig2.Reason != "against orderflow" {
		t.Fatalf("reversal must respect flow veto, got %s (%s)", sig2.Type, sig2.Reason)
	}
}

func TestParabolicSAR(t *testing.T) {
	// 単調上昇では SAR は下付きっぱなし（反転なし）。
	up := risingBars(30, 0.5)
	sar := ParabolicSAR(up, 0.02, 0.2)
	if len(sar) != len(up) {
		t.Fatalf("sar len = %d, want %d", len(sar), len(up))
	}
	if sar[len(sar)-1] >= up[len(up)-1].C {
		t.Fatalf("rising SAR must stay below: sar=%v close=%v", sar[len(sar)-1], up[len(up)-1].C)
	}
	if got := sarFlip(up, sar); got != 0 {
		t.Fatalf("steady rise must not flip, got %d", got)
	}
	// 急反発で上転換する。
	var bars []Bar
	p := 120.0
	for i := 0; i < 25; i++ {
		o := p
		c := o - 0.5
		bars = append(bars, Bar{Start: int64(i) * 300000, O: o, H: o + 0.05, L: c - 0.05, C: c, V: 1})
		p = c
	}
	o := p
	c := o + 2.0
	bars = append(bars, Bar{Start: int64(25) * 300000, O: o, H: c + 0.05, L: o - 0.05, C: c, V: 1})
	if got := sarFlip(bars, ParabolicSAR(bars, 0.02, 0.2)); got != 1 {
		t.Fatalf("V reversal must flip up, got %d", got)
	}
}

func TestEngineRciSarEntries(t *testing.T) {
	// 下に張り付いた RCI＋上転換で買い。上位は上昇固定。
	mkUp := func() *Engine {
		cfg := DefaultConfig()
		cfg.TrendTFSec = 3600
		eng := NewEngine(cfg)
		rising := risingBars(40, 0.5)
		for i := range rising {
			rising[i].Start = int64(i) * 3600000
		}
		for _, b := range rising {
			eng.trendFor("TST").done = append(eng.trendFor("TST").done, b)
		}
		return eng
	}
	var bars []Bar
	p := 120.0
	for i := 0; i < 26; i++ {
		o := p
		c := o - 0.5
		bars = append(bars, Bar{Start: int64(i) * 300000, O: o, H: o + 0.05, L: c - 0.05, C: c, V: 1})
		p = c
	}
	// 揉み4本で3足RCIを持ち上げ、5足minだけ底に残す。
	for _, d := range []float64{0.2, -0.1, 0.2, -0.1} {
		o := p
		c := o + d
		hi, lo := c+0.05, o-0.05
		if d < 0 {
			hi, lo = o+0.05, c-0.05
		}
		bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: o, H: hi, L: lo, C: c, V: 1})
		p = c
	}
	o := p
	c := o + 1.0
	bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: o + 0.05, H: c + 0.05, L: o - 0.05, C: c + 0.05, V: 1})
	eng := mkUp()
	for _, b := range bars {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
	}
	sig := eng.EvalEntry("TST", 9999999)
	// RCI底＋SAR転換では rci_sar が出る。rci_turn と条件が重なるため、
	// どちらかの RCI 反転買いが出ることをもって合格とする。
	if sig.Type != Buy || (sig.Pattern != "rci_sar" && sig.Pattern != "rci_turn") {
		t.Fatalf("expected rci bottom reversal buy, got %s %s (%s)", sig.Type, sig.Pattern, sig.Reason)
	}
	// 対称形：上に張り付いた RCI＋下転換で売り。
	mkDown := func() *Engine {
		cfg := DefaultConfig()
		cfg.TrendTFSec = 3600
		eng := NewEngine(cfg)
		falling := make([]Bar, 0, 40)
		q := 200.0
		for i := 0; i < 40; i++ {
			o := q
			c := q - 0.5
			falling = append(falling, Bar{Start: int64(i) * 3600000, O: o, H: o + 0.1, L: c - 0.1, C: c, V: 1})
			q = c
		}
		for _, b := range falling {
			eng.trendFor("TST").done = append(eng.trendFor("TST").done, b)
		}
		return eng
	}
	var dbars []Bar
	q := 100.0
	for i := 0; i < 30; i++ {
		o := q
		c := o + 0.4
		dbars = append(dbars, Bar{Start: int64(i) * 300000, O: o, H: c + 0.05, L: o - 0.05, C: c, V: 1})
		q = c
	}
	o = q
	c = o - 1.5
	dbars = append(dbars, Bar{Start: int64(30) * 300000, O: o - 0.05, H: o + 0.05, L: c - 0.05, C: c - 0.05, V: 1})
	eng2 := mkDown()
	for _, b := range dbars {
		eng2.builderFor("TST").done = append(eng2.builderFor("TST").done, b)
	}
	sig2 := eng2.EvalEntry("TST", 9999999)
	if sig2.Type != Sell || (sig2.Pattern != "rci_sar_down" && sig2.Pattern != "rci_turn_down") {
		t.Fatalf("expected rci top reversal sell, got %s %s (%s)", sig2.Type, sig2.Pattern, sig2.Reason)
	}
}

func tripleTopBars() []Bar {
	var bars []Bar
	for i := 0; i < 12; i++ {
		bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: 100, H: 100.1, L: 99.9, C: 100, V: 1})
	}
	p := 100.0
	mkpeak := func(top float64) {
		o := p
		bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: o, H: top + 0.1, L: o - 3, C: top - 0.5, V: 1})
		p = top - 0.5
		for i := 0; i < 5; i++ {
			o := p
			c := o - 0.4
			bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: o, H: o + 0.1, L: c - 0.1, C: c, V: 1})
			p = c
		}
	}
	mkpeak(110)
	mkpeak(112)
	mkpeak(111)
	// 小陽線バッファ（三兵・宵の明星の連鎖を断つ）→ネック割れの大陰線。
	bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: p, H: p + 0.35, L: p - 0.05, C: p + 0.3, V: 1})
	p += 0.3
	o := p
	c := o - 2.0
	bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: o - 0.05, H: o + 0.05, L: c - 0.05, C: c, V: 1})
	return bars
}

func TestEngineTripleTopEntry(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TrendTFSec = 0
	cfg.ShortAllow = []string{"triple_top"}
	eng := NewEngine(cfg)
	for _, b := range tripleTopBars() {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
	}
	sig := eng.EvalEntry("TST", 9999999)
	if sig.Type != Sell {
		t.Fatalf("expected triple_top sell, got %s (%s) rsi=%.1f", sig.Type, sig.Reason, sig.RSI)
	}
	if sig.Pattern != "triple_top" {
		t.Fatalf("expected triple_top, got %s (%s)", sig.Pattern, sig.Reason)
	}
}

func doubleBottomBars() []Bar {
	var bars []Bar
	p := 100.0
	for i := 0; i < 20; i++ {
		d := 0.3
		if i%3 == 2 {
			d = -0.1
		}
		o := p
		c := o + d
		hi, lo := c+0.05, o-0.05
		if d < 0 {
			hi, lo = o+0.05, c-0.05
		}
		bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: o, H: hi, L: lo, C: c, V: 1})
		p = c
	}
	mkvalley := func(drop float64) {
		o := p
		c := o - drop
		bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: o, H: o + 0.05, L: c - 0.05, C: c, V: 1})
		p = c
		for i := 0; i < 4; i++ {
			o := p
			c := o + 0.4
			bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: o, H: c + 0.05, L: o - 0.05, C: c, V: 1})
			p = c
		}
	}
	mkvalley(1.2)
	mkvalley(1.1)
	// ネック上抜けの陽線（包み・三兵を避けるためギャップ＋上ヒゲ）。
	o := p
	c := o + 1.2
	bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: o + 0.05, H: c + 1.0, L: o - 0.05, C: c + 0.05, V: 1})
	return bars
}

func TestEngineDoubleBottomEntry(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TrendTFSec = 3600
	eng := NewEngine(cfg)
	rising := risingBars(40, 0.5)
	for i := range rising {
		rising[i].Start = int64(i) * 3600000
	}
	for _, b := range rising {
		eng.trendFor("TST").done = append(eng.trendFor("TST").done, b)
	}
	for _, b := range doubleBottomBars() {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
	}
	sig := eng.EvalEntry("TST", 9999999)
	if sig.Type != Buy {
		t.Fatalf("expected double_bottom buy, got %s (%s) rsi=%.1f pb=%.2f", sig.Type, sig.Reason, sig.RSI, sig.PercentB)
	}
	if sig.Pattern != "double_bottom" {
		t.Fatalf("expected double_bottom, got %s (%s)", sig.Pattern, sig.Reason)
	}
}

func closesOf(bars []Bar) []float64 { return Closes(bars) }

func TestTopBottomHelpers(t *testing.T) {
	// 三尊：110→112→111 の3峰、中央最高、両端±1.5%以内。
	var bars []Bar
	for i := 0; i < 10; i++ {
		bars = append(bars, Bar{O: 100, H: 100.1, L: 99.9, C: 100, V: 1})
	}
	p := 100.0
	mkpeak := func(top float64) {
		o := p
		bars = append(bars, Bar{O: o, H: top + 0.1, L: o - 3, C: top - 0.5, V: 1})
		p = top - 0.5
		for i := 0; i < 4; i++ {
			o := p
			c := o - 0.8
			bars = append(bars, Bar{O: o, H: o + 0.1, L: c - 0.1, C: c, V: 1})
			p = c
		}
	}
	mkpeak(110)
	mkpeak(112)
	mkpeak(111)
	closes := closesOf(bars)
	neck, ok := tripleTopNeck(closes)
	if !ok {
		t.Fatal("expected triple top")
	}
	if neck <= 100 || neck >= 109.5 {
		t.Fatalf("bad neck %v", neck)
	}
	if _, ok := doubleTopNeck(closes); !ok {
		t.Fatal("expected double top on last two peaks")
	}
	// 単調上昇では不発火。
	if _, ok := tripleTopNeck(Closes(risingBars(40, 0.5))); ok {
		t.Fatal("rising must not be triple top")
	}
	// W底：逆向きの谷2つ。
	var dl []Bar
	for i := 0; i < 10; i++ {
		dl = append(dl, Bar{O: 120, H: 120.1, L: 119.9, C: 120, V: 1})
	}
	q := 120.0
	mkvalley := func(bot float64) {
		o := q
		dl = append(dl, Bar{O: o, H: o + 3, L: bot - 0.1, C: bot + 0.5, V: 1})
		q = bot + 0.5
		for i := 0; i < 4; i++ {
			o := q
			c := o + 0.8
			dl = append(dl, Bar{O: o, H: c + 0.1, L: o - 0.1, C: c, V: 1})
			q = c
		}
	}
	mkvalley(110)
	mkvalley(111)
	if _, ok := doubleBottomNeck(closesOf(dl)); !ok {
		t.Fatal("expected double bottom")
	}
	if _, ok := invHeadShoulderNeck(closesOf(dl)); ok {
		t.Fatal("two valleys must not be inv head shoulder")
	}
	// 上昇率・MAクロス鮮度。
	up := Closes(risingBars(40, 0.5))
	if got := risePct(up, 10); got < 0.02 {
		t.Fatalf("risePct = %v, want >= 0.02", got)
	}
	if dropPct(up, 5) != -risePct(up, 5) {
		t.Fatal("dropPct must mirror risePct")
	}
	// 体積急増の判定はヘルパー経由。
	if !isVolumeSurge(append(risingBars(20, 0.2), Bar{O: 104, H: 104.5, L: 103.9, C: 104.4, V: 5})) {
		t.Fatal("expected volume surge")
	}
	if isVolumeSurge(risingBars(20, 0.2)) {
		t.Fatal("flat volume must not surge")
	}
}

func TestLongShortDeny(t *testing.T) {
	// 実績除外（deny）は許可集合より優先し、空なら従来動作を保つ。
	// 出口の未加工反転には影響しない（別テストで保証）。
	mk := func(ld, sd []string) *Engine {
		cfg := DefaultConfig()
		cfg.TrendTFSec = 0
		cfg.LongDeny = ld
		cfg.ShortDeny = sd
		return NewEngine(cfg)
	}
	// ロング：deny なし → morning_star 発火。
	eng := mk(nil, nil)
	for _, b := range morningStarEntry() {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
	}
	if sig := eng.EvalEntry("TST", 9999999); sig.Type != Buy {
		t.Fatalf("must fire without deny, got %s (%s)", sig.Type, sig.Reason)
	}
	// ロング：deny あり → 不発火。
	eng2 := mk([]string{"morning_star"}, nil)
	for _, b := range morningStarEntry() {
		eng2.builderFor("TST").done = append(eng2.builderFor("TST").done, b)
	}
	if sig := eng2.EvalEntry("TST", 9999999); sig.Type != Hold || sig.Reason != "long pattern denied" {
		t.Fatalf("denied long must hold, got %s (%s)", sig.Type, sig.Reason)
	}
	// ショート：deny あり → 許可集合内でも不発火。
	breakdown := func() []Bar {
		var bars []Bar
		p := 120.0
		for i := 0; i < 20; i++ {
			o := p
			c := p - 0.5
			bars = append(bars, Bar{Start: int64(i) * 300000, O: o, H: o + 0.05, L: c - 0.05, C: c, V: 1})
			p = c
		}
		for i := 20; i < 40; i++ {
			c := p + float64(i%2)*0.01
			bars = append(bars, Bar{Start: int64(i) * 300000, O: p, H: c + 0.02, L: c - 0.02, C: c, V: 1})
		}
		bars = append(bars, Bar{Start: int64(40) * 300000, O: p, H: p + 0.02, L: p - 1.0, C: p - 0.9, V: 1})
		return bars
	}
	eng3 := mk(nil, []string{"bb_breakdown"})
	for _, b := range breakdown() {
		eng3.builderFor("TST").done = append(eng3.builderFor("TST").done, b)
	}
	if sig := eng3.EvalEntry("TST", 9999999); sig.Type != Hold || sig.Reason != "short pattern denied" {
		t.Fatalf("denied short must hold, got %s (%s)", sig.Type, sig.Reason)
	}
	// deny が空なら従来通り発火する。
	eng4 := mk(nil, nil)
	for _, b := range breakdown() {
		eng4.builderFor("TST").done = append(eng4.builderFor("TST").done, b)
	}
	if sig := eng4.EvalEntry("TST", 9999999); sig.Type != Sell {
		t.Fatalf("must fire without deny, got %s (%s)", sig.Type, sig.Reason)
	}
}

func TestShortAllowlist(t *testing.T) {
	if got := DefaultConfig().ShortAllow; len(got) != 18 {
		t.Fatalf("default short allowlist = %v, want 18 patterns", got)
	}
	if got := SplitPatterns("rci_turn_down,, bb_breakdown "); len(got) != 2 {
		t.Fatalf("split = %v, want 2", got)
	}
	// 下抜け形（20本凪＋下抜け）：許可集合なら発火する。
	mk := func() *Engine {
		cfg := DefaultConfig()
		cfg.TrendTFSec = 0
		return NewEngine(cfg)
	}
	breakdown := func() []Bar {
		var bars []Bar
		p := 120.0
		for i := 0; i < 20; i++ {
			o := p
			c := p - 0.5
			bars = append(bars, Bar{Start: int64(i) * 300000, O: o, H: o + 0.05, L: c - 0.05, C: c, V: 1})
			p = c
		}
		for i := 20; i < 40; i++ {
			c := p + float64(i%2)*0.01
			bars = append(bars, Bar{Start: int64(i) * 300000, O: p, H: c + 0.02, L: c - 0.02, C: c, V: 1})
		}
		bars = append(bars, Bar{Start: int64(40) * 300000, O: p, H: p + 0.02, L: p - 1.0, C: p - 0.9, V: 1})
		return bars
	}
	eng := mk()
	for _, b := range breakdown() {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
	}
	if sig := eng.EvalEntry("TST", 9999999); sig.Type != Sell || sig.Pattern != "bb_breakdown" {
		t.Fatalf("allowed breakdown must fire, got %s (%s)", sig.Type, sig.Reason)
	}
	// 許可外（空振り集合）では不発火、空集合では全許可。
	cfg := DefaultConfig()
	cfg.TrendTFSec = 0
	cfg.ShortAllow = []string{"morning_star"}
	eng2 := NewEngine(cfg)
	for _, b := range breakdown() {
		eng2.builderFor("TST").done = append(eng2.builderFor("TST").done, b)
	}
	if sig := eng2.EvalEntry("TST", 9999999); sig.Type != Hold || sig.Reason != "short pattern disabled" {
		t.Fatalf("disabled short must hold, got %s (%s)", sig.Type, sig.Reason)
	}
	cfg.ShortAllow = []string{}
	eng3 := NewEngine(cfg)
	for _, b := range breakdown() {
		eng3.builderFor("TST").done = append(eng3.builderFor("TST").done, b)
	}
	if sig := eng3.EvalEntry("TST", 9999999); sig.Type != Sell {
		t.Fatalf("empty allowlist must fire all, got %s (%s)", sig.Type, sig.Reason)
	}
}

func TestReversalMirrorsEntry(t *testing.T) {
	// 放置欠陥の回帰：許可外ショートパターンでは entry（EvalEntry）は
	// Hold になるが、exit の未加工判定（reversalExitPattern）は検出する。
	// 従来はフィルタ済み sig だけを見ていたため、許可外・レンジ帯の
	// 反転で買い玉が一生売られなかった（FCR/BCH/SOL放置）。
	mk := func(allow []string) *Engine {
		cfg := DefaultConfig()
		cfg.TrendTFSec = 0
		cfg.ShortAllow = allow
		return NewEngine(cfg)
	}
	breakdown := func() []Bar {
		var bars []Bar
		p := 120.0
		for i := 0; i < 20; i++ {
			o := p
			c := p - 0.5
			bars = append(bars, Bar{Start: int64(i) * 300000, O: o, H: o + 0.05, L: c - 0.05, C: c, V: 1})
			p = c
		}
		for i := 20; i < 40; i++ {
			c := p + float64(i%2)*0.01
			bars = append(bars, Bar{Start: int64(i) * 300000, O: p, H: c + 0.02, L: c - 0.02, C: c, V: 1})
		}
		bars = append(bars, Bar{Start: int64(40) * 300000, O: p, H: p + 0.02, L: p - 1.0, C: p - 0.9, V: 1})
		return bars
	}
	// 許可あり → entry は Sell。
	eng := mk(DefaultConfig().ShortAllow)
	for _, b := range breakdown() {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
	}
	if sig := eng.EvalEntry("TST", 9999999); sig.Type != Sell {
		t.Fatalf("allowed breakdown must fire, got %s (%s)", sig.Type, sig.Reason)
	}
	// 許可外 → entry は Hold（従来の出口経路では売れない）。
	eng2 := mk([]string{"morning_star"})
	for _, b := range breakdown() {
		eng2.builderFor("TST").done = append(eng2.builderFor("TST").done, b)
	}
	if sig := eng2.EvalEntry("TST", 9999999); sig.Type != Hold || sig.Reason != "short pattern disabled" {
		t.Fatalf("disabled short must hold, got %s (%s)", sig.Type, sig.Reason)
	}
	// 未加工の反転判定は許可集合に縛られず bb_breakdown を検出する。
	if pat, ok := eng2.reversalExitPattern("TST", Buy); !ok || pat != "bb_breakdown" {
		t.Fatalf("raw reversal must fire bb_breakdown, got %q %v", pat, ok)
	}
	// 売り玉側には反応しない。
	if pat, ok := eng2.reversalExitPattern("TST", Sell); ok {
		t.Fatalf("sell side must not see short reversal, got %q", pat)
	}
}

func TestExitReversalClosesStuckLong(t *testing.T) {
	// 微損の買い玉＋許可外に落ちうる反転でも EvalExit が signal で閉じる。
	// EntryRSI<50・含み損で指標系離脱（要含み益）・rsi_weak・板系の
	// いずれも発火しない死角を反転離脱が塞ぐ。
	cfg := DefaultConfig()
	cfg.TrendTFSec = 0                        // 上昇揉み合いで trend_flip を封じる（反転確定の単独検証）
	cfg.SLRatio = 0                           // SL を切って反転経路だけにする
	cfg.ShortAllow = []string{"morning_star"} // bb_breakdown を許可外にする
	eng := NewEngine(cfg)
	for _, b := range microClimb() {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
	}
	// 天井圏の連続大陰線で反転確定を検証する（幾何はプローブで確定済み）。
	q := 100.0 + 40*0.05
	pos := OpenPos{Side: Buy, EntryPrice: q, EntryTime: 1, Volume: 1,
		EntryRSI: 40, EntryPattern: "pullback_to_ma"}
	// 初発の反転では降ろさず印だけ付ける（単発ヒゲでの早売り防止）。
	eng.builderFor("TST").done = append(eng.builderFor("TST").done,
		Bar{Start: int64(40) * 300000, O: q, H: q + 0.05, L: q - 0.85, C: q - 0.8, V: 1})
	if pat, ok := eng.reversalExitPattern("TST", Buy); !ok {
		t.Fatalf("first black must match reversal, got %q %v", pat, ok)
	}
	if ok, r := eng.EvalExit("TST", pos, q-0.8, 99999); ok {
		t.Fatalf("first reversal must hold for confirmation, got exit %q", r)
	}
	// 別足で反転が続き、確証票が足りれば signal で閉じる。
	// （票要求なし設定のため、2足確定だけで発火する）
	eng.builderFor("TST").done = append(eng.builderFor("TST").done,
		Bar{Start: int64(41) * 300000, O: q - 0.8, H: q - 0.75, L: q - 1.65, C: q - 1.6, V: 1})
	if ok, r := eng.EvalExit("TST", pos, q-1.6, 99999); !ok || r != "signal" {
		t.Fatalf("confirmed reversal must exit with signal, got %v %q", ok, r)
	}
}

func TestExitReversalVotes(t *testing.T) {
	// 反転確定にも確証投票を要求する。票不足では連続反転でも保持する。
	mk := func(votes int) *Engine {
		cfg := DefaultConfig()
		cfg.TrendTFSec = 0
		cfg.SLRatio = 0
		cfg.ShortAllow = []string{"morning_star"}
		cfg.ExitConfirmVotes = votes
		eng := NewEngine(cfg)
		for _, b := range microClimb() {
			eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
		}
		return eng
	}
	q := 100.0 + 40*0.05
	pos := OpenPos{Side: Buy, EntryPrice: q, EntryTime: 1, Volume: 1,
		EntryRSI: 40, EntryPattern: "pullback_to_ma"}
	// 票5（到達不能）では連続反転でも保持する。
	engHold := mk(5)
	engHold.builderFor("TST").done = append(engHold.builderFor("TST").done,
		Bar{Start: int64(40) * 300000, O: q, H: q + 0.05, L: q - 0.85, C: q - 0.8, V: 1})
	if ok, r := engHold.EvalExit("TST", pos, q-0.8, 99999); ok {
		t.Fatalf("first must hold, got exit %q", r)
	}
	engHold.builderFor("TST").done = append(engHold.builderFor("TST").done,
		Bar{Start: int64(41) * 300000, O: q - 0.8, H: q - 0.75, L: q - 1.65, C: q - 1.6, V: 1})
	if ok, r := engHold.EvalExit("TST", pos, q-1.6, 99999); ok {
		t.Fatalf("starved votes must hold confirmed reversal, got exit %q", r)
	}
	// 票2では確定で発火する（MACD減速・反対足等の裏付けあり）。
	engFire := mk(2)
	engFire.builderFor("TST").done = append(engFire.builderFor("TST").done,
		Bar{Start: int64(40) * 300000, O: q, H: q + 0.05, L: q - 0.85, C: q - 0.8, V: 1})
	if ok, r := engFire.EvalExit("TST", pos, q-0.8, 99999); ok {
		t.Fatalf("first must hold, got exit %q", r)
	}
	engFire.builderFor("TST").done = append(engFire.builderFor("TST").done,
		Bar{Start: int64(41) * 300000, O: q - 0.8, H: q - 0.75, L: q - 1.65, C: q - 1.6, V: 1})
	if ok, r := engFire.EvalExit("TST", pos, q-1.6, 99999); !ok || r != "signal" {
		t.Fatalf("backed confirmation must exit, got %v %q", ok, r)
	}
}

func TestExitReversalDisarms(t *testing.T) {
	// 反転→陽線→反転では、途切れで解除されるため3手目も保持し、
	// 4手目の連続で初めて離脱する。
	cfg := DefaultConfig()
	cfg.TrendTFSec = 3600 // 上位は上昇に固定し trend_flip を封じる（解除ロジックの単独検証）
	cfg.SLRatio = 0       // SL を切って反転確定経路だけにする
	eng := NewEngine(cfg)
	rising := risingBars(40, 0.5)
	for i := range rising {
		rising[i].Start = int64(i) * 3600000
	}
	for _, b := range rising {
		eng.trendFor("TST").done = append(eng.trendFor("TST").done, b)
	}
	bars := microClimb()
	for _, b := range bars {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
	}
	q := bars[len(bars)-1].C
	black := func(start int64, base float64) Bar {
		return Bar{Start: start, O: base, H: base + 0.05, L: base - 0.85, C: base - 0.8, V: 1}
	}
	pos := OpenPos{Side: Buy, EntryPrice: q, EntryTime: 1, Volume: 1,
		EntryRSI: 40, EntryPattern: "pullback_to_ma"}
	if pat, ok := eng.reversalExitPattern("TST", Buy); ok {
		t.Fatalf("climb must not match reversal, got %q", pat)
	}
	// 1手目：大陰線で反転検出→保持（印）。
	eng.builderFor("TST").done = append(eng.builderFor("TST").done, black(int64(40)*300000, q))
	if pat, ok := eng.reversalExitPattern("TST", Buy); !ok {
		t.Fatalf("black bar must match reversal, got %q %v", pat, ok)
	}
	if ok, r := eng.EvalExit("TST", pos, q-0.8, 99999); ok {
		t.Fatalf("first reversal must hold, got exit %q", r)
	}
	// 2手目：陽線で途切れ→解除され保持。
	eng.builderFor("TST").done = append(eng.builderFor("TST").done,
		Bar{Start: int64(41) * 300000, O: q - 0.8, H: q - 0.4, L: q - 0.85, C: q - 0.5, V: 1})
	if ok, r := eng.EvalExit("TST", pos, q-0.5, 99999); ok {
		t.Fatalf("broken reversal must hold, got exit %q", r)
	}
	// 3手目：再び大陰線でも単発扱いで保持（解除が効いている証拠）。
	eng.builderFor("TST").done = append(eng.builderFor("TST").done, black(int64(42)*300000, q-0.5))
	if ok, r := eng.EvalExit("TST", pos, q-1.3, 99999); ok {
		t.Fatalf("re-armed reversal must hold, got exit %q", r)
	}
	// 4手目：連続で確定して離脱。
	eng.builderFor("TST").done = append(eng.builderFor("TST").done, black(int64(43)*300000, q-1.3))
	if ok, r := eng.EvalExit("TST", pos, q-2.1, 99999); !ok || r != "signal" {
		t.Fatalf("confirmed reversal must exit, got %v %q", ok, r)
	}
}

// microClimb は反転パターン不発の緩やかな上昇足（売り反転の裏付け用）。
// 小幅陽線の連続で、陰線型の売りパターン・MACDデッドクロス・RSI反転の
// いずれにも当たらない。買い玉の出口テストで他の離脱経路を塞ぐ土台。
func microClimb() []Bar {
	var bars []Bar
	q := 100.0
	for i := 0; i < 40; i++ {
		o := q
		c := q + 0.05
		bars = append(bars, Bar{Start: int64(i) * 300000, O: o, H: c + 0.05, L: o - 0.05, C: c, V: 1})
		q = c
	}
	return bars
}

func TestExitTrendFlip(t *testing.T) {
	// パターン不発でも上位足が明確に反転したら trend_flip で手仕舞う。
	cfg := DefaultConfig()
	cfg.TrendTFSec = 3600
	eng := NewEngine(cfg)
	bars := microClimb()
	for _, b := range bars {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
	}
	falling := risingBars(40, -0.5)
	for i := range falling {
		falling[i].Start = int64(i) * 3600000
	}
	for _, b := range falling {
		eng.trendFor("TST").done = append(eng.trendFor("TST").done, b)
	}
	// 土台確認：反転パターンは不発であること（trend_flip の単独検証）。
	if pat, ok := eng.reversalExitPattern("TST", Buy); ok {
		t.Fatalf("setup must not match reversal, got %q", pat)
	}
	lastC := bars[len(bars)-1].C
	pos := OpenPos{Side: Buy, EntryPrice: lastC, EntryTime: 1, Volume: 1,
		EntryRSI: 40, EntryPattern: "pullback_to_ma"}
	if ok, r := eng.EvalExit("TST", pos, lastC, 99999); !ok || r != "trend_flip" {
		t.Fatalf("trend flip must exit, got %v %q", ok, r)
	}
	// 順行トレンドでは出ない。
	eng2 := NewEngine(cfg)
	for _, b := range microClimb() {
		eng2.builderFor("TST").done = append(eng2.builderFor("TST").done, b)
	}
	rising := risingBars(40, 0.5)
	for i := range rising {
		rising[i].Start = int64(i) * 3600000
	}
	for _, b := range rising {
		eng2.trendFor("TST").done = append(eng2.trendFor("TST").done, b)
	}
	if ok, r := eng2.EvalExit("TST", pos, lastC, 99999); ok {
		t.Fatalf("aligned trend must hold, got exit %q", r)
	}
}

func TestExitMaxHoldSafetyNet(t *testing.T) {
	// 最終安全網：何も発火しない相場でも時間切れで必ず閉じる。
	// 旧既定（MaxHoldSec=0）では無期限放置になった。
	// 土台は反転不発の緩やかな上昇（TrendTFSec=0 で上位も同一＝trend_up、
	// trend_flip も出ない。損益ゼロ・EntryRSI 40 で他経路も塞ぐ）。
	mk := func(maxHold float64) *Engine {
		cfg := DefaultConfig()
		cfg.TrendTFSec = 0
		cfg.MaxHoldSec = maxHold
		eng := NewEngine(cfg)
		for _, b := range microClimb() {
			eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
		}
		return eng
	}
	b := microClimb()
	lastC := b[len(b)-1].C
	pos := OpenPos{Side: Buy, EntryPrice: lastC, EntryTime: 1000, Volume: 1,
		EntryRSI: 40, EntryPattern: "pullback_to_ma"}
	// 13時間保有＋12時間上限 → max_hold で閉じる。
	if ok, r := mk(43200).EvalExit("TST", pos, lastC, 1000+13*3600*1000); !ok || r != "max_hold" {
		t.Fatalf("stale open must time-stop, got %v %q", ok, r)
	}
	// 旧既定（無効）では閉じない＝放置が再現する。安全網の必要性の裏付け。
	if ok, r := mk(0).EvalExit("TST", pos, lastC, 1000+13*3600*1000); ok {
		t.Fatalf("disabled time-stop must hold (reproduces stuck bug), got exit %q", r)
	}
}

// atrClimb は高ボラの緩やかな上昇足（レンジ6・ATR約6）。
// 売り反転パターン不発・指標系離脱不発の土台で、SL/トレイルの
// 固定値と ATR 拡大の対比を検証する。
func atrClimb() []Bar {
	var bars []Bar
	q := 100.0
	for i := 0; i < 40; i++ {
		o := q
		c := q + 1.0
		bars = append(bars, Bar{Start: int64(i) * 300000, O: o, H: c + 2.5, L: o - 2.5, C: c, V: 1})
		q = c
	}
	return bars
}

func TestSLATRWidens(t *testing.T) {
	// ATR 拡大時は固定 2% より広い沫で耐え、深掘りでは切る。
	mk := func(slATR float64) *Engine {
		cfg := DefaultConfig()
		cfg.TrendTFSec = 0
		cfg.SLRatio = 0.02
		cfg.SLATR = slATR
		cfg.MaxHoldSec = 0
		eng := NewEngine(cfg)
		for _, b := range atrClimb() {
			eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
		}
		return eng
	}
	eng := mk(1.0)
	// 土台確認：反転パターンは不発（SL の単独検証）。
	if pat, ok := eng.reversalExitPattern("TST", Buy); ok {
		t.Fatalf("setup must not match reversal, got %q", pat)
	}
	lastC := 140.0
	pos := OpenPos{Side: Buy, EntryPrice: lastC, EntryTime: 1, Volume: 1,
		EntryRSI: 40, EntryPattern: "pullback_to_ma"}
	// -2.5% は ATR 有効時（しきい約4.3%）に保持される。
	if ok, r := eng.EvalExit("TST", pos, lastC*0.975, 99999); ok {
		t.Fatalf("ATR must ride through -2.5%% dip, got exit %q", r)
	}
	// -5% は ATR（しきい約4.3%）でも損切りする。
	if ok, r := eng.EvalExit("TST", pos, lastC*0.95, 99999); !ok || r != "stop_loss" {
		t.Fatalf("deep dip must stop, got %v %q", ok, r)
	}
	// 対照：ATR 無効（固定 2%）では -2.5% で切れる。
	if ok, r := mk(0).EvalExit("TST", pos, lastC*0.975, 99999); !ok || r != "stop_loss" {
		t.Fatalf("fixed 2%% must stop -2.5%% dip, got %v %q", ok, r)
	}
}

func TestTrailATRWidens(t *testing.T) {
	// 荒れ場では発動・押し戻し共に広げ、浅い押しでは降ろさない。
	// 固定値では 106 の押しで降りるが、ATR 有効時は 100 まで耐えて
	// 深い押しで trailing_stop する。
	mk := func(aATR, bATR float64) *Engine {
		cfg := DefaultConfig()
		cfg.TrendTFSec = 0
		cfg.TrailActivateR, cfg.TrailBackR = 0.015, 0.008
		cfg.TrailActivateATR, cfg.TrailBackATR = aATR, bATR
		eng := NewEngine(cfg)
		for _, b := range atrClimb() {
			eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
		}
		return eng
	}
	pos := OpenPos{Side: Buy, EntryPrice: 100, EntryTime: 1, Volume: 1,
		EntryRSI: 40, EntryPattern: "pullback_to_ma"}
	atr := mk(1.0, 1.0)
	if pat, ok := atr.reversalExitPattern("TST", Buy); ok {
		t.Fatalf("setup must not match reversal, got %q", pat)
	}
	fixed := mk(0, 0)
	mids := []float64{102, 107, 106}
	for _, m := range mids {
		if ok, r := atr.EvalExit("TST", pos, m, 99999); ok {
			t.Fatalf("ATR trail must hold at %.1f, got exit %q", m, r)
		}
	}
	// 固定値は 106 の押し（0.9%）で降りる。
	if ok, r := fixed.EvalExit("TST", pos, 102, 99999); ok {
		t.Fatalf("fixed must hold at peak 102, got exit %q", r)
	}
	if ok, _ := fixed.EvalExit("TST", pos, 107, 99999); ok {
		t.Fatal("fixed must hold at new peak 107")
	}
	if ok, r := fixed.EvalExit("TST", pos, 106, 99999); !ok || r != "trailing_stop" {
		t.Fatalf("fixed must trail-stop at 106, got %v %q", ok, r)
	}
	// ATR 有効は 100 の深押し（6.5%）で trailing_stop する。
	if ok, r := atr.EvalExit("TST", pos, 100, 99999); !ok || r != "trailing_stop" {
		t.Fatalf("ATR trail must stop at 100, got %v %q", ok, r)
	}
}

func TestImbalanceMinHold(t *testing.T) {
	// 板系離脱は最低保有秒数未満では発火しない（足未確定の即切り禁止）。
	// 売り圧5連続＋含み損でも、建玉10秒後は保持、400秒後は離脱する。
	mk := func(minHold float64) *Engine {
		cfg := DefaultConfig()
		cfg.ImbMinHoldSec = minHold
		eng := NewEngine(cfg)
		for i := 0; i < 6; i++ {
			eng.Observe("TST", int64(940000+i*5000), 100, 1, 9)
		}
		return eng
	}
	pos := OpenPos{Side: Buy, EntryPrice: 100, EntryTime: 1000000, Volume: 1}
	if ok, r := mk(300).EvalExit("TST", pos, 99, 1010000); ok {
		t.Fatalf("10s-old must hold through board noise, got exit %q", r)
	}
	if ok, r := mk(300).EvalExit("TST", pos, 99, 1400000); !ok || r != "imbalance_flip" {
		t.Fatalf("400s-old must flip, got %v %q", ok, r)
	}
	// 既定（0＝無効）では従来通り即離脱する。
	if ok, r := mk(0).EvalExit("TST", pos, 99, 1010000); !ok || r != "imbalance_flip" {
		t.Fatalf("disabled gate must flip immediately, got %v %q", ok, r)
	}
}

func TestExitConfirmVotes(t *testing.T) {
	// 弱い出口は確証票数で開閉する。票はトレンド逆行・MACD減速・
	// 強い反対足・板逆行の単純多数決。既定（0）は従来通り無条件。
	// 上位足は上昇固定で trend_flip を封じ、rsi_weak 経路を分離する。
	mkBars := func() []Bar {
		var bars []Bar
		q := 100.0
		for i := 0; i < 37; i++ {
			d := 0.3
			if i%2 == 1 {
				d = -0.3
			}
			o := q
			c := o + d
			hi, lo := c+0.05, o-0.05
			if d < 0 {
				hi, lo = o+0.05, c-0.05
			}
			bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: o, H: hi, L: lo, C: c, V: 1})
			q = c
		}
		// 終盤3本の下落で RSI を割り込ませ、MACD減速・反対足の票を作る。
		for i := 0; i < 3; i++ {
			o := q
			c := o - 0.4
			bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: o, H: o + 0.05, L: c - 0.05, C: c, V: 1})
			q = c
		}
		return bars
	}
	mk := func(votes int) *Engine {
		cfg := DefaultConfig()
		cfg.TrendTFSec = 3600
		cfg.ExitConfirmVotes = votes
		eng := NewEngine(cfg)
		rising := risingBars(40, 0.5)
		for i := range rising {
			rising[i].Start = int64(i) * 3600000
		}
		for _, b := range rising {
			eng.trendFor("TST").done = append(eng.trendFor("TST").done, b)
		}
		for _, b := range mkBars() {
			eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
		}
		return eng
	}
	bars := mkBars()
	lastC := bars[len(bars)-1].C
	pos := OpenPos{Side: Buy, EntryPrice: lastC, EntryTime: 1, Volume: 1,
		EntryRSI: 60, EntryPattern: "pullback_to_ma"}
	// 票不足（5票要求・最大4票）では rsi_weak でも保持する。
	if ok, r := mk(5).EvalExit("TST", pos, lastC, 99999); ok {
		t.Fatalf("starved votes must hold, got exit %q", r)
	}
	// 1票あれば発火する（MACD減速・反対足等の裏付けあり）。
	if ok, r := mk(1).EvalExit("TST", pos, lastC, 99999); !ok || r != "rsi_weak" {
		t.Fatalf("backed weak exit must fire, got %v %q", ok, r)
	}
	// 既定（0）は無条件に従来通り発火する。
	if ok, r := mk(0).EvalExit("TST", pos, lastC, 99999); !ok || r != "rsi_weak" {
		t.Fatalf("default must fire unconditionally, got %v %q", ok, r)
	}
}

func TestExitConfirmVotesImbalance(t *testing.T) {
	// 板系は板自体の票を持つため、閾値1では従来通り、2では裏付けを要する。
	// 足なし（cold）では他の票が立たないため決定論的。
	mk := func(votes int) *Engine {
		cfg := DefaultConfig()
		cfg.ExitConfirmVotes = votes
		eng := NewEngine(cfg)
		for i := 0; i < 6; i++ {
			eng.Observe("TST", int64(940000+i*5000), 100, 1, 9)
		}
		return eng
	}
	pos := OpenPos{Side: Buy, EntryPrice: 100, EntryTime: 1000000, Volume: 1,
		EntryRSI: 40, EntryPattern: "pullback_to_ma"}
	if ok, r := mk(2).EvalExit("TST", pos, 99, 1400000); ok {
		t.Fatalf("lone board vote must hold at threshold 2, got exit %q", r)
	}
	if ok, r := mk(1).EvalExit("TST", pos, 99, 1400000); !ok || r != "imbalance_flip" {
		t.Fatalf("board vote must fire at threshold 1, got %v %q", ok, r)
	}
}

func TestExitDenySoftExit(t *testing.T) {
	mk := func(deny []string) *Engine {
		cfg := DefaultConfig()
		cfg.ExitConfirmVotes = 1
		cfg.ExitDeny = deny
		eng := NewEngine(cfg)
		for i := 0; i < 6; i++ {
			eng.Observe("TST", int64(940000+i*5000), 100, 1, 9)
		}
		return eng
	}
	pos := OpenPos{Side: Buy, EntryPrice: 100, EntryTime: 1000000, Volume: 1,
		EntryRSI: 40, EntryPattern: "pullback_to_ma"}
	if ok, r := mk(nil).EvalExit("TST", pos, 99, 1400000); !ok || r != "imbalance_flip" {
		t.Fatalf("baseline must exit, got %v %q", ok, r)
	}
	if ok, r := mk([]string{"imbalance_flip"}).EvalExit("TST", pos, 99, 1400000); ok {
		t.Fatalf("denied soft exit must hold, got exit %q", r)
	}
	if ok, r := mk([]string{"imbalance_flip", "stop_loss"}).EvalExit("TST", pos, 90, 99999); !ok || r != "stop_loss" {
		t.Fatalf("hard exit must fire despite deny, got %v %q", ok, r)
	}
}

// flatBars は死んだ板の揉み合い足（100本・±0.02）。
func flatBars(n int) []Bar {
	var bars []Bar
	q := 100.0
	for i := 0; i < n; i++ {
		c := q
		if i%2 == 0 {
			c = q + 0.02
		} else {
			c = q - 0.02
		}
		bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: q, H: c + 0.03, L: c - 0.03, C: c, V: 1})
		q = c
	}
	return bars
}

func TestSqueezeRank(t *testing.T) {
	// 死んだ板はレンジ帯、上昇一途は非レンジ、足不足は不明。
	flat := NewEngine(DefaultConfig())
	for _, b := range flatBars(100) {
		flat.builderFor("TST").done = append(flat.builderFor("TST").done, b)
	}
	if !flat.squeezedEntry("TST") {
		t.Fatal("dead flat market must read as squeezed")
	}
	up := NewEngine(DefaultConfig())
	for _, b := range risingBars(100, 0.5) {
		up.builderFor("TST").done = append(up.builderFor("TST").done, b)
	}
	if up.squeezedEntry("TST") {
		t.Fatal("steady climb must not read as squeezed")
	}
	short := NewEngine(DefaultConfig())
	for _, b := range risingBars(40, 0.5) {
		short.builderFor("TST").done = append(short.builderFor("TST").done, b)
	}
	if short.squeezedEntry("TST") {
		t.Fatal("short history must fail open")
	}
}

// squeezeBase builds an engine with rising upper trend and 100 dead-flat
// entry bars (range regime on entry TF). Patterns fire only via breakout.
func squeezeBase() *Engine {
	cfg := DefaultConfig()
	cfg.TrendTFSec = 3600
	eng := NewEngine(cfg)
	rising := risingBars(40, 0.5)
	for i := range rising {
		rising[i].Start = int64(i) * 3600000
	}
	for _, b := range rising {
		eng.trendFor("TST").done = append(eng.trendFor("TST").done, b)
	}
	q := 100.0
	for i := 0; i < 100; i++ {
		c := q + 0.03
		if i%2 == 1 {
			c = q - 0.03
		}
		o := q
		hi, lo := c+0.03, o-0.03
		if c < o {
			hi, lo = o+0.03, c-0.03
		}
		eng.builderFor("TST").done = append(eng.builderFor("TST").done,
			Bar{Start: int64(len(eng.builderFor("TST").Bars())) * 300000, O: o, H: hi, L: lo, C: c, V: 1})
		q = c
	}
	return eng
}

// chopBars は広い方向なし揉み合い足（100本・±1.0往復・短ヒゲ）。
func chopBars(n int) []Bar {
	var bars []Bar
	q := 100.0
	for i := 0; i < n; i++ {
		c := q + 1.0
		if i%2 == 1 {
			c = q - 1.0
		}
		o := q
		hi, lo := c+0.02, o-0.02
		if c < o {
			hi, lo = o+0.02, c-0.02
		}
		bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: o, H: hi, L: lo, C: c, V: 1})
		q = c
	}
	return bars
}

func TestChoppyEntryBlocked(t *testing.T) {
	// 帯域は開いているが方向がない揉み合いはレンジ帯と判定する。
	chop := NewEngine(DefaultConfig())
	for _, b := range chopBars(100) {
		chop.builderFor("TST").done = append(chop.builderFor("TST").done, b)
	}
	if !chop.squeezedEntry("TST") {
		t.Fatal("wide directionless chop must read as range")
	}
	// 上昇一途（正味変位あり）は対象外のまま。
	up := NewEngine(DefaultConfig())
	for _, b := range risingBars(100, 0.5) {
		up.builderFor("TST").done = append(up.builderFor("TST").done, b)
	}
	if up.squeezedEntry("TST") {
		t.Fatal("steady climb must not read as range")
	}
}

// jumpChopBars は値飛びの往復揉み合い足（±0.5の揉み合い＋±5の往復
// スパイク＋小足8本）。スパイク直後はMAに残留が残るため、小足で
// 洗い流した後の静寂を判定対象にする。
func jumpChopBars() []Bar {
	var bars []Bar
	q := 100.0
	push := func(o, h, l, c float64) {
		bars = append(bars, Bar{Start: int64(len(bars)) * 300000, O: o, H: h, L: l, C: c, V: 1})
		q = c
	}
	for i := 0; i < 90; i++ {
		if i%2 == 0 {
			push(q, q+0.6, q-0.1, q+0.5)
		} else {
			push(q, q+0.1, q-0.6, q-0.5)
		}
	}
	push(q, q+5.2, q-0.1, q+5.0)
	push(q, q+0.1, q-5.2, q-5.0)
	for i := 0; i < 8; i++ {
		if i%2 == 0 {
			push(q, q+0.06, q-0.01, q+0.05)
		} else {
			push(q, q+0.01, q-0.06, q-0.05)
		}
	}
	return bars
}

func TestJumpChopBlocked(t *testing.T) {
	// 値飛びの往復はレンジ帯と判定する。一方向の連続拡大は対象外。
	eng := NewEngine(DefaultConfig())
	for _, b := range jumpChopBars() {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
	}
	if !eng.squeezedEntry("TST") {
		t.Fatal("jumpy round-trip chop must read as range")
	}
	// 一方向の連続拡大（正味変位大）は対象外のまま。
	up := NewEngine(DefaultConfig())
	for _, b := range risingBars(100, 0.5) {
		up.builderFor("TST").done = append(up.builderFor("TST").done, b)
	}
	if up.squeezedEntry("TST") {
		t.Fatal("directional expansion must not read as range")
	}
}

func TestSqueezeGateBlocksEntries(t *testing.T) {
	// レンジ帯では非ブレイク（包み線の買い）を見送り、ブレイクは撃つ。
	// ゲート無効時は同じ足で発火する（プローブで幾何確定済み）。
	n := int64(100)
	eng := squeezeBase()
	if !eng.squeezedEntry("TST") {
		t.Fatal("flat base must read as squeezed")
	}
	eng.builderFor("TST").done = append(eng.builderFor("TST").done,
		Bar{Start: n * 300000, O: 100.0, H: 100.05, L: 99.96, C: 100.045, V: 10})
	// 塵足（実体4.5bps）は絶対下限で止まるため squeeze 到達前に不発火になる。
	// squeeze gate 自体は後段の揉み合い・値飛びプローブで検証する。
	if sig := eng.EvalEntry("TST", 9999999); sig.Type != Hold {
		t.Fatalf("dust non-breakout in squeeze must hold, got %s (%s)", sig.Type, sig.Reason)
	}
	off := squeezeBase()
	off.cfg.SqueezeGate = false
	// ゲート無効時の発火確認には塵下限超えの実体を使う（塵足自体は絶対下限で止まる）。
	off.builderFor("TST").done = append(off.builderFor("TST").done,
		Bar{Start: n * 300000, O: 100.0, H: 100.07, L: 99.96, C: 100.06, V: 10})
	if sig := off.EvalEntry("TST", 9999999); sig.Type != Buy {
		t.Fatalf("disabled gate must fire, got %s (%s)", sig.Type, sig.Reason)
	}
	brk := squeezeBase()
	brk.builderFor("TST").done = append(brk.builderFor("TST").done,
		Bar{Start: n * 300000, O: 100.0, H: 102.1, L: 99.9, C: 102, V: 5})
	if sig := brk.EvalEntry("TST", 9999999); sig.Type != Buy || sig.Pattern != "bb_breakout_up" {
		t.Fatalf("breakout must fire through squeeze, got %s %s (%s)", sig.Type, sig.Pattern, sig.Reason)
	}
	// 広い揉み合いでも非ブレイクは見送り、ゲート無効時は発火する。
	chop := squeezeBaseChop()
	chop.builderFor("TST").done = append(chop.builderFor("TST").done,
		Bar{Start: int64(100) * 300000, O: 100.0, H: 101.05, L: 99.9, C: 101.0, V: 10})
	if sig := chop.EvalEntry("TST", 9999999); sig.Type != Hold || sig.Reason != "squeeze non-breakout" {
		t.Fatalf("non-breakout in wide chop must hold, got %s (%s)", sig.Type, sig.Reason)
	}
	chopOff := squeezeBaseChop()
	chopOff.cfg.SqueezeGate = false
	chopOff.builderFor("TST").done = append(chopOff.builderFor("TST").done,
		Bar{Start: int64(100) * 300000, O: 100.0, H: 101.05, L: 99.9, C: 101.0, V: 10})
	if sig := chopOff.EvalEntry("TST", 9999999); sig.Type != Buy {
		t.Fatalf("disabled gate must fire in wide chop, got %s (%s)", sig.Type, sig.Reason)
	}
	// 値飛びの往復では非ブレイクは見送る（ブレイクは通す）。
	// どのパターンが掛かっても、非ブレイクの買いが出ないことを保証する。
	mkJump := func(gate bool) *Engine {
		cfg := DefaultConfig()
		cfg.TrendTFSec = 3600
		cfg.SqueezeGate = gate
		eng := NewEngine(cfg)
		rising := risingBars(40, 0.5)
		for i := range rising {
			rising[i].Start = int64(i) * 3600000
		}
		for _, b := range rising {
			eng.trendFor("TST").done = append(eng.trendFor("TST").done, b)
		}
		for _, b := range jumpChopBars() {
			eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
		}
		return eng
	}
	jc := mkJump(true)
	jc.builderFor("TST").done = append(jc.builderFor("TST").done,
		Bar{Start: int64(100) * 300000, O: 100.0, H: 101.05, L: 99.9, C: 101.0, V: 10})
	if sig := jc.EvalEntry("TST", 9999999); sig.Type == Buy && !squeezeBreakout[sig.Pattern] {
		t.Fatalf("non-breakout must not fire after jumps, got %s (%s)", sig.Pattern, sig.Reason)
	}
	jcOff := mkJump(false)
	jcOff.builderFor("TST").done = append(jcOff.builderFor("TST").done,
		Bar{Start: int64(100) * 300000, O: 100.0, H: 101.05, L: 99.9, C: 101.0, V: 10})
	if sig := jcOff.EvalEntry("TST", 9999999); sig.Type != Buy {
		t.Fatalf("disabled gate must fire after jumps, got %s (%s)", sig.Type, sig.Reason)
	}
}

// squeezeBaseDown は下降上位足＋凪の入口足100本の土台を返す（売り側用）。
// 凪は±0.15往復・短ヒゲで、帯域（半幅約0.3）内側に実体が収まる
// 小陰線を作れる。出来高急増の売りでゲートの開閉を検証する。
func squeezeBaseDown() *Engine {
	cfg := DefaultConfig()
	cfg.TrendTFSec = 3600
	eng := NewEngine(cfg)
	falling := make([]Bar, 0, 40)
	p := 200.0
	for i := 0; i < 40; i++ {
		o := p
		c := p - 0.5
		falling = append(falling, Bar{Start: int64(i) * 3600000, O: o, H: o + 0.1, L: c - 0.1, C: c, V: 1})
		p = c
	}
	for _, b := range falling {
		eng.trendFor("TST").done = append(eng.trendFor("TST").done, b)
	}
	q := 100.0
	for i := 0; i < 100; i++ {
		c := q + 0.15
		if i%2 == 1 {
			c = q - 0.15
		}
		o := q
		hi, lo := c+0.01, o-0.01
		if c < o {
			hi, lo = o+0.01, c-0.01
		}
		eng.builderFor("TST").done = append(eng.builderFor("TST").done,
			Bar{Start: int64(len(eng.builderFor("TST").Bars())) * 300000, O: o, H: hi, L: lo, C: c, V: 1})
		q = c
	}
	return eng
}

func TestSqueezeGateShortSide(t *testing.T) {
	// 売り側も対称：凪では非ブレイクの売りを見送り、下抜けは撃つ。
	// ゲート無効時は同じ足で発火する。
	n := int64(100)
	eng := squeezeBaseDown()
	if !eng.squeezedEntry("TST") {
		t.Fatal("flat base must read as squeezed")
	}
	// 出来高急増の小陰線：ゲート有効時は見送り、無効時は発火する。
	// 帯域内に収まるため下抜け判定には掛からず、ゲートの開閉だけを検証する。
	eng.builderFor("TST").done = append(eng.builderFor("TST").done,
		Bar{Start: n * 300000, O: 100.05, H: 100.06, L: 99.97, C: 99.978, V: 10})
	if sig := eng.EvalEntry("TST", 9999999); sig.Type != Hold || sig.Reason != "squeeze non-breakout" {
		t.Fatalf("non-breakout short in squeeze must hold, got %s (%s)", sig.Type, sig.Reason)
	}
	off := squeezeBaseDown()
	off.cfg.SqueezeGate = false
	off.builderFor("TST").done = append(off.builderFor("TST").done,
		Bar{Start: n * 300000, O: 100.05, H: 100.06, L: 99.97, C: 99.978, V: 10})
	if sig := off.EvalEntry("TST", 9999999); sig.Type != Sell {
		t.Fatalf("disabled gate must fire short, got %s (%s)", sig.Type, sig.Reason)
	}
	// 下抜けはゲート有効でも発火する。
	brk := squeezeBaseDown()
	brk.builderFor("TST").done = append(brk.builderFor("TST").done,
		Bar{Start: n * 300000, O: 100.0, H: 100.05, L: 98.9, C: 98.0, V: 5})
	if sig := brk.EvalEntry("TST", 9999999); sig.Type != Sell || sig.Pattern != "bb_breakdown" {
		t.Fatalf("breakdown must fire through squeeze, got %s %s (%s)", sig.Type, sig.Pattern, sig.Reason)
	}
}

// squeezeBaseChop は上昇上位足＋広い揉み合い100本の土台を返す。
func squeezeBaseChop() *Engine {
	cfg := DefaultConfig()
	cfg.TrendTFSec = 3600
	eng := NewEngine(cfg)
	rising := risingBars(40, 0.5)
	for i := range rising {
		rising[i].Start = int64(i) * 3600000
	}
	for _, b := range rising {
		eng.trendFor("TST").done = append(eng.trendFor("TST").done, b)
	}
	for _, b := range chopBars(100) {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done, b)
	}
	return eng
}

func TestImbalanceNeedsConviction(t *testing.T) {
	// 符号の連続だけでは足りず、平均傾きの確信が必要。
	// 弱い偏り（-0.1級）の連続では保持し、強い偏りでは離脱する。
	mk := func(bid, ask float64) *Engine {
		eng := NewEngine(DefaultConfig())
		for i := 0; i < 6; i++ {
			eng.Observe("TST", int64(940000+i*5000), 100, bid, ask)
		}
		return eng
	}
	buyPos := OpenPos{Side: Buy, EntryPrice: 100, EntryTime: 1000000, Volume: 1,
		EntryRSI: 40, EntryPattern: "pullback_to_ma"}
	// 弱い売り並び（平均-0.1）では含み損でも保持する。
	if ok, r := mk(9, 11).EvalExit("TST", buyPos, 99, 1400000); ok {
		t.Fatalf("weak flow must hold, got exit %q", r)
	}
	// 売り玉の弱い買い並び（平均+0.1）でも保持する。
	sellPos := OpenPos{Side: Sell, EntryPrice: 100, EntryTime: 1000000, Volume: 1,
		EntryRSI: 60, EntryPattern: "pullback_to_ma_down"}
	if ok, r := mk(11, 9).EvalExit("TST", sellPos, 101, 1400000); ok {
		t.Fatalf("weak flow must hold shorts, got exit %q", r)
	}
	// 確信的な売り並び（平均-0.8）では従来通り離脱する。
	if ok, r := mk(1, 9).EvalExit("TST", buyPos, 99, 1400000); !ok || r != "imbalance_flip" {
		t.Fatalf("firm flow must flip, got %v %q", ok, r)
	}
	// 売り玉の確信的な買い並び（平均+0.8）でも対称に離脱する。
	sellStrong := OpenPos{Side: Sell, EntryPrice: 100, EntryTime: 1000000, Volume: 1,
		EntryRSI: 60, EntryPattern: "pullback_to_ma_down"}
	if ok, r := mk(9, 1).EvalExit("TST", sellStrong, 101, 1400000); !ok || r != "imbalance_flip" {
		t.Fatalf("firm flow must flip shorts, got %v %q", ok, r)
	}
}

func TestProbeSqueezeShort(t *testing.T) {
	mk := func() (*Engine, []Bar) {
		eng := squeezeBaseDown()
		var bars []Bar
		return eng, bars
	}
	_ = mk
	eng := squeezeBaseDown()
	base := append([]Bar{}, eng.builderFor("TST").Bars()...)
	closes := Closes(base)
	_, upper, lower := Bollinger(closes[len(closes)-20:], 20, 2.0)
	atr := ATR(base, 14)
	t.Logf("base n=%d lower=%.5f upper=%.5f atr=%.5f", len(base), lower, upper, atr)
	for _, dc := range []float64{0.06, 0.08, 0.09, 0.10} {
		c := 100.0 - dc
		e2 := squeezeBaseDown()
		e2.builderFor("TST").done = append(e2.builderFor("TST").done,
			Bar{Start: int64(100) * 300000, O: 100.0, H: 100.0 + 0.01, L: c - 0.01, C: c, V: 10})
		bb, _ := bbBreakout(append(append([]float64{}, closes...), c), 20, 2.0)
		_ = bb
		sig := e2.EvalEntry("TST", 9999999)
		t.Logf("C=%.3f sig=%s %s (%s)", c, sig.Type, sig.Pattern, sig.Reason)
	}
}

func TestProbeDipPullback(t *testing.T) {
	eng := squeezeBaseDown()
	q := 100.0
	base := len(eng.builderFor("TST").Bars())
	for i := 0; i < 5; i++ {
		o := q
		c := o - 0.3
		eng.builderFor("TST").done = append(eng.builderFor("TST").done,
			Bar{Start: int64(base+i) * 300000, O: o, H: o + 0.02, L: c - 0.02, C: c, V: 1})
		q = c
	}
	eng.builderFor("TST").done = append(eng.builderFor("TST").done,
		Bar{Start: int64(base+5) * 300000, O: q, H: q + 0.35, L: q - 0.02, C: q + 0.3, V: 1})
	q += 0.3
	eng.builderFor("TST").done = append(eng.builderFor("TST").done,
		Bar{Start: int64(base+6) * 300000, O: q, H: q + 0.02, L: q - 0.1, C: q - 0.08, V: 1})
	bars := eng.builderFor("TST").Bars()
	closes := Closes(bars)
	_, up, lo := Bollinger(closes, 20, 2.0)
	rsi := RSI(closes, 14)
	_, _, hist := MACD(closes, 12, 26, 9)
	_, _, prevHist := MACD(closes[:len(closes)-1], 12, 26, 9)
	maF, maS := SMA(closes, 5), SMA(closes, 20)
	bbMid := SMA(closes, 20)
	cur := bars[len(bars)-1]
	t.Logf("lastC=%.3f pb=%.3f rsi=%.1f hist=%.5f prevHist=%.5f maF=%.3f maS=%.3f bbMid=%.3f bull=%v bear=%v pullMA=%v pullBB=%v",
		closes[len(closes)-1], PercentB(closes[len(closes)-1], up, lo), rsi, hist, prevHist,
		maF, maS, bbMid, IsBullish(cur), IsBearish(cur),
		isPullbackToMAShort(bars, maF, maS), isPullbackToBBMidShort(bars, bbMid))
	sig := eng.EvalEntry("TST", 9999999)
	t.Logf("sig=%s %s (%s)", sig.Type, sig.Pattern, sig.Reason)
}

func TestExitRciFadeNeedsProof(t *testing.T) {
	// 微反転（3pt・MACD堅調）では保持、深い切り下げ＋MACD減速で離脱。
	mkBars := func(lastD float64) []Bar {
		var bars []Bar
		p := 100.0
		// 40本（MACD 12,26,9 が計算可能になる長さ。裏付け脚が評価できる）。
		for i := 0; i < 39; i++ {
			o := p
			c := o + 0.6
			bars = append(bars, Bar{Start: int64(len(bars)) * 60000, O: o, H: c + 0.05, L: o - 0.05, C: c, V: 1})
			p = c
		}
		o := p
		c := o + lastD
		hi, lo := c+0.05, o-0.05
		if lastD < 0 {
			hi, lo = o+0.05, c-0.05
		}
		bars = append(bars, Bar{Start: int64(len(bars)) * 60000, O: o, H: hi, L: lo, C: c, V: 1})
		return bars
	}
	pos := func(last float64) (OpenPos, float64) {
		entry := last * (1 - 0.003)
		return OpenPos{Side: Buy, EntryPrice: entry, EntryTime: 1, Volume: 1,
			EntryRSI: 60, EntryPattern: "bb_breakout_up"}, last
	}
	// 微反転 +0.45 継続 → 保持。
	bars := mkBars(0.45)
	last := bars[len(bars)-1].C
	p, mid := pos(last)
	if ok, r := exitEngine(bars).EvalExit("TST", p, mid, 99999); ok {
		t.Fatalf("micro turn must hold, got exit %q", r)
	}
	// 深い切り下げ -1.2 → rci_fade で離脱。
	bars = mkBars(-1.2)
	last = bars[len(bars)-1].C
	p, mid = pos(last)
	if ok, r := exitEngine(bars).EvalExit("TST", p, mid, 99999); !ok || r != "rci_fade" {
		t.Fatalf("expected rci_fade, got %v %q", ok, r)
	}
}

// boardBreakdownBase は上位上昇＋入口凪100本の土台に下抜け1本を載せる。
// LINK 9:45-9:50 型：上位は上昇のまま、5分足がスクイーズから下抜け＋
// MACDデッドクロス＋売り板持続でショートを撃つ対称オーバーライド用。
// 板フローは直接 imbs に積む（Observe だと上位足ビルダーを汚すため）。
// 凪は+0.03終わり（MACDをわずかに正に保ち、DCの跨ぎを確定させる）。
func boardBreakdownBase(flowBid, flowAsk float64) *Engine {
	cfg := DefaultConfig()
	cfg.TrendTFSec = 3600
	eng := NewEngine(cfg)
	rising := risingBars(40, 0.5)
	for i := range rising {
		rising[i].Start = int64(i) * 3600000
	}
	for _, b := range rising {
		eng.trendFor("TST").done = append(eng.trendFor("TST").done, b)
	}
	q := 100.0
	for i := 0; i < 100; i++ {
		c := q - 0.03
		if i%2 == 1 {
			c = q + 0.03
		}
		o := q
		hi, lo := c+0.03, o-0.03
		if c < o {
			hi, lo = o+0.03, c-0.03
		}
		eng.builderFor("TST").done = append(eng.builderFor("TST").done,
			Bar{Start: int64(len(eng.builderFor("TST").Bars())) * 300000, O: o, H: hi, L: lo, C: c, V: 1})
		q = c
	}
	// 下抜け足：帯を割り込む大陰線（bb_breakdown geometry 用）。
	eng.builderFor("TST").done = append(eng.builderFor("TST").done,
		Bar{Start: int64(100) * 300000, O: 100.0, H: 100.05, L: 98.9, C: 98.0, V: 5})
	// 板フロー：売り圧を持続させる（bid<ask）。tot>0 で imb を直接積む。
	// 近接板厚も対称に積む（override の wallAgree 用）。
	for i := 0; i < 10; i++ {
		tot := flowBid + flowAsk
		imb := 0.0
		if tot > 0 {
			imb = (flowBid - flowAsk) / tot
		}
		eng.imbs["TST"] = append(eng.imbs["TST"], imb)
		eng.walls["TST"] = append(eng.walls["TST"], imb)
	}
	return eng
}

// boardBreakoutBase は対称形：上位下降＋入口凪100本＋上抜け1本。
func boardBreakoutBase(flowBid, flowAsk float64) *Engine {
	cfg := DefaultConfig()
	cfg.TrendTFSec = 3600
	eng := NewEngine(cfg)
	falling := make([]Bar, 0, 40)
	p := 200.0
	for i := 0; i < 40; i++ {
		o := p
		c := p - 0.5
		falling = append(falling, Bar{Start: int64(i) * 3600000, O: o, H: o + 0.1, L: c - 0.1, C: c, V: 1})
		p = c
	}
	for _, b := range falling {
		eng.trendFor("TST").done = append(eng.trendFor("TST").done, b)
	}
	q := 100.0
	for i := 0; i < 100; i++ {
		c := q + 0.03
		if i%2 == 1 {
			c = q - 0.03
		}
		o := q
		hi, lo := c+0.03, o-0.03
		if c < o {
			hi, lo = o+0.03, c-0.03
		}
		eng.builderFor("TST").done = append(eng.builderFor("TST").done,
			Bar{Start: int64(len(eng.builderFor("TST").Bars())) * 300000, O: o, H: hi, L: lo, C: c, V: 1})
		q = c
	}
	eng.builderFor("TST").done = append(eng.builderFor("TST").done,
		Bar{Start: int64(100) * 300000, O: 100.0, H: 102.1, L: 99.9, C: 102, V: 5})
	for i := 0; i < 10; i++ {
		tot := flowBid + flowAsk
		imb := 0.0
		if tot > 0 {
			imb = (flowBid - flowAsk) / tot
		}
		eng.imbs["TST"] = append(eng.imbs["TST"], imb)
		eng.walls["TST"] = append(eng.walls["TST"], imb)
	}
	return eng
}

func TestBoardConfirmedBreakdownShort(t *testing.T) {
	// 上位上昇中でも、下抜け＋MACD DC＋売り板持続ならショートを撃つ。
	// 板なし（買い圧）では撃たない（誤射防止・回帰防止）。
	eng := boardBreakdownBase(1, 9)
	sig := eng.EvalEntry("TST", 9999999)
	if sig.Type != Sell || sig.Pattern != "bb_breakdown" {
		t.Fatalf("board-confirmed breakdown must short in uptrend, got %s %s (%s) rsi=%.1f pb=%.2f hist=%.4f flow=%d",
			sig.Type, sig.Pattern, sig.Reason, sig.RSI, sig.PercentB, sig.MACDHist, eng.flowDir("TST"))
	}
	// 板が逆（買い圧）なら見送る。
	eng2 := boardBreakdownBase(9, 1)
	sig2 := eng2.EvalEntry("TST", 9999999)
	if sig2.Type == Sell && sig2.Pattern == "bb_breakdown" {
		t.Fatalf("adverse board must not override, got %s %s (%s)", sig2.Type, sig2.Pattern, sig2.Reason)
	}
}

func TestBoardConfirmedBreakoutLong(t *testing.T) {
	// 対称形：上位下降中でも、上抜け＋MACD GC＋買い板持続ならロングを撃つ。
	eng := boardBreakoutBase(9, 1)
	sig := eng.EvalEntry("TST", 9999999)
	if sig.Type != Buy || sig.Pattern != "bb_breakout_up" {
		t.Fatalf("board-confirmed breakout must long in downtrend, got %s %s (%s)",
			sig.Type, sig.Pattern, sig.Reason)
	}
	eng2 := boardBreakoutBase(1, 9)
	sig2 := eng2.EvalEntry("TST", 9999999)
	if sig2.Type == Buy && sig2.Pattern == "bb_breakout_up" {
		t.Fatalf("adverse board must not override long, got %s %s (%s)", sig2.Type, sig2.Pattern, sig2.Reason)
	}
}

// strongSellBase は売り勝ち玉の強包み離脱用土台を返す。
// 40本下落→小陰線→大陽包み（実体2.5/レンジ3）。flowBid/flowAsk で板向きを指定する。
func strongSellBase(flowBid, flowAsk float64) *Engine {
	eng := NewEngine(DefaultConfig())
	p := 120.0
	for i := 0; i < 40; i++ {
		o := p
		c := p - 0.5
		eng.builderFor("TST").done = append(eng.builderFor("TST").done,
			Bar{Start: int64(i) * 300000, O: o, H: o + 0.05, L: c - 0.05, C: c, V: 1})
		p = c
	}
	// 小陰線（包まれる側）。
	eng.builderFor("TST").done = append(eng.builderFor("TST").done,
		Bar{Start: int64(40) * 300000, O: 100, H: 100.05, L: 99 - 0.05, C: 99, V: 1})
	// 大陽包み（実体2.5が前実体1.0を包む）。
	eng.builderFor("TST").done = append(eng.builderFor("TST").done,
		Bar{Start: int64(41) * 300000, O: 98.5, H: 101.05, L: 98.45, C: 101, V: 5})
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

// strongBuyBase は対称形：40本上昇→小陽線→大陰包み。
func strongBuyBase(flowBid, flowAsk float64) *Engine {
	eng := NewEngine(DefaultConfig())
	p := 100.0
	for i := 0; i < 40; i++ {
		o := p
		c := o + 0.5
		eng.builderFor("TST").done = append(eng.builderFor("TST").done,
			Bar{Start: int64(len(eng.builderFor("TST").Bars())) * 300000, O: o, H: c + 0.05, L: o - 0.05, C: c, V: 1})
		p = c
	}
	eng.builderFor("TST").done = append(eng.builderFor("TST").done,
		Bar{Start: int64(40) * 300000, O: 120, H: 121.05, L: 120 - 0.05, C: 121, V: 1})
	eng.builderFor("TST").done = append(eng.builderFor("TST").done,
		Bar{Start: int64(41) * 300000, O: 121.5, H: 121.55, L: 118.95, C: 119, V: 5})
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

func TestExitStrongReversalSellWinner(t *testing.T) {
	// 売り勝ち玉＋強陽包み＋買い板＋MACD反転なら単発で strong_reversal 離脱する。
	// 板逆（売り圧残）・含み損では出ない（誤射防止）。
	eng := strongSellBase(9, 1)
	pos := OpenPos{Side: Sell, EntryPrice: 120, EntryTime: 1, Volume: 1,
		EntryRSI: 43, EntryPattern: "bb_ride_momentum_down"}
	if ok, r := eng.EvalExit("TST", pos, 101, 99999); !ok || r != "strong_reversal" {
		t.Fatalf("strong bullish engulfing must exit winner short, got %v %q", ok, r)
	}
	// 板が売り残なら保持する。
	eng2 := strongSellBase(1, 9)
	if ok, r := eng2.EvalExit("TST", pos, 101, 99999); ok && r == "strong_reversal" {
		t.Fatalf("adverse board must not strong-exit, got %q", r)
	}
	// 含み損玉では strong_reversal しない（2本ルートに譲る）。
	loser := OpenPos{Side: Sell, EntryPrice: 100, EntryTime: 1, Volume: 1,
		EntryRSI: 43, EntryPattern: "bb_ride_momentum_down"}
	if ok, r := eng.EvalExit("TST", loser, 101, 99999); ok && r == "strong_reversal" {
		t.Fatalf("loser must not strong-exit, got %q", r)
	}
}

func TestExitStrongReversalBuyWinner(t *testing.T) {
	// 対称形：買い勝ち玉＋強陰包み＋売り板＋MACD反転なら単発離脱する。
	eng := strongBuyBase(1, 9)
	pos := OpenPos{Side: Buy, EntryPrice: 100, EntryTime: 1, Volume: 1,
		EntryRSI: 60, EntryPattern: "bb_breakout_up"}
	if ok, r := eng.EvalExit("TST", pos, 119, 99999); !ok || r != "strong_reversal" {
		t.Fatalf("strong bearish engulfing must exit winner long, got %v %q", ok, r)
	}
	eng2 := strongBuyBase(9, 1)
	if ok, r := eng2.EvalExit("TST", pos, 119, 99999); ok && r == "strong_reversal" {
		t.Fatalf("adverse board must not strong-exit long, got %q", r)
	}
	loser := OpenPos{Side: Buy, EntryPrice: 120, EntryTime: 1, Volume: 1,
		EntryRSI: 60, EntryPattern: "bb_breakout_up"}
	if ok, r := eng.EvalExit("TST", loser, 119, 99999); ok && r == "strong_reversal" {
		t.Fatalf("loser long must not strong-exit, got %q", r)
	}
}

func TestBarsContinuous(t *testing.T) {
	mk := func(starts ...int64) []Bar {
		var out []Bar
		for _, s := range starts {
			out = append(out, Bar{Start: s, O: 100, H: 100.1, L: 99.9, C: 100, V: 1})
		}
		return out
	}
	if !barsContinuous(mk(0, 300000, 600000), 300000) {
		t.Fatal("exact spacing must pass")
	}
	if !barsContinuous(mk(0, 60000, 120000), 300000) {
		t.Fatal("sub-width spacing must pass (hand-built test bars)")
	}
	if barsContinuous(mk(0, 300000, 900000), 300000) {
		t.Fatal("skipped slot must not pass")
	}
	if barsContinuous(mk(0, 300000), 300000) {
		t.Fatal("short history must not pass")
	}
	if barsContinuous(mk(0, 300000, 600000), 0) {
		t.Fatal("bad width must not pass")
	}
}

func TestWallAgree(t *testing.T) {
	mk := func(vs ...float64) *Engine {
		eng := NewEngine(DefaultConfig())
		eng.walls["TST"] = append([]float64{}, vs...)
		return eng
	}
	firmSell := []float64{-0.8, -0.8, -0.8, -0.8, -0.8, -0.8, -0.8, -0.8, -0.8, -0.8}
	if !mk(firmSell...).wallAgree("TST", -1) {
		t.Fatal("firm sell wall must agree short")
	}
	if mk(firmSell...).wallAgree("TST", 1) {
		t.Fatal("sell wall must not agree long")
	}
	if mk(-0.2, -0.2, -0.2, -0.2, -0.2).wallAgree("TST", -1) {
		t.Fatal("weak tilt (ATOM14:40 type) must not agree")
	}
	if mk().wallAgree("TST", -1) {
		t.Fatal("cold wall must not agree (fail-closed)")
	}
	if mk(-0.8, -0.8, 0.8, -0.8, -0.8).wallAgree("TST", -1) {
		t.Fatal("mixed signs must not agree")
	}
	firmBuy := []float64{0.7, 0.7, 0.7, 0.7, 0.7}
	if !mk(firmBuy...).wallAgree("TST", 1) {
		t.Fatal("firm buy wall must agree long")
	}
}

func TestFlowAgree(t *testing.T) {
	// 肯定投票：冷間は通す（fail-open）、弱確信は落とす、壁・テープいずれかの
	// 確信で通る。両side対称。
	cold := NewEngine(DefaultConfig())
	if !cold.flowAgree("TST", 1) || !cold.flowAgree("TST", -1) {
		t.Fatal("cold must fail open (no data kills nothing)")
	}
	weak := NewEngine(DefaultConfig())
	for i := 0; i < 10; i++ {
		weak.walls["TST"] = append(weak.walls["TST"], -0.2)
	}
	if weak.flowAgree("TST", -1) {
		t.Fatal("weak wall without tape must not agree (ATOM14:40 type)")
	}
	firm := NewEngine(DefaultConfig())
	for i := 0; i < 10; i++ {
		firm.walls["TST"] = append(firm.walls["TST"], -0.8)
	}
	if !firm.flowAgree("TST", -1) {
		t.Fatal("firm wall must agree")
	}
	if firm.flowAgree("TST", 1) {
		t.Fatal("sell wall must not agree long")
	}
	// テープ強確信が弱い壁を救う（live配線の検証も兼ねる）。
	taped := NewEngine(DefaultConfig())
	for i := 0; i < 10; i++ {
		taped.walls["TST"] = append(taped.walls["TST"], -0.2)
	}
	for i := 0; i < 50; i++ {
		taped.AddTape("TST", int64(i)*1000, 100, 9, false)
		taped.AddTape("TST", int64(i)*1000, 100, 1, true)
	}
	if !taped.flowAgree("TST", -1) {
		t.Fatal("strong tape must rescue weak wall")
	}
	if taped.flowAgree("TST", 1) {
		t.Fatal("sell tape must not agree long")
	}
}

func TestEntryHoldsOnTimeGap(t *testing.T) {
	// 板確認ブレイクの土台（上位上昇＋下抜け＋売り板）でも、直近に欠測があれば見送る。
	// ロング側も対称。
	eng := boardBreakdownBase(1, 9)
	for i := 0; i < 5; i++ {
		eng.walls["TST"] = append(eng.walls["TST"], -0.8)
	}
	bars := eng.builderFor("TST").Bars()
	bars[len(bars)-1].Start += 600000 // 最終足だけ2スロット飛ばす
	eng.builderFor("TST").done = bars
	if sig := eng.EvalEntry("TST", 9999999); sig.Type != Hold || sig.Reason != "time gap" {
		t.Fatalf("gapped breakdown must hold, got %s (%s)", sig.Type, sig.Reason)
	}
	eng2 := boardBreakoutBase(9, 1)
	for i := 0; i < 5; i++ {
		eng2.walls["TST"] = append(eng2.walls["TST"], 0.8)
	}
	bars2 := eng2.builderFor("TST").Bars()
	bars2[len(bars2)-1].Start += 600000
	eng2.builderFor("TST").done = bars2
	if sig := eng2.EvalEntry("TST", 9999999); sig.Type != Hold || sig.Reason != "time gap" {
		t.Fatalf("gapped breakout must hold, got %s (%s)", sig.Type, sig.Reason)
	}
}

// heatedFlatBase は加熱後の横ばい土台を返す（ATOM14:40型の回帰用）。
// 30本上昇でRSI/MACDを加熱した後に横ばいを続け、最終足をつぶす。
func heatedFlatBase(rises int, flats int, lastBody float64) *Engine {
	cfg := DefaultConfig()
	cfg.TrendTFSec = 0
	eng := NewEngine(cfg)
	p := 100.0
	for i := 0; i < rises; i++ {
		o := p
		c := p + 0.5
		eng.builderFor("TST").done = append(eng.builderFor("TST").done,
			Bar{Start: int64(len(eng.builderFor("TST").Bars())) * 300000, O: o, H: c + 0.05, L: o - 0.05, C: c, V: 1})
		p = c
	}
	for i := 0; i < flats; i++ {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done,
			Bar{Start: int64(len(eng.builderFor("TST").Bars())) * 300000, O: p, H: p + 0.02, L: p - 0.02, C: p, V: 1})
	}
	if lastBody != 0 {
		o := p
		c := p + lastBody
		hi, lo := c+0.05, o-0.05
		if lastBody < 0 {
			hi, lo = o+0.05, c-0.05
		}
		eng.builderFor("TST").done = append(eng.builderFor("TST").done,
			Bar{Start: int64(len(eng.builderFor("TST").Bars())) * 300000, O: o, H: hi, L: lo, C: c, V: 1})
	} else {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done,
			Bar{Start: int64(len(eng.builderFor("TST").Bars())) * 300000, O: p, H: p, L: p, C: p, V: 1})
	}
	for i := 0; i < 10; i++ {
		eng.imbs["TST"] = append(eng.imbs["TST"], -0.8)
		eng.walls["TST"] = append(eng.walls["TST"], -0.8)
	}
	return eng
}

func TestThresholdFamilyHoldsOnDeadBar(t *testing.T) {
	// ATOM14:40型：加熱後のつぶれ足（レンジ0）では閾値系6パターンを出さない。
	// 板厚が確信的でも方向・実体なしでは発火しない（対称に買い側も）。
	eng := heatedFlatBase(30, 5, 0)
	sig := eng.EvalEntry("TST", 9999999)
	if sig.Type == Sell && (sig.Pattern == "overheat_macd" || sig.Pattern == "macd_cross_down" || sig.Pattern == "macd_cross_down_high" || sig.Pattern == "rsi_fade") {
		t.Fatalf("dead flat bar must not fire threshold shorts, got %s (%s)", sig.Pattern, sig.Reason)
	}
	eng2 := heatedFlatBase(30, 5, 0)
	for i := range eng2.walls["TST"] {
		eng2.walls["TST"][i] = 0.8
	}
	for i := range eng2.imbs["TST"] {
		eng2.imbs["TST"][i] = 0.8
	}
	sig2 := eng2.EvalEntry("TST", 9999999)
	if sig2.Type == Buy && (sig2.Pattern == "oversold_macd" || sig2.Pattern == "macd_cross" || sig2.Pattern == "macd_cross_deep" || sig2.Pattern == "rsi_recover") {
		t.Fatalf("dead flat bar must not fire threshold longs, got %s (%s)", sig2.Pattern, sig2.Reason)
	}
}

// waterfallEngulfBase は滝途中のデッドキャット抱き線の土台を返す（ETH23:20型）。
// 上位足は順行（買い側なら上昇）に固定し、入口足だけが加速下落→小反発する。
// 板は順行厚固定だが、MACD/RCIはなお逆行中のため新ゲートでは抱き線を出さない。
// down=true で売り側の鏡像（上位下降＋加速上昇中の戻り抱き線）を返す。
func waterfallEngulfBase(down bool) *Engine {
	cfg := DefaultConfig()
	cfg.TrendTFSec = 3600
	eng := NewEngine(cfg)
	if !down {
		rising := risingBars(40, 0.5)
		for i := range rising {
			rising[i].Start = int64(i) * 3600000
		}
		for _, b := range rising {
			eng.trendFor("TST").done = append(eng.trendFor("TST").done, b)
		}
	} else {
		falling := make([]Bar, 0, 40)
		p := 200.0
		for i := 0; i < 40; i++ {
			o := p
			c := p - 0.5
			falling = append(falling, Bar{Start: int64(i) * 3600000, O: o, H: o + 0.1, L: c - 0.1, C: c, V: 1})
			p = c
		}
		for _, b := range falling {
			eng.trendFor("TST").done = append(eng.trendFor("TST").done, b)
		}
	}
	p := 100.0
	push := func(o, h, l, c float64) {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done,
			Bar{Start: int64(len(eng.builderFor("TST").Bars())) * 300000, O: o, H: h, L: l, C: c, V: 1})
	}
	if !down {
		for i := 0; i < 30; i++ {
			d := 0.5
			if i%3 == 2 {
				d = -0.4
			}
			o := p
			c := o + d
			hi, lo := c+0.05, o-0.05
			if d < 0 {
				hi, lo = o+0.05, c-0.05
			}
			push(o, hi, lo, c)
			p = c
		}
		for _, d := range []float64{-2.5, -2.5} {
			o := p
			c := o + d
			push(o, o+0.05, c-0.05, c)
			p = c
		}
		o := p
		c := o - 0.5
		push(o, o+0.05, c-0.05, c)
		p = c
		o = p - 0.2
		c = p + 1.2
		push(o, c+0.05, o-0.05, c)
		for i := 0; i < 10; i++ {
			eng.imbs["TST"] = append(eng.imbs["TST"], 0.8)
			eng.walls["TST"] = append(eng.walls["TST"], 0.8)
		}
		return eng
	}
	for i := 0; i < 30; i++ {
		d := -0.5
		if i%3 == 2 {
			d = 0.4
		}
		o := p
		c := o + d
		hi, lo := c+0.05, o-0.05
		if d < 0 {
			hi, lo = o+0.05, c-0.05
		}
		push(o, hi, lo, c)
		p = c
	}
	for _, d := range []float64{2.5, 2.5} {
		o := p
		c := o + d
		push(o, c+0.05, o-0.05, c)
		p = c
	}
	o := p
	c := o + 0.5
	push(o, c+0.05, o-0.05, c)
	p = c
	o = p + 0.2
	c = p - 1.2
	push(o, o+0.05, c-0.05, c)
	for i := 0; i < 10; i++ {
		eng.imbs["TST"] = append(eng.imbs["TST"], -0.8)
		eng.walls["TST"] = append(eng.walls["TST"], -0.8)
	}
	return eng
}

func TestWaterfallEngulfingHolds(t *testing.T) {
	// ETH23:20型：加速下落中のデッドキャット陽包みでは bullish_engulfing を出さない。
	// 板厚があってもMACD/RCIが下向きのままなら見送る（対称に売り側も）。
	eng := waterfallEngulfBase(false)
	sig := eng.EvalEntry("TST", 9999999)
	t.Logf("waterfall long: type=%s pattern=%s reason=%s regime=%s", sig.Type, sig.Pattern, sig.Reason, sig.Regime)
	if sig.Type == Buy && sig.Pattern == "bullish_engulfing" {
		t.Fatalf("waterfall dead-cat must not fire engulfing long, got %s (%s)", sig.Pattern, sig.Reason)
	}
	eng2 := waterfallEngulfBase(true)
	sig2 := eng2.EvalEntry("TST", 9999999)
	t.Logf("waterfall short: type=%s pattern=%s reason=%s regime=%s", sig2.Type, sig2.Pattern, sig2.Reason, sig2.Regime)
	if sig2.Type == Sell && sig2.Pattern == "bearish_engulfing" {
		t.Fatalf("mirror dead-cat must not fire engulfing short, got %s (%s)", sig2.Pattern, sig2.Reason)
	}
}

func TestNearLow20(t *testing.T) {
	mkCloses := func(base float64, n int, lastC float64) []float64 {
		out := make([]float64, 0, n+1)
		for i := 0; i < n; i++ {
			out = append(out, base)
		}
		return append(out, lastC)
	}
	if !nearLow20(mkCloses(100, 20, 100.4), 100.4) {
		t.Fatal("0.4% above low must count as near low")
	}
	if nearLow20(mkCloses(100, 20, 100.7), 100.7) {
		t.Fatal("0.7% above low must not count as near low")
	}
	if nearLow20(mkCloses(100, 10, 100.1), 100.1) {
		t.Fatal("short history must not count")
	}
	if nearLow20(mkCloses(100, 20, 0), 0) {
		t.Fatal("zero price must not count")
	}
}

func TestDustBarHoldsBreakdown(t *testing.T) {
	// LINK04:15型：凪の塵ブレイクは絶対下限で止める。
	// 極小横ばい（±0.005）なら帯も極小化し、実体0.04でも帯割れの幾何は成立する。
	// 旧条件（ATR比のみ）では発火するが、新条件（価格比5bps）で止まる。
	eng := boardBreakdownBase(1, 9)
	for i := 0; i < 5; i++ {
		eng.walls["TST"] = append(eng.walls["TST"], -0.8)
	}
	var tight []Bar
	q := 100.0
	for i := 0; i < 100; i++ {
		c := q + 0.005
		if i%2 == 1 {
			c = q - 0.005
		}
		o := q
		hi, lo := c+0.005, o-0.005
		if c < o {
			hi, lo = o+0.005, c-0.005
		}
		tight = append(tight, Bar{Start: int64(len(tight)) * 300000, O: o, H: hi, L: lo, C: c, V: 1})
		q = c
	}
	o := q
	c := q - 0.04
	tight = append(tight, Bar{Start: int64(len(tight)) * 300000, O: o, H: o + 0.01, L: c - 0.01, C: c, V: 5})
	eng.builderFor("TST").done = tight
	sig := eng.EvalEntry("TST", 9999999)
	if sig.Type == Sell && sig.Pattern == "bb_breakdown" {
		t.Fatalf("dust breakdown must not fire, got %s (%s)", sig.Pattern, sig.Reason)
	}
}

func TestHighBlackProximity(t *testing.T) {
	// 高値0.7%下の陰線は high_black を出さない（0.5%境界）。
	// 対称の安値近接は TestNearLow20 で検証する。
	eng := squeezeBaseDown()
	eng.cfg.SqueezeGate = false
	for i := 0; i < 10; i++ {
		eng.imbs["TST"] = append(eng.imbs["TST"], -0.8)
		eng.walls["TST"] = append(eng.walls["TST"], -0.8)
	}
	base := eng.builderFor("TST").Bars()
	hi := base[0].C
	for _, b := range base {
		if b.C > hi {
			hi = b.C
		}
	}
	lastC := hi * (1 - 0.007)
	eng.builderFor("TST").done = append(eng.builderFor("TST").done,
		Bar{Start: int64(100) * 300000, O: lastC + 1.0, H: lastC + 1.05, L: lastC - 0.05, C: lastC, V: 10})
	sig := eng.EvalEntry("TST", 9999999)
	if sig.Type == Sell && sig.Pattern == "high_black" {
		t.Fatalf("0.7%%-off-high must not fire high_black, got %s (%s)", sig.Pattern, sig.Reason)
	}
}

func TestMMState(t *testing.T) {
	// MM体制分類：壁・テープの確信集計。両side対称・PF分離キー。
	mk := func() *Engine { return NewEngine(DefaultConfig()) }
	if got := mk().mmState("TST"); got != 0 {
		t.Fatalf("cold must be balanced, got %d", got)
	}
	if got := mk().mmState("TST"); got != 0 {
		t.Fatalf("empty must be balanced, got %d", got)
	}
	buy := mk()
	for i := 0; i < 10; i++ {
		buy.walls["TST"] = append(buy.walls["TST"], 0.8)
	}
	if got := buy.mmState("TST"); got != 1 {
		t.Fatalf("firm buy wall must accumulate, got %d", got)
	}
	sell := mk()
	for i := 0; i < 10; i++ {
		sell.walls["TST"] = append(sell.walls["TST"], -0.8)
	}
	if got := sell.mmState("TST"); got != -1 {
		t.Fatalf("firm sell wall must distribute, got %d", got)
	}
	weak := mk()
	for i := 0; i < 10; i++ {
		weak.walls["TST"] = append(weak.walls["TST"], -0.2)
	}
	if got := weak.mmState("TST"); got != 0 {
		t.Fatalf("weak tilt must be balanced, got %d", got)
	}
	taped := mk()
	for i := 0; i < 50; i++ {
		taped.AddTape("TST", int64(i)*1000, 100, 9, true)
		taped.AddTape("TST", int64(i)*1000, 100, 1, false)
	}
	if got := taped.mmState("TST"); got != 1 {
		t.Fatalf("buy tape must accumulate, got %d", got)
	}
	if n := mmStateName(1); n != "accumulate" {
		t.Fatalf("name(1) = %q", n)
	}
	if n := mmStateName(-1); n != "distribute" {
		t.Fatalf("name(-1) = %q", n)
	}
	if n := mmStateName(0); n != "balanced" {
		t.Fatalf("name(0) = %q", n)
	}
}

func TestTrendWith(t *testing.T) {
	eng := NewEngine(DefaultConfig())
	if eng.trendWith("TST", Buy) || eng.trendWith("TST", Sell) {
		t.Fatal("cold trend must not align (fast exit side)")
	}
	rising := risingBars(40, 0.5)
	for i := range rising {
		rising[i].Start = int64(i) * 3600000
	}
	for _, b := range rising {
		eng.trendFor("TST").done = append(eng.trendFor("TST").done, b)
	}
	if !eng.trendWith("TST", Buy) {
		t.Fatal("rising upper must align long")
	}
	if eng.trendWith("TST", Sell) {
		t.Fatal("rising upper must not align short")
	}
}

func TestWallRunover(t *testing.T) {
	// 壁の逆行加速では含み損玉を早期離脱する（両side対称・勝ち玉対象外）。
	// 足なし（Observeのみ）で評価し、他出口との干渉を避ける。
	mkFlow := func(bid, ask float64) *Engine {
		eng := NewEngine(DefaultConfig())
		for i := 0; i < 6; i++ {
			eng.Observe("TST", int64(940000+i*5000), 100, bid, ask)
		}
		return eng
	}
	buyPos := OpenPos{Side: Buy, EntryPrice: 100, EntryTime: 1000000, Volume: 1,
		EntryRSI: 40, EntryPattern: "pullback_to_ma"}
	eng := mkFlow(9, 1)
	for _, w := range []float64{0.5, 0.3, 0.1, -0.1} {
		eng.AddWall("TST", w)
	}
	if ok, r := eng.EvalExit("TST", buyPos, 99, 1400000); !ok || r != "wall_runover" {
		t.Fatalf("draining buy wall must exit loser long, got %v %q", ok, r)
	}
	// 上昇壁では保持する。
	up := mkFlow(9, 1)
	for _, w := range []float64{-0.5, -0.3, -0.1, 0.1} {
		up.AddWall("TST", w)
	}
	if ok, r := up.EvalExit("TST", buyPos, 99, 1400000); ok && r == "wall_runover" {
		t.Fatalf("recovering wall must not runover long, got %q", r)
	}
	// 勝ち玉では発火しない。
	if ok, r := eng.EvalExit("TST", buyPos, 101, 1400000); ok && r == "wall_runover" {
		t.Fatalf("winner must not runover, got %q", r)
	}
	// 売り対称形。
	sellPos := OpenPos{Side: Sell, EntryPrice: 100, EntryTime: 1000000, Volume: 1,
		EntryRSI: 60, EntryPattern: "pullback_to_ma_down"}
	seng := mkFlow(1, 9)
	for _, w := range []float64{-0.5, -0.3, -0.1, 0.1} {
		seng.AddWall("TST", w)
	}
	if ok, r := seng.EvalExit("TST", sellPos, 101, 1400000); !ok || r != "wall_runover" {
		t.Fatalf("rising ask wall must exit loser short, got %v %q", ok, r)
	}
}

func TestChopBreakoutHolds(t *testing.T) {
	// ADA17-19時型：MACD反転の繰り返す揉み合いでの上抜けは追わない。
	// ブレイク・ride・新高値・divのいずれも保ち合い時は不発火にする（対称）。
	cfg := DefaultConfig()
	cfg.TrendTFSec = 0
	eng := NewEngine(cfg)
	p := 100.0
	push := func(o, h, l, c float64) {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done,
			Bar{Start: int64(len(eng.builderFor("TST").Bars())) * 300000, O: o, H: h, L: l, C: c, V: 1})
	}
	for i := 0; i < 40; i++ {
		d := 3.0
		if i%2 == 1 {
			d = -3.0
		}
		o := p
		c := o + d
		hi, lo := c+0.05, o-0.05
		if d < 0 {
			hi, lo = o+0.05, c-0.05
		}
		push(o, hi, lo, c)
		p = c
	}
	o := p
	c := o + 2.5
	push(o, c+0.05, o-0.05, c)
	closes := Closes(eng.builderFor("TST").Bars())
	t.Logf("chop flips=%d", HistFlips(closes, 12, 26, 9, 5))
	sig := eng.EvalEntry("TST", 9999999)
	if sig.Type == Buy && (sig.Pattern == "bb_breakout_up" || sig.Pattern == "new_high" || sig.Pattern == "bb_ride_momentum" || sig.Pattern == "rsi_div" || sig.Pattern == "macd_div") {
		t.Fatalf("choppy breakout must not fire momentum longs, got %s (%s)", sig.Pattern, sig.Reason)
	}
}

// expansionBase は収縮なしの急落土台を返す（BTC22:55型）。
// 横ばい後の急落で直近20本安値を更新する。板は売り厚固定。
// DC・板・実体・保ち合い少数の全点灯で regime を抜けて撃つ。
func expansionBase() *Engine {
	cfg := DefaultConfig()
	cfg.TrendTFSec = 3600
	eng := NewEngine(cfg)
	rising := risingBars(40, 0.5)
	for i := range rising {
		rising[i].Start = int64(i) * 3600000
	}
	for _, b := range rising {
		eng.trendFor("TST").done = append(eng.trendFor("TST").done, b)
	}
	p := 100.0
	push := func(o, h, l, c float64) {
		eng.builderFor("TST").done = append(eng.builderFor("TST").done,
			Bar{Start: int64(len(eng.builderFor("TST").Bars())) * 300000, O: o, H: h, L: l, C: c, V: 1})
	}
	for i := 0; i < 100; i++ {
		c := p - 0.03
		if i%2 == 1 {
			c = p + 0.03
		}
		o := p
		hi, lo := c+0.03, o-0.03
		if c < o {
			hi, lo = o+0.03, c-0.03
		}
		push(o, hi, lo, c)
		p = c
	}
	o := p
	c := o - 3.0
	push(o, o+0.05, c-0.05, c)
	for i := 0; i < 10; i++ {
		eng.imbs["TST"] = append(eng.imbs["TST"], -0.8)
		eng.walls["TST"] = append(eng.walls["TST"], -0.8)
	}
	return eng
}

func TestExpansionBreakdownOverride(t *testing.T) {
	// 収縮なしの急落でも、帯下抜け＋拡大＋DC＋板確信なら regime を抜いて撃つ。
	eng := expansionBase()
	bars := eng.builderFor("TST").Bars()
	closes := Closes(bars)
	_, _, hist := MACD(closes, 12, 26, 9)
	_, _, prevHist := MACD(closes[:len(closes)-1], 12, 26, 9)
	t.Logf("expansion probe: brokeLow=%v hist=%.3f prevHist=%.3f",
		brokeLow20(closes, closes[len(closes)-1]), hist, prevHist)
	sig := eng.EvalEntry("TST", 9999999)
	if sig.Type != Sell || sig.Pattern != "bb_breakdown" {
		t.Fatalf("expansion breakdown must short against upper trend, got %s %s (%s)",
			sig.Type, sig.Pattern, sig.Reason)
	}
	// 板が逆（買い厚）なら見送る。
	eng2 := expansionBase()
	for i := range eng2.walls["TST"] {
		eng2.walls["TST"][i] = 0.8
	}
	for i := range eng2.imbs["TST"] {
		eng2.imbs["TST"][i] = 0.8
	}
	if sig2 := eng2.EvalEntry("TST", 9999999); sig2.Type == Sell && sig2.Pattern == "bb_breakdown" {
		t.Fatalf("adverse board must block expansion short, got %s (%s)", sig2.Type, sig2.Reason)
	}
}

func TestWickDetect(t *testing.T) {
	// 上ヒゲ拒否：上ヒゲ2倍・下ヒゲ以下・実体あり。対称に下ヒゲも。
	up := Bar{O: 100, H: 104.5, L: 99.8, C: 101}
	if !isUpperWick(up, 1.0, 100) {
		t.Fatal("long upper wick must detect")
	}
	if isLowerWick(up, 1.0, 100) {
		t.Fatal("upper wick must not read as lower")
	}
	// 下ヒゲ長大は上ヒゲ拒否にならない。
	downwig := Bar{O: 101, H: 101.2, L: 96.5, C: 100}
	if isUpperWick(downwig, 1.0, 100) {
		t.Fatal("lower-heavy bar must not read as upper wick")
	}
	if !isLowerWick(downwig, 1.0, 100) {
		t.Fatal("long lower wick must detect")
	}
	// 塵足は対象外。
	dust := Bar{O: 100, H: 100.03, L: 99.99, C: 100.01}
	if isUpperWick(dust, 1.0, 100) || isLowerWick(dust, 1.0, 100) {
		t.Fatal("dust must not count as wick")
	}
	if isUpperWick(up, 0, 100) || isUpperWick(up, 1.0, 0) {
		t.Fatal("zero atr/price must not detect")
	}
	eng := NewEngine(DefaultConfig())
	eng.wickHi["TST"] = 1000000
	if !eng.wickFreshHi("TST", 1000000+3600000) {
		t.Fatal("1h-old wick must be fresh")
	}
	if eng.wickFreshHi("TST", 1000000+7*3600000) {
		t.Fatal("7h-old wick must expire")
	}
	if eng.wickFreshLo("TST", 1000000) {
		t.Fatal("unset wick must not be fresh")
	}
}

// wickArmedBase は上位上昇＋加熱後の大陰線の土台を返す（初動用）。
// RCI張り付き＋陰線＋売り板でヒゲ確認つきなら regime を抜けて撃つ。
func wickArmedBase() *Engine {
	cfg := DefaultConfig()
	cfg.TrendTFSec = 3600
	eng := NewEngine(cfg)
	rising := risingBars(40, 0.5)
	for i := range rising {
		rising[i].Start = int64(i) * 3600000
	}
	for _, b := range rising {
		eng.trendFor("TST").done = append(eng.trendFor("TST").done, b)
	}
	p := 100.0
	for i := 0; i < 30; i++ {
		o := p
		c := o + 0.5
		eng.builderFor("TST").done = append(eng.builderFor("TST").done,
			Bar{Start: int64(len(eng.builderFor("TST").Bars())) * 300000, O: o, H: c + 0.05, L: o - 0.05, C: c, V: 1})
		p = c
	}
	o := p
	c := o - 2.0
	eng.builderFor("TST").done = append(eng.builderFor("TST").done,
		Bar{Start: int64(len(eng.builderFor("TST").Bars())) * 300000, O: o, H: o + 0.05, L: c - 0.05, C: c, V: 5})
	for i := 0; i < 10; i++ {
		eng.imbs["TST"] = append(eng.imbs["TST"], -0.8)
		eng.walls["TST"] = append(eng.walls["TST"], -0.8)
	}
	eng.wickHi["TST"] = 9999999 - 3600000
	return eng
}

func TestWickConfirmedTopTurn(t *testing.T) {
	// ヒゲ確認＋RCI反転＋板確信なら上昇中でもショートする。
	eng := wickArmedBase()
	sig := eng.EvalEntry("TST", 9999999)
	if sig.Type != Sell || sig.Pattern != "rci_turn_down" {
		t.Fatalf("wick-confirmed top must short, got %s %s (%s)", sig.Type, sig.Pattern, sig.Reason)
	}
	// ヒゲなしでは出ない。
	cold := wickArmedBase()
	delete(cold.wickHi, "TST")
	if sig := cold.EvalEntry("TST", 9999999); sig.Type == Sell && sig.Pattern == "rci_turn_down" {
		t.Fatalf("wickless top must hold, got %s (%s)", sig.Type, sig.Reason)
	}
	// 板逆（買い厚）では出ない。
	adverse := wickArmedBase()
	for i := range adverse.walls["TST"] {
		adverse.walls["TST"][i] = 0.8
	}
	for i := range adverse.imbs["TST"] {
		adverse.imbs["TST"][i] = 0.8
	}
	if sig := adverse.EvalEntry("TST", 9999999); sig.Type == Sell && sig.Pattern == "rci_turn_down" {
		t.Fatalf("adverse board must block wick short, got %s (%s)", sig.Type, sig.Reason)
	}
}

// rideBase は一直線の3連続陰線の土台を返す（継続用）。
// 上位上昇＋定常上昇後の3連続下落で、安値更新＋板売り厚つき。
func rideBase() *Engine {
	cfg := DefaultConfig()
	cfg.TrendTFSec = 3600
	eng := NewEngine(cfg)
	rising := risingBars(40, 0.5)
	for i := range rising {
		rising[i].Start = int64(i) * 3600000
	}
	for _, b := range rising {
		eng.trendFor("TST").done = append(eng.trendFor("TST").done, b)
	}
	p := 100.0
	for i := 0; i < 40; i++ {
		o := p
		c := o + 0.3
		eng.builderFor("TST").done = append(eng.builderFor("TST").done,
			Bar{Start: int64(len(eng.builderFor("TST").Bars())) * 300000, O: o, H: c + 0.05, L: o - 0.05, C: c, V: 1})
		p = c
	}
	for i := 0; i < 3; i++ {
		o := p
		c := o - 2.5
		eng.builderFor("TST").done = append(eng.builderFor("TST").done,
			Bar{Start: int64(len(eng.builderFor("TST").Bars())) * 300000, O: o, H: o + 0.05, L: c - 0.05, C: c, V: 2})
		p = c
	}
	for i := 0; i < 10; i++ {
		eng.imbs["TST"] = append(eng.imbs["TST"], -0.8)
		eng.walls["TST"] = append(eng.walls["TST"], -0.8)
	}
	return eng
}

func TestRideContinuationOverride(t *testing.T) {
	// ride形＋板確信＋安値更新なら上昇中でも乗り始める。
	eng := rideBase()
	bars := eng.builderFor("TST").Bars()
	closes := Closes(bars)
	_, upper, lower := Bollinger(closes, 20, 2.0)
	_, _, hist := MACD(closes, 12, 26, 9)
	_, _, prevHist := MACD(closes[:len(closes)-1], 12, 26, 9)
	t.Logf("ride probe: pb=%.3f rsi=%.1f hist=%.3f prevHist=%.3f brokeLow=%v flips=%d flow=%d",
		PercentB(closes[len(closes)-1], upper, lower), RSI(closes, 14), hist, prevHist,
		brokeLow20(closes, closes[len(closes)-1]), HistFlips(closes, 12, 26, 9, 5), eng.flowDir("TST"))
	sig := eng.EvalEntry("TST", 9999999)
	if sig.Type != Sell || sig.Pattern != "bb_ride_momentum_down" {
		t.Fatalf("confirmed ride must short, got %s %s (%s)", sig.Type, sig.Pattern, sig.Reason)
	}
	// 板なし（買い厚）では出ない。
	adverse := rideBase()
	for i := range adverse.walls["TST"] {
		adverse.walls["TST"][i] = 0.8
	}
	for i := range adverse.imbs["TST"] {
		adverse.imbs["TST"][i] = 0.8
	}
	if sig := adverse.EvalEntry("TST", 9999999); sig.Type == Sell && sig.Pattern == "bb_ride_momentum_down" {
		t.Fatalf("adverse board must block ride short, got %s (%s)", sig.Type, sig.Reason)
	}
}

func TestConfirmedTopBypass(t *testing.T) {
	// 除外3種の迂回はヒゲ＋板確信の両立でのみ開く。
	eng := NewEngine(DefaultConfig())
	eng.wickHi["TST"] = 9999999 - 3600000
	for i := 0; i < 10; i++ {
		eng.walls["TST"] = append(eng.walls["TST"], -0.8)
	}
	if !eng.confirmedTopBypass("TST", 9999999, "bearish_engulfing") {
		t.Fatal("wick plus board must bypass")
	}
	if !eng.confirmedTopBypass("TST", 9999999, "evening_star") {
		t.Fatal("evening star must be bypassable")
	}
	if !eng.confirmedTopBypass("TST", 9999999, "macd_cross_down") {
		t.Fatal("dead cross must be bypassable")
	}
	if eng.confirmedTopBypass("TST", 9999999, "three_crows") {
		t.Fatal("non-target must not bypass")
	}
	delete(eng.wickHi, "TST")
	if eng.confirmedTopBypass("TST", 9999999, "bearish_engulfing") {
		t.Fatal("wickless must not bypass")
	}
	eng.wickHi["TST"] = 9999999 - 3600000
	for i := range eng.walls["TST"] {
		eng.walls["TST"][i] = 0.8
	}
	if eng.confirmedTopBypass("TST", 9999999, "bearish_engulfing") {
		t.Fatal("adverse board must not bypass")
	}
}

// textbookTopBase は教科書の天井売り土台を返す。
// 上位上昇＋加熱上昇＋大陽線→大陰包み（ヒゲ確認・売り板つき）。
func textbookTopBase() *Engine {
	cfg := DefaultConfig()
	cfg.TrendTFSec = 3600
	eng := NewEngine(cfg)
	rising := risingBars(40, 0.5)
	for i := range rising {
		rising[i].Start = int64(i) * 3600000
	}
	for _, b := range rising {
		eng.trendFor("TST").done = append(eng.trendFor("TST").done, b)
	}
	p := 100.0
	for i := 0; i < 30; i++ {
		o := p
		c := o + 0.5
		if i%2 == 1 {
			c = o - 0.4
		}
		eng.builderFor("TST").done = append(eng.builderFor("TST").done,
			Bar{Start: int64(len(eng.builderFor("TST").Bars())) * 300000, O: o, H: c + 0.05, L: o - 0.05, C: c, V: 1})
		p = c
	}
	o := p
	c := o + 0.8
	eng.builderFor("TST").done = append(eng.builderFor("TST").done,
		Bar{Start: int64(len(eng.builderFor("TST").Bars())) * 300000, O: o, H: c + 0.05, L: o - 0.05, C: c, V: 1})
	p = c
	o = p + 0.1
	c = o - 1.8
	eng.builderFor("TST").done = append(eng.builderFor("TST").done,
		Bar{Start: int64(len(eng.builderFor("TST").Bars())) * 300000, O: o, H: o + 0.05, L: c - 0.05, C: c, V: 5})
	for i := 0; i < 10; i++ {
		eng.imbs["TST"] = append(eng.imbs["TST"], -0.8)
		eng.walls["TST"] = append(eng.walls["TST"], -0.8)
	}
	eng.wickHi["TST"] = 9999999 - 3600000
	return eng
}

func TestTextbookTopSell(t *testing.T) {
	// 教科書シーケンス：上位上昇中の大陽包み陰線＋ヒゲ＋板確信で売る。
	// 許可制除外でも迂回して発火すること（初動の取り逃がし防止）。
	eng := textbookTopBase()
	sig := eng.EvalEntry("TST", 9999999)
	if sig.Type != Sell || sig.Pattern != "bearish_engulfing" {
		t.Fatalf("textbook top must sell engulfing, got %s %s (%s)", sig.Type, sig.Pattern, sig.Reason)
	}
	// ヒゲなしでは許可制どおり不発火。
	cold := textbookTopBase()
	delete(cold.wickHi, "TST")
	if sig := cold.EvalEntry("TST", 9999999); sig.Type == Sell && sig.Pattern == "bearish_engulfing" {
		t.Fatalf("wickless top must hold, got %s (%s)", sig.Type, sig.Reason)
	}
}

// breakoutBase は保ち合い＋上抜けサージの土台を返す。
// 天井追いfail-closed（板確信）の検証用。board=trueで買い板確信つき、
// falseで板はあるが無確信（本番の確信なし状態。完全冷間は別経路で担保）。
func breakoutBase(board bool) *Engine {
	cfg := DefaultConfig()
	cfg.TrendTFSec = 3600
	eng := NewEngine(cfg)
	rising := risingBars(40, 0.5)
	for i := range rising {
		rising[i].Start = int64(i) * 3600000
	}
	for _, b := range rising {
		eng.trendFor("TST").done = append(eng.trendFor("TST").done, b)
	}
	p := 100.0
	for i := 0; i < 45; i++ {
		o := p
		c := o + 0.35
		if i%2 == 1 {
			c = o - 0.25
		}
		eng.builderFor("TST").done = append(eng.builderFor("TST").done,
			Bar{Start: int64(len(eng.builderFor("TST").Bars())) * 300000, O: o, H: c + 0.05, L: o - 0.05, C: c, V: 1})
		p = c
	}
	o := p
	c := o + 2.0
	eng.builderFor("TST").done = append(eng.builderFor("TST").done,
		Bar{Start: int64(len(eng.builderFor("TST").Bars())) * 300000, O: o, H: c + 0.05, L: o - 0.05, C: c, V: 5})
	if board {
		for i := 0; i < 10; i++ {
			eng.imbs["TST"] = append(eng.imbs["TST"], 0.8)
			eng.walls["TST"] = append(eng.walls["TST"], 0.8)
		}
	} else {
		for i := 0; i < 10; i++ {
			eng.imbs["TST"] = append(eng.imbs["TST"], 0.0)
			eng.walls["TST"] = append(eng.walls["TST"], 0.0)
		}
	}
	return eng
}

func TestBreakoutNeedsBoard(t *testing.T) {
	// 板確信＋MA合意があればブレイク発火し、発火マークが記録される。
	eng := breakoutBase(true)
	sig := eng.EvalEntry("TST", 9999999)
	if sig.Type != Buy || sig.Pattern != "bb_breakout_up" {
		t.Fatalf("confirmed breakout must fire, got %s %s (%s)", sig.Type, sig.Pattern, sig.Reason)
	}
	m, ok := eng.expFire["TST"]
	if !ok || m.dir != 1 {
		t.Fatalf("expansion fire must be recorded, got %+v %v", m, ok)
	}
	// 板なしでは天井追い（breakout/ride）は撃たない（fail-closed）。
	bare := breakoutBase(false)
	sig = bare.EvalEntry("TST", 9999999)
	if sig.Type == Buy && (sig.Pattern == "bb_breakout_up" || sig.Pattern == "bb_ride_momentum") {
		t.Fatalf("boardless chase must not fire, got %s (%s)", sig.Pattern, sig.Reason)
	}
}

func TestExpansionCooling(t *testing.T) {
	// 直前に同方向の拡大が発火し、進捗なしなら見送る。
	eng := breakoutBase(true)
	lastPx := eng.builderFor("TST").Bars()
	px := lastPx[len(lastPx)-1].C
	n := len(lastPx)
	eng.recordExpansion("TST", 1, px, n)
	if sig := eng.EvalEntry("TST", 9999999); sig.Type != Hold || sig.Reason != "expansion cooling" {
		t.Fatalf("repeat expansion must cool, got %s %s (%s)", sig.Type, sig.Pattern, sig.Reason)
	}
	// 逆方向は止めない。
	if eng.expansionBlocked("TST", -1, px, n+1) {
		t.Fatal("opposite direction must not cool")
	}
	// 0.3%以上順行すれば解除される。
	if eng.expansionBlocked("TST", 1, px*1.005, n+1) {
		t.Fatal("progress must release cooling")
	}
	// 48バー経過すれば解除される。
	if eng.expansionBlocked("TST", 1, px, n+expCooldownBarsDefault) {
		t.Fatal("aged mark must release cooling")
	}
}

func TestTrackWall(t *testing.T) { // 壁数量履歴が積まれ、不正値は捨てられ、上限で古いものから落ちる。
	eng := NewEngine(DefaultConfig())
	for i := 0; i < 200; i++ {
		eng.TrackWall("TST", int64(1000+i), 5.0, 7.0, 100.0)
	}
	hist := eng.wallLv["TST"]
	if len(hist) != wallLevelKeep {
		t.Fatalf("wall history must cap at %d, got %d", wallLevelKeep, len(hist))
	}
	if hist[0].TS != 1020 || hist[len(hist)-1].TS != 1199 {
		t.Fatalf("wall history must slide window, got %d..%d", hist[0].TS, hist[len(hist)-1].TS)
	}
	before := len(hist)
	eng.TrackWall("TST", 1200, -1, 7.0, 100.0)
	eng.TrackWall("TST", 1200, 5.0, 7.0, 0)
	if len(eng.wallLv["TST"]) != before {
		t.Fatal("invalid wall levels must be dropped")
	}
}

func TestDistributing(t *testing.T) {
	// 上昇＋売り約定優勢＝売り上がり分布。逆も対称。データ不足は0。
	eng := NewEngine(DefaultConfig())
	if eng.distributing("TST") != 0 {
		t.Fatal("no data must read zero")
	}
	for i := 0; i < 40; i++ {
		eng.TrackWall("TST", int64(1000+i*1000), 5.0, 5.0, 100.0*(1+0.00005*float64(i)))
	}
	for i := 0; i < 30; i++ {
		eng.AddTape("TST", int64(1000+i*1000), 100.0, 1.0, false)
	}
	if got := eng.distributing("TST"); got != 1 {
		t.Fatalf("rising with sells must read distribute-up, got %d", got)
	}
	seng := NewEngine(DefaultConfig())
	for i := 0; i < 40; i++ {
		seng.TrackWall("TST", int64(1000+i*1000), 5.0, 5.0, 100.0*(1-0.00005*float64(i)))
	}
	for i := 0; i < 30; i++ {
		seng.AddTape("TST", int64(1000+i*1000), 100.0, 1.0, true)
	}
	if got := seng.distributing("TST"); got != -1 {
		t.Fatalf("falling with buys must read distribute-down, got %d", got)
	}
}

func TestAbsorbConfirmed(t *testing.T) {
	// 買い優勢＋売り壁縮小＋膠着＝本物の吸収。壁が減らなければ偽。
	mkAbsorb := func(shrink bool) *Engine {
		eng := NewEngine(DefaultConfig())
		for i := 0; i < 40; i++ {
			ask := 10.0
			if shrink {
				ask = 10.0 - 0.2*float64(i)
			}
			eng.TrackWall("TST", int64(1000+i*1000), 5.0, ask, 100.0)
		}
		for i := 0; i < 40; i++ {
			eng.AddTape("TST", int64(1000+i*1000), 100.0, 3.0, true)
		}
		for i := 0; i < 10; i++ {
			eng.AddTape("TST", int64(41000+i*1000), 100.0, 1.0, false)
		}
		return eng
	}
	if !mkAbsorb(true).absorbConfirmed("TST", 1) {
		t.Fatal("shrinking ask with buys must confirm absorption")
	}
	if mkAbsorb(false).absorbConfirmed("TST", 1) {
		t.Fatal("steady wall must not confirm absorption")
	}
	if mkAbsorb(true).absorbConfirmed("TST", -1) {
		t.Fatal("wrong side must not confirm")
	}
}

func TestSpoofSide(t *testing.T) {
	// 中央値の3倍の買い壁が出現→約定なしに消失＝spoof。食われれば本物。
	mkSpoof := func(eaten bool) *Engine {
		eng := NewEngine(DefaultConfig())
		for i := 0; i < 20; i++ {
			eng.TrackWall("TST", int64(1000+i*1000), 5.0, 5.0, 100.0)
		}
		eng.TrackWall("TST", 21000, 15.0, 5.0, 100.0)
		eng.TrackWall("TST", 22000, 16.0, 5.0, 100.0)
		for i := 0; i < 18; i++ {
			eng.TrackWall("TST", int64(23000+i*1000), 4.0, 5.0, 100.0)
		}
		if eaten {
			for i := 0; i < 20; i++ {
				eng.AddTape("TST", int64(21000+i*500), 100.0, 1.0, true)
			}
		}
		return eng
	}
	if got := mkSpoof(false).spoofSide("TST"); got != 1 {
		t.Fatalf("vanishing wall without fills must read bid spoof, got %d", got)
	}
	if got := mkSpoof(true).spoofSide("TST"); got != 0 {
		t.Fatalf("eaten wall must not read spoof, got %d", got)
	}
	if got := NewEngine(DefaultConfig()).spoofSide("TST"); got != 0 {
		t.Fatalf("no data must read zero, got %d", got)
	}
}

func TestSpoofBlocked(t *testing.T) { // spoof検出でその方向を止め、1時間後に解除。逆方向は止めない。
	eng := NewEngine(DefaultConfig())
	for i := 0; i < 20; i++ {
		eng.TrackWall("TST", int64(1000+i*1000), 5.0, 5.0, 100.0)
	}
	eng.TrackWall("TST", 21000, 15.0, 5.0, 100.0)
	for i := 0; i < 18; i++ {
		eng.TrackWall("TST", int64(22000+i*1000), 4.0, 5.0, 100.0)
	}
	if !eng.spoofBlocked("TST", 1, 50000) {
		t.Fatal("active spoof must block buys")
	}
	if eng.spoofBlocked("TST", -1, 50000) {
		t.Fatal("opposite side must not block")
	}
	// 検出窓を外れても1時間は維持される。
	for i := 0; i < 60; i++ {
		eng.TrackWall("TST", int64(100000+i*1000), 5.0, 5.0, 100.0)
	}
	if !eng.spoofBlocked("TST", 1, 50000+1800000) {
		t.Fatal("halt must persist one hour")
	}
	if eng.spoofBlocked("TST", 1, 50000+spoofHaltMsDefault+1) {
		t.Fatal("halt must expire after one hour")
	}
}

func TestDistributingVeto(t *testing.T) {
	// 上昇＋売り約定優勢の分布中は天井追い（breakout/ride）を撃たない。
	eng := breakoutBase(true)
	for i := 0; i < 40; i++ {
		eng.TrackWall("TST", int64(1000+i*1000), 5.0, 5.0, 100.0*(1+0.00005*float64(i)))
	}
	for i := 0; i < 30; i++ {
		eng.AddTape("TST", int64(1000+i*1000), 100.0, 1.0, false)
	}
	if got := eng.distributing("TST"); got != 1 {
		t.Fatalf("setup must read distribution, got %d", got)
	}
	sig := eng.EvalEntry("TST", 9999999)
	if sig.Type == Buy && (sig.Pattern == "bb_breakout_up" || sig.Pattern == "bb_ride_momentum") {
		t.Fatalf("distribution must veto chase buys, got %s (%s)", sig.Pattern, sig.Reason)
	}
}

func TestSpoofHaltEntry(t *testing.T) { // なりすまし買い壁の検出中は、その方向の新規（breakout）を止める。
	eng := breakoutBase(true)
	for i := 0; i < 20; i++ {
		eng.TrackWall("TST", int64(1000+i*1000), 5.0, 5.0, 100.0)
	}
	eng.TrackWall("TST", 21000, 15.0, 5.0, 100.0)
	eng.TrackWall("TST", 22000, 16.0, 5.0, 100.0)
	for i := 0; i < 18; i++ {
		eng.TrackWall("TST", int64(23000+i*1000), 4.0, 5.0, 100.0)
	}
	if got := eng.spoofSide("TST"); got != 1 {
		t.Fatalf("setup must read bid spoof, got %d", got)
	}
	sig := eng.EvalEntry("TST", 9999999)
	if sig.Type != Hold || sig.Reason != "spoof halt" {
		t.Fatalf("spoof must halt new buys, got %s %s (%s)", sig.Type, sig.Pattern, sig.Reason)
	}
}

func TestApplyDeny(t *testing.T) { // 日次キルスイッチの置き換え：指定パターンの新規が止まり、外すと戻る。
	eng := breakoutBase(true)
	if sig := eng.EvalEntry("TST", 9999999); sig.Pattern != "bb_breakout_up" {
		t.Fatalf("setup must fire breakout, got %s %s", sig.Type, sig.Pattern)
	}
	eng2 := breakoutBase(true)
	eng2.ApplyDeny([]string{"bb_breakout_up"}, nil)
	if sig := eng2.EvalEntry("TST", 9999999); sig.Type == Buy && sig.Pattern == "bb_breakout_up" {
		t.Fatalf("denied pattern must not fire, got %s (%s)", sig.Pattern, sig.Reason)
	}
	eng2.ApplyDeny(nil, nil)
	eng3 := breakoutBase(true)
	if sig := eng3.EvalEntry("TST", 9999999); sig.Pattern != "bb_breakout_up" {
		t.Fatalf("cleared deny must fire again, got %s %s", sig.Type, sig.Pattern)
	}
}

func TestApplyTune(t *testing.T) { // 安全ノブの置き換え：範囲内は適用、範囲外は無視、既定値は維持。
	eng := NewEngine(DefaultConfig())
	if got := eng.wallFirmMin(); got != WallFirmMinDefault {
		t.Fatalf("default wall firm must hold, got %v", got)
	}
	eng.ApplyTune(TuneParams{WallFirmMin: 0.7, ExpCooldownBars: 24, ExpProgressMin: 0.005, SpoofHaltMs: 1800000, ExitConfirmVotes: 3})
	if got := eng.wallFirmMin(); got != 0.7 {
		t.Fatalf("tuned wall firm must apply, got %v", got)
	}
	if got := eng.expCooldown(); got != 24 {
		t.Fatalf("tuned cooldown must apply, got %v", got)
	}
	eng.ApplyTune(TuneParams{WallFirmMin: 5.0, ExpCooldownBars: 1000, ExpProgressMin: 0.5, SpoofHaltMs: 1, ExitConfirmVotes: 99})
	if got := eng.wallFirmMin(); got != 0.7 {
		t.Fatalf("wild tune must be ignored, got %v", got)
	}
	// 0値（未設定）は維持する。
	eng.ApplyTune(TuneParams{})
	if got := eng.wallFirmMin(); got != 0.7 {
		t.Fatalf("empty tune must keep values, got %v", got)
	}
}

func TestAllowCells(t *testing.T) {
	// 実証セル制：セル内は発火、セル外・方向違いは不発火、空集合は無制限。
	mk := func(cells []AllowCell) *Engine {
		eng := breakoutBase(true)
		eng.ApplyAllow(cells)
		return eng
	}
	if sig := mk([]AllowCell{{Side: "buy", Pattern: "bb_breakout_up", Regime: "trend_up"}}).EvalEntry("TST", 9999999); sig.Pattern != "bb_breakout_up" {
		t.Fatalf("allowed cell must fire, got %s %s (%s)", sig.Type, sig.Pattern, sig.Reason)
	}
	if sig := mk([]AllowCell{{Side: "buy", Pattern: "pullback_to_bbmid", Regime: "trend_up"}}).EvalEntry("TST", 9999999); sig.Type != Hold || sig.Reason != "cell not allowed" {
		t.Fatalf("foreign cell must hold, got %s %s (%s)", sig.Type, sig.Pattern, sig.Reason)
	}
	if sig := mk([]AllowCell{{Side: "sell", Pattern: "bb_breakout_up", Regime: "trend_up"}}).EvalEntry("TST", 9999999); sig.Type != Hold {
		t.Fatalf("wrong side must hold, got %s %s", sig.Type, sig.Pattern)
	}
	// 壊れたセルは無視され、無制限に戻る。
	if sig := mk([]AllowCell{{Side: "hold", Pattern: "", Regime: ""}}).EvalEntry("TST", 9999999); sig.Pattern != "bb_breakout_up" {
		t.Fatalf("broken cells must be ignored, got %s %s", sig.Type, sig.Pattern)
	}
}
