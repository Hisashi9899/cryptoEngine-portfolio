package vt

// grid.go はレンジ帯専用のグリッド（逆張り買い）戦略を提供する。
//
// 設計原則（二刀流）：
//   - trend（EvalEntry）が担当しない regime=="range" の局面だけを受け持つ。
//     trend_up/down 時は一切発火しないため、既存 trend との二重発火はない。
//   - 現物ロング専用。ショート脚は paper 実績で辺りゼロのため持たない。
//   - 出口の安全網（SL・時間切れ・トレイル・反転・板逆行）は EvalExit に委ね、
//     grid 固有の利確（BB中央）・無効化（帯割れ）だけを先に判定する。
//     損益の帰属は入口パターン名（grid_ 接頭辞）で分離できる。
//   - 手数料負け防止：帯域幅下限と fee edge（期待値>コスト）を要求する。

// gridMinBandwidth はグリッド稼働に必要な帯域幅の下限（比率）。
// これ未満の死んだレンジでは TP がコストに満たないため不発火にする。
const gridMinBandwidth = 0.01

// gridCostRate はコスト率の固定部（手数料5bps＋滑り安全域10bps）。
// 呼び出し側の spreadRatio を加えたものが総コストになる。
const gridCostRate = 0.0015

// gridConfidence はグリッド入口の確信度。live の minConf 0.58 を超える値に
// 固定する（可変にすると較正が発散するため）。
const gridConfidence = 0.60

// GridPattern はグリッド入口パターン名。PF分離のキーになる。
const GridPattern = "grid_range_buy"

// IsGridPattern はグリッド建玉かを返す（両side将来拡張用の接頭辞判定）。
func IsGridPattern(pattern string) bool {
	return len(pattern) >= 5 && pattern[:5] == "grid_"
}

// GridEntry はレンジ帯の押し目買い判定を行う。買わないときは Hold を返す。
// spreadRatio は呼び出し側 tick のスプレッド率（fee edge 用）。
func (e *Engine) GridEntry(symbol string, tsMs int64, spreadRatio float64) *Signal {
	bars := e.builderFor(symbol).Bars()
	mkHold := func(reason string) *Signal {
		last := 0.0
		if len(bars) > 0 {
			last = bars[len(bars)-1].C
		}
		return &Signal{Symbol: symbol, Type: Hold, Price: last, Reason: reason, Timestamp: tsMs}
	}
	need := e.cfg.MASlow + e.cfg.MACDSig + 2
	if len(bars) < need {
		return mkHold("warming up")
	}
	// 足の連続性：欠測時は指標が歪むため入口を見送る（trend と同一）。
	// 出口（GridExit・EvalExit）は止めない。
	if !barsContinuous(bars, e.cfg.BarsSec*1000) {
		return mkHold("time gap")
	}
	// レンジ認定：上位足が range であること（trend logic と同一判定）。
	// trend_up/down 時は trend に任せる。スクイーズ帯の有無は問わない。
	// 押し目足自体が帯域・ドリフトを膨らませるため、スクイーズ判定は
	// 押し目局面と相性が悪い。代わりに帯域幅下限で取引可能幅を保証する。
	// なお squeezedSeries は trend 側のゲート・テストで共用する。
	// 入口足の下落トレンドでは止めない。押し目は定義上5分足が下を向く
	// 瞬間であり、そこで止めると押し目自体が成立しない。
	// 滝途中の拾い過ぎは無効化（帯割れ即切り）が面倒を見る。
	if regime := trendOf(Closes(e.trendFor(symbol).Bars()), e.cfg.MAFast, e.cfg.MASlow); regime != "range" {
		return mkHold("grid need range")
	}
	closes := Closes(bars)
	mid, upper, lower := Bollinger(closes, e.cfg.BBPeriod, e.cfg.BBK)
	if mid <= 0 || upper <= lower {
		return mkHold("no band basis")
	}
	// 帯域幅：TP（中央までの半値幅）がコストを賄えるレンジのみ。
	if bw := Bandwidth(closes, e.cfg.BBPeriod, e.cfg.BBK); bw < gridMinBandwidth {
		return mkHold("grid thin range")
	}
	last := closes[len(closes)-1]
	pb := PercentB(last, upper, lower)
	rsi := RSI(closes, e.cfg.RSIPeriod)
	minRSI5 := rsi
	for k := 1; k < 5 && len(closes)-k >= e.cfg.RSIPeriod+1; k++ {
		if v := RSI(closes[:len(closes)-k], e.cfg.RSIPeriod); v < minRSI5 {
			minRSI5 = v
		}
	}
	_, _, hist := MACD(closes, e.cfg.MACDFast, e.cfg.MACDSlow, e.cfg.MACDSig)
	prevHist := 0.0
	if len(closes) >= e.cfg.MACDSlow+e.cfg.MACDSig+1 {
		_, _, prevHist = MACD(closes[:len(closes)-1], e.cfg.MACDFast, e.cfg.MACDSlow, e.cfg.MACDSig)
	}
	// fee edge：中央までの期待値が総コスト（スプレッド＋固定部）を超えること。
	// 中途半端な entry を手数料負けさせないための核。
	cost := gridCostRate
	if e.cfg.GridCostRate > 0 {
		cost = e.cfg.GridCostRate
	}
	if spreadRatio > 0 {
		cost += spreadRatio
	}
	if mid <= 0 || (mid-last)/last <= cost {
		return mkHold("grid edge too thin")
	}
	// 活発さ・迷い足・実体は trend と同一基準（対称思想）。
	if medV := MedianVolume(bars, 20); medV <= 0 || bars[len(bars)-1].V < volumeActivityMin*medV {
		return mkHold("low activity")
	}
	if IsDoji(bars[len(bars)-1]) {
		return mkHold("doji indecision")
	}
	atr := ATR(bars, 14)
	cur := bars[len(bars)-1]
	sigBody := atr > 0 && Body(cur) >= patternMinATR*atr
	if ref := last; !(ref > 0 && Body(cur) >= minBodyRatio*ref) {
		sigBody = false
	}
	// 板の裏取り：売り圧下の押し目買いは見送る（veto。冷間は通す）。
	// 確信投票（flowAgree）は閾値到達の押し目には要求しない。
	// レンジ帯の板は均衡が常態のため、確信一致の要求は過剰だからである。
	// ただし強い逆板のときだけ止める点は trend と同一。
	flow := e.flowDir(symbol)
	tapeOKLong := e.tapeAligned(symbol, 1)
	if flow < 0 {
		return mkHold("against orderflow")
	}
	if !tapeOKLong {
		return mkHold("against tapeflow")
	}
	// 下限圏の陽線復帰：下位30%・洗い・方向・実体・MACD/RCI反転あり。
	// 下位15%に絞ると回復1本で圏外に出て発火しないため、下位30%とする。
	// それでも中央（TP域）には届かない位置に限定する。
	if pb <= 0.30 && minRSI5 <= 35 && IsBullish(cur) && sigBody && hist >= prevHist {
		if e.longDeny["grid_range_buy"] {
			return mkHold("long pattern denied")
		}
		rciFast, prevRciFast := RCI(closes, rciFastPeriod), 0.0
		if len(closes) > rciFastPeriod {
			prevRciFast = RCI(closes[:len(closes)-1], rciFastPeriod)
		}
		if rciFast < prevRciFast {
			return mkHold("grid need rci turn")
		}
		return &Signal{
			Symbol: symbol, Type: Buy, Price: last,
			Reason: "grid range dip with edge", Timestamp: tsMs, Confidence: gridConfidence,
			Pattern: GridPattern, Regime: "range",
			RSI: rsi, PercentB: pb, MACDHist: hist,
			BarO: cur.O, BarH: cur.H, BarL: cur.L, BarV: cur.V,
			MMState: mmStateName(e.mmState(symbol)),
		}
	}
	return mkHold("no grid trigger")
}

// GridExit はグリッド建玉の固有出口を判定する。決済すべきなら理由を返す。
// 利確（BB中央到達）と無効化（帯割れ＝レンジ破壊→trend へ明け渡し）のみ。
// それ以外は false を返し、呼び出し側は EvalExit（SL・時間・トレイル・
// 反転・板逆行の安全網）に委ねること。損益帰属は入口パターンで分離できる。
func (e *Engine) GridExit(symbol string, pos OpenPos, mid float64, tsMs int64) (bool, string) {
	if mid <= 0 || pos.EntryPrice <= 0 || pos.Side != Buy {
		return false, ""
	}
	bars := e.builderFor(symbol).Bars()
	need := e.cfg.BBPeriod
	if e.cfg.MACDSlow+e.cfg.MACDSig > need {
		need = e.cfg.MACDSlow + e.cfg.MACDSig
	}
	if len(bars) < need {
		return false, ""
	}
	closes := Closes(bars)
	midBB, upper, lower := Bollinger(closes, e.cfg.BBPeriod, e.cfg.BBK)
	if midBB <= 0 || upper <= lower {
		return false, ""
	}
	if PercentB(mid, upper, lower) >= 0.5 {
		return true, "grid_tp"
	}
	if mid < lower {
		return true, "grid_invalidate"
	}
	return false, ""
}
