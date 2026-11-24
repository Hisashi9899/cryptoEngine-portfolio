package vt

// tape.go は約定テープ（テイカー売買）の CVD・吸収検知を提供する。
//
// 板不均衡が「置かれた注文の偏り」なのに対し、テープは「実際に食った量」
// で、方向の裏付けとして一段強い。用途は2つ：
//   - 入口ゲート：確信的な逆テープフローでのエントリーを見送る
//   - 出口：吸収（片寄った約定にもかかわらず価格が動かない＝隠れ反対圧力）
//     を検出したら損切り前に離脱する（imbalance_flip と対）

// TapeTick は約定1件。
type TapeTick struct {
	Timestamp int64
	Price     float64
	Qty       float64
	Buy       bool // true=買い食い（テイカー買い）
}

// TapeWindow はテープ判定の窓（件数）。
const TapeWindow = 50

// TapeAlignMin は方向認定に必要な符号の割合。
const TapeAlignMin = 0.6

// AbsorbCVDRatio は吸収とみなす CVD 比率の下限。
const AbsorbCVDRatio = 0.7

// AbsorbMoveMax は吸収とみなす窓内値動きの上限（比率）。
const AbsorbMoveMax = 0.0005

// AddTape は約定をテープ履歴に追加する。最新200件保持。
func (e *Engine) AddTape(symbol string, tsMs int64, price, qty float64, buy bool) {
	if price <= 0 || qty <= 0 {
		return
	}
	e.tape[symbol] = append(e.tape[symbol], TapeTick{Timestamp: tsMs, Price: price, Qty: qty, Buy: buy})
	if len(e.tape[symbol]) > 200 {
		e.tape[symbol] = e.tape[symbol][len(e.tape[symbol])-200:]
	}
}

// tapeWindow は直近窓を返す。
func (e *Engine) tapeWindow(symbol string) []TapeTick {
	ticks := e.tape[symbol]
	if len(ticks) > TapeWindow {
		ticks = ticks[len(ticks)-TapeWindow:]
	}
	return ticks
}

// CVDRatio は窓内の CVD 比率（-1〜+1）を返す。件数不足時は 0 と false。
func (e *Engine) CVDRatio(symbol string) (float64, bool) {
	ticks := e.tapeWindow(symbol)
	if len(ticks) < 10 {
		return 0, false
	}
	signed, total := 0.0, 0.0
	for _, t := range ticks {
		if t.Buy {
			signed += t.Qty
		} else {
			signed -= t.Qty
		}
		total += t.Qty
	}
	if total <= 0 {
		return 0, false
	}
	return signed / total, true
}

// tapeAligned はテープ方向が want（+1買い/-1売り）と一致するか返す。
// 不明時は true（fail-open。冷間始動を殺さない）。
func (e *Engine) tapeAligned(symbol string, want int) bool {
	ratio, ok := e.CVDRatio(symbol)
	if !ok {
		return true
	}
	if want > 0 {
		return ratio > -TapeAlignMin
	}
	return ratio < TapeAlignMin
}

// absorbed は吸収状態かどうかを返す（片寄った約定＋動かない価格）。
func (e *Engine) absorbed(symbol string) bool {
	ticks := e.tapeWindow(symbol)
	if len(ticks) < 10 {
		return false
	}
	signed, total := 0.0, 0.0
	for _, t := range ticks {
		if t.Buy {
			signed += t.Qty
		} else {
			signed -= t.Qty
		}
		total += t.Qty
	}
	if total <= 0 {
		return false
	}
	ratio := signed / total
	if ratio < 0 {
		ratio = -ratio
	}
	if ratio < AbsorbCVDRatio {
		return false
	}
	first, last := ticks[0].Price, ticks[len(ticks)-1].Price
	if first <= 0 {
		return false
	}
	move := (last - first) / first
	if move < 0 {
		move = -move
	}
	return move <= AbsorbMoveMax
}
