package vt

// cost.go は執行層のコスト・ゲートと計測に使う読み取り専用ヘルパ群。
//
// 判定ロジック（EvalEntry / GridEntry）には手を入れず、執行・記録層から
// 参照するだけの純粋な観測値を提供する。失敗時は 0 を返し、呼び出し側は
// fail-open（不明は通す）で扱う。ロング・ショート対称。

// ATRRatio は入口足の ATR(14) を直近終値で割った比率を返す。
// 1トレードで狙える値動きの大きさの代理値。足不足・価格不明は 0。
func (e *Engine) ATRRatio(symbol string) float64 {
	bars := e.builderFor(symbol).Bars()
	if len(bars) < 15 {
		return 0
	}
	last := bars[len(bars)-1].C
	if last <= 0 {
		return 0
	}
	atr := ATR(bars, 14)
	if atr <= 0 {
		return 0
	}
	return atr / last
}

// AbsorbConfirmed は本物の吸収（壁の縮小＋テープ優勢＋価格膠着）を
// 執行層から判定するための公開ラッパ。side>0 は買い、side<0 は売り。
// データ不足時は false（呼び出し側は Maker 条件としてのみ使う）。
func (e *Engine) AbsorbConfirmed(symbol string, side int) bool {
	if side == 0 {
		return false
	}
	return e.absorbConfirmed(symbol, side)
}
