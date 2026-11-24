package vt

// tactics.go はMM戦術の分類器（P2）。
//
// 板 snapshot と約定テープから、MMの3戦術を判定する：
//   - distributing: 上昇中に売り約定が支配的＝MMの売り上がり（在庫処分）。
//     その方向の追いはMMの在庫を買わされるため止める。
//   - absorbConfirmed: 壁が食われながら価格が動かない＝本物の吸収。
//     吸収後の順行に便乗する根拠にする。
//   - spoofSide: 約定を伴わず現れて消える壁＝なりすまし。
//     その方向の新規を1時間停止する。
// いずれもデータ不足時は 0/false（fail-open。冷間・短足列を殺さない）。
// ロング・ショート対称。

// tacticWindow は戦術判定に使う壁履歴の窓（件数。1tick/秒で約60秒）。
const tacticWindow = 60

// distribMoveMin は分布とみなす窓内値動きの下限（比率0.1%）。
const distribMoveMin = 0.001

// distribCVDMin は分布とみなす逆行CVDの大きさ。
const distribCVDMin = 0.4

// absorbShrinkMin は吸収とみなす壁の減少率。
const absorbShrinkMin = 0.3

// spoofSizeMult はなりすまし壁とみなす中央値比。
const spoofSizeMult = 2.0

// spoofVanishMax はなりすまし消失とみなす残存率（これ以下まで減る）。
const spoofVanishMax = 0.3

// spoofFillMax はなりすましとみなす約定量の上限（消失量比）。
// 消えた量の半分も食われていなければ取消＝spoofとみなす。
const spoofFillMax = 0.5

// spoofActiveMs はspoof検出の有効窓（ms）。検出後この間は停止を更新する。
const spoofActiveMs = int64(60000)

// spoofHaltMsDefault はspoof検出後の新規停止期間の既定値（ms。1時間）。
const spoofHaltMsDefault = int64(3600000)

// spoofMark はspoof検出の停止記録。
type spoofMark struct {
	side int
	tsMs int64
}

// wallMedian は壁履歴の数量中央値を返す。
func wallMedian(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	cp := append([]float64(nil), xs...)
	for i := 1; i < len(cp); i++ {
		for j := i; j > 0 && cp[j] < cp[j-1]; j-- {
			cp[j], cp[j-1] = cp[j-1], cp[j]
		}
	}
	m := len(cp) / 2
	if len(cp)%2 == 0 {
		return (cp[m-1] + cp[m]) / 2
	}
	return cp[m]
}

// wallSideSeries は壁履歴から指定sideの数量列とmid列を返す。
// side>0で買い壁（Bid3）、side<0で売り壁（Ask3）。
func (e *Engine) wallSideSeries(symbol string, side int) (sizes, mids []float64, tss []int64) {
	hist := e.wallLv[symbol]
	if len(hist) > tacticWindow {
		hist = hist[len(hist)-tacticWindow:]
	}
	for _, w := range hist {
		if side > 0 {
			sizes = append(sizes, w.Bid3)
		} else {
			sizes = append(sizes, w.Ask3)
		}
		mids = append(mids, w.Mid)
		tss = append(tss, w.TS)
	}
	return sizes, mids, tss
}

// distributing はMMの分布方向を返す（+1: 上昇中の売り上がり、
// -1: 下降中の買い集め、0: なし・不明）。
// 価格が窓内に0.1%以上動いたのにCVDが逆行していれば、見える値動きと
// 逆の実需（＝MMの在庫処分・仕込み）とみなす。
func (e *Engine) distributing(symbol string) int {
	sizes, mids, _ := e.wallSideSeries(symbol, 1)
	_ = sizes
	if len(mids) < 30 {
		return 0
	}
	first, last := mids[0], mids[len(mids)-1]
	if first <= 0 {
		return 0
	}
	move := (last - first) / first
	ratio, ok := e.CVDRatio(symbol)
	if !ok {
		return 0
	}
	if move >= distribMoveMin && ratio <= -distribCVDMin {
		return 1
	}
	if move <= -distribMoveMin && ratio >= distribCVDMin {
		return -1
	}
	return 0
}

// tapeVolumeBetween は区間の約定量合計を返す。
func (e *Engine) tapeVolumeBetween(symbol string, fromMs, toMs int64) float64 {
	total := 0.0
	for _, t := range e.tape[symbol] {
		if t.Timestamp >= fromMs && t.Timestamp <= toMs {
			total += t.Qty
		}
	}
	return total
}

// absorbConfirmed は本物の吸収を確認できたかを返す。
// side>0: 買い約定優勢＋売り壁の縮小＋価格膠着（売り壁を食う本物の買い）。
// side<0: 対称。既存 absorbed()（テープのみ）に壁の裏付けを足した版。
func (e *Engine) absorbConfirmed(symbol string, side int) bool {
	if !e.absorbed(symbol) {
		return false
	}
	ratio, ok := e.CVDRatio(symbol)
	if !ok {
		return false
	}
	sizes, _, tss := e.wallSideSeries(symbol, -side)
	if len(sizes) < 30 {
		return false
	}
	if side > 0 && ratio < AbsorbCVDRatio {
		return false
	}
	if side < 0 && ratio > -AbsorbCVDRatio {
		return false
	}
	first, last := sizes[0], sizes[len(sizes)-1]
	if first <= 0 {
		return false
	}
	if (first-last)/first < absorbShrinkMin {
		return false
	}
	_ = tss
	return true
}

// spoofSide は約定を伴わない壁の出現→消失を検出したらその壁側を返す。
// +1: 買い壁spoof（上への誘い＝買い新規を止める）、-1: 売り壁spoof、0: なし。
// 中央値の2倍以上の壁が現れ、60秒窓内に7割以上消え、かつ消えた量の
// 半分も約定していなければ取消＝spoofとみなす。
func (e *Engine) spoofSide(symbol string) int {
	if s := e.spoofSideDir(symbol, 1); s != 0 {
		return s
	}
	return e.spoofSideDir(symbol, -1)
}

func (e *Engine) spoofSideDir(symbol string, side int) int {
	sizes, _, tss := e.wallSideSeries(symbol, side)
	if len(sizes) < 30 {
		return 0
	}
	med := wallMedian(sizes)
	if med <= 0 {
		return 0
	}
	peak, peakIdx := 0.0, -1
	for i, v := range sizes {
		if v >= med*spoofSizeMult && v > peak {
			peak, peakIdx = v, i
		}
	}
	if peakIdx < 0 {
		return 0
	}
	vanished := sizes[len(sizes)-1]
	if vanished/peak > spoofVanishMax {
		return 0
	}
	fills := e.tapeVolumeBetween(symbol, tss[peakIdx], tss[len(tss)-1])
	if fills/(peak-vanished+1e-9) > spoofFillMax {
		return 0
	}
	return side
}

// spoofBlocked はspoof方向の新規を止めるべきかを返す。
// 検出中は停止マークを更新し、検出後1時間は停止を維持する。
// sideは発注方向（+1買い/-1売り）。データ不足時は偽。
func (e *Engine) spoofBlocked(symbol string, side int, tsMs int64) bool {
	if s := e.spoofSide(symbol); s == side {
		e.spoofHalt[symbol] = spoofMark{side: side, tsMs: tsMs}
		return true
	}
	if m, ok := e.spoofHalt[symbol]; ok && m.side == side && tsMs-m.tsMs < e.spoofHaltWindow() {
		return true
	}
	return false
}
