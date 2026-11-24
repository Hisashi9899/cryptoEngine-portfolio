package vt

import (
	"math"
	"sort"
)

// bar.go はティックmid列の時間足生成と基本指標を提供する。
//
// V.Tエンジンは板の5秒スナップショットではなく、確定足の形で相場を読む。
// 足種は 60/300/900 秒（1分・5分・15分）。出来高は合計する。

// Bar は1本の確定足。
type Bar struct {
	Start int64   // 開始時刻ミリ秒
	O     float64 // 始値
	H     float64 // 高値
	L     float64 // 安値
	C     float64 // 終値
	V     float64 // 出来高合計
}

// Builder はティックを足に束ねる。完成足のみ保持する。
type Builder struct {
	widthMs int64
	maxKeep int
	cur     *Bar
	done    []Bar
}

// NewBuilder は足幅秒と保持本数で Builder を作る。
func NewBuilder(widthSec int64, maxKeep int) *Builder {
	if widthSec <= 0 {
		widthSec = 300
	}
	if maxKeep <= 0 {
		maxKeep = 200
	}
	return &Builder{widthMs: widthSec * 1000, maxKeep: maxKeep}
}

// Add はティックを追加し、新規確定足があれば返す（無ければ nil）。
func (b *Builder) Add(tsMs int64, price, volume float64) *Bar {
	if price <= 0 {
		return nil
	}
	slot := (tsMs / b.widthMs) * b.widthMs
	if b.cur == nil || b.cur.Start != slot {
		var finished *Bar
		if b.cur != nil {
			cp := *b.cur
			finished = &cp
			b.done = append(b.done, cp)
			if len(b.done) > b.maxKeep {
				b.done = b.done[len(b.done)-b.maxKeep:]
			}
		}
		b.cur = &Bar{Start: slot, O: price, H: price, L: price, C: price, V: volume}
		return finished
	}
	if price > b.cur.H {
		b.cur.H = price
	}
	if price < b.cur.L {
		b.cur.L = price
	}
	b.cur.C = price
	b.cur.V += volume
	return nil
}

// Bars は確定足列を古い順に返す。
func (b *Builder) Bars() []Bar {
	return b.done
}

// barsContinuous は直近3本に欠測スロットがないかを返す。
// いずれかの間隔が足幅を超えていたら欠測とみなす（配信停止・収集欠測では
// MA/RSI/MACDが歪むため入口を見送る）。等幅・短幅（テストの手作り足を含む）
// は通す。出口には適用しない（離脱を止めると塩漬けになるため）。
// 不足・不正時は偽（fail-closed。確認できない足で撃たない）。
func barsContinuous(bars []Bar, widthMs int64) bool {
	if widthMs <= 0 || len(bars) < 3 {
		return false
	}
	ia := bars[len(bars)-2].Start - bars[len(bars)-3].Start
	ib := bars[len(bars)-1].Start - bars[len(bars)-2].Start
	return ia > 0 && ia <= widthMs && ib > 0 && ib <= widthMs
}

// nearLow20 は終値が直近20本安値の0.5%以内かを返す。
// high_black の高値近接（0.5%）の対称形。big_white_dip が中段の戻りを
// ディップと誤認しないための定義。ロング・ショート対称の定義として
// 入口・出口鏡像の両方で使う。
func nearLow20(closes []float64, lastC float64) bool {
	if len(closes) <= 20 || lastC <= 0 {
		return false
	}
	lo := minClose(closes[:len(closes)-1], 20)
	if lo <= 0 {
		return false
	}
	return (lastC-lo)/lo < 0.005
}

// brokeLow20 は終値が直近20本安値を更新したかを返す。
// 収縮なしのエクスパンション下抜け検出用（帯域に追従してしまう帯抜けの代替）。
// 対称に brokeHigh20。両side対称の定義として使う。
func brokeLow20(closes []float64, lastC float64) bool {
	if len(closes) <= 20 || lastC <= 0 {
		return false
	}
	lo := minClose(closes[:len(closes)-1], 20)
	return lo > 0 && lastC < lo
}

// brokeHigh20 は終値が直近20本高値を更新したかを返す（対称形）。
func brokeHigh20(closes []float64, lastC float64) bool {
	if len(closes) <= 20 || lastC <= 0 {
		return false
	}
	hi := maxClose(closes[:len(closes)-1], 20)
	return hi > 0 && lastC > hi
}

// Closes は終値列を返す。
func Closes(bars []Bar) []float64 {
	out := make([]float64, 0, len(bars))
	for _, b := range bars {
		out = append(out, b.C)
	}
	return out
}

// SMA は直近 n 本の単純平均を返す。不足時は 0。
func SMA(vals []float64, n int) float64 {
	if n <= 0 || len(vals) < n {
		return 0
	}
	sum := 0.0
	for _, v := range vals[len(vals)-n:] {
		sum += v
	}
	return sum / float64(n)
}

// SD は直近 n 本の母標準偏差を返す。不足時は 0。
func SD(vals []float64, n int) float64 {
	if n <= 0 || len(vals) < n {
		return 0
	}
	mean := SMA(vals, n)
	sum := 0.0
	for _, v := range vals[len(vals)-n:] {
		d := v - mean
		sum += d * d
	}
	if sum <= 0 {
		return 0
	}
	return math.Sqrt(sum / float64(n))
}

// Bollinger は (mid, upper, lower) を返す。不足時は全て 0。
func Bollinger(closes []float64, n int, k float64) (mid, upper, lower float64) {
	mid = SMA(closes, n)
	if mid <= 0 {
		return 0, 0, 0
	}
	sd := SD(closes, n)
	upper = mid + k*sd
	lower = mid - k*sd
	return mid, upper, lower
}

// EMA は指数平均の最終値を返す。不足時は 0。
func EMA(vals []float64, n int) float64 {
	if n <= 0 || len(vals) < n {
		return 0
	}
	k := 2.0 / (float64(n) + 1.0)
	e := vals[len(vals)-n]
	for _, v := range vals[len(vals)-n+1:] {
		e = v*k + e*(1-k)
	}
	return e
}

// RSI は直近 n 期間の RSI を返す。不足時は 50（中立）。
func RSI(closes []float64, n int) float64 {
	if n <= 0 || len(closes) < n+1 {
		return 50
	}
	gain, loss := 0.0, 0.0
	prev := closes[len(closes)-n-1]
	for _, c := range closes[len(closes)-n:] {
		d := c - prev
		if d > 0 {
			gain += d
		} else {
			loss -= d
		}
		prev = c
	}
	if loss <= 0 {
		return 100
	}
	if gain <= 0 {
		return 0
	}
	rs := (gain / float64(n)) / (loss / float64(n))
	return 100 - 100/(1+rs)
}

// MACD は (macd, signal, hist) を返す。不足時は全て 0。
func MACD(closes []float64, fast, slow, sig int) (macd, signal, hist float64) {
	if fast <= 0 || slow <= 0 || sig <= 0 || len(closes) < slow+sig {
		return 0, 0, 0
	}
	line := make([]float64, 0, len(closes)-slow+1)
	kf := 2.0 / (float64(fast) + 1.0)
	ks := 2.0 / (float64(slow) + 1.0)
	ef, es := closes[0], closes[0]
	for i, c := range closes {
		ef = c*kf + ef*(1-kf)
		es = c*ks + es*(1-ks)
		if i >= slow-1 {
			line = append(line, ef-es)
		}
	}
	macd = line[len(line)-1]
	k := 2.0 / (float64(sig) + 1.0)
	signal = line[0]
	for _, v := range line[1:] {
		signal = v*k + signal*(1-k)
	}
	hist = macd - signal
	return macd, signal, hist
}

// ATR は直近 n 本の平均レンジ（簡易 ATR）を返す。不足時は 0。
func ATR(bars []Bar, n int) float64 {
	if n <= 0 || len(bars) < n {
		return 0
	}
	sum := 0.0
	for _, b := range bars[len(bars)-n:] {
		sum += Range(b)
	}
	return sum / float64(n)
}

// MedianVolume は直近 n 本の出来高中央値を返す。不足時は 0。
func MedianVolume(bars []Bar, n int) float64 {
	if n <= 0 || len(bars) < n {
		return 0
	}
	vs := make([]float64, 0, n)
	for _, b := range bars[len(bars)-n:] {
		vs = append(vs, b.V)
	}
	sort.Float64s(vs)
	m := len(vs) / 2
	if len(vs)%2 == 0 {
		return (vs[m-1] + vs[m]) / 2
	}
	return vs[m]
}

// isVolumeSurge は出来高急増かどうかを返す（直近中央値の2倍以上）。
// 株の達人の出来高急増パターンの裏付けに使う。
func isVolumeSurge(bars []Bar) bool {
	if len(bars) == 0 {
		return false
	}
	med := MedianVolume(bars, 20)
	if med <= 0 {
		return false
	}
	return bars[len(bars)-1].V >= 2*med
}

// ParabolicSAR は放物線 SAR 列を返す（High/Low 基準・標準加速）。
// 反転時は EP を極値にリセットする。不足時はゼロ埋め。
// 株の達人の RCI+パラボリック複合（トレンド転換点の計測）に使う。
func ParabolicSAR(bars []Bar, step, maxAF float64) []float64 {
	n := len(bars)
	out := make([]float64, n)
	if n < 2 || step <= 0 || maxAF <= 0 {
		return out
	}
	up := bars[1].C >= bars[0].C
	af := step
	var ep, sar float64
	if up {
		ep = bars[0].H
		sar = bars[0].L
	} else {
		ep = bars[0].L
		sar = bars[0].H
	}
	for i := 1; i < n; i++ {
		sar += af * (ep - sar)
		if up {
			lo := bars[i-1].L
			if i >= 2 && bars[i-2].L < lo {
				lo = bars[i-2].L
			}
			if sar > lo {
				sar = lo
			}
			if bars[i].L < sar {
				up = false
				sar = ep
				af = step
				ep = bars[i].L
			} else if bars[i].H > ep {
				ep = bars[i].H
				af += step
				if af > maxAF {
					af = maxAF
				}
			}
		} else {
			hi := bars[i-1].H
			if i >= 2 && bars[i-2].H > hi {
				hi = bars[i-2].H
			}
			if sar < hi {
				sar = hi
			}
			if bars[i].H > sar {
				up = true
				sar = ep
				af = step
				ep = bars[i].H
			} else if bars[i].L < ep {
				ep = bars[i].L
				af += step
				if af > maxAF {
					af = maxAF
				}
			}
		}
		out[i] = sar
	}
	return out
}

// sarFlip は SAR の転換方向を返す（+1 上転換 / -1 下転換 / 0 不明）。
// 足確定ベース：1本前は逆側・現足は順側にあることを要求する。
func sarFlip(bars []Bar, sar []float64) int {
	n := len(bars)
	if n < 3 || len(sar) != n {
		return 0
	}
	prevAbove := sar[n-2] >= bars[n-2].C
	curBelow := sar[n-1] < bars[n-1].C
	prevBelow := sar[n-2] < bars[n-2].C
	curAbove := sar[n-1] >= bars[n-1].C
	if prevAbove && curBelow {
		return 1
	}
	if prevBelow && curAbove {
		return -1
	}
	return 0
}

// Bandwidth は終値列のボリンジャーバンド幅比率を返す。
func Bandwidth(closes []float64, period int, k float64) float64 {
	mid, upper, lower := Bollinger(closes, period, k)
	if mid <= 0 {
		return 0
	}
	return (upper - lower) / mid
}

// squeezeBefore は現足を除く直前窓で帯が締まっていたかを返す。
// 最も締まった状態から1.2倍以内にまだ収まっていればスクイーズ継続とみなす。
func squeezeBefore(closes []float64, period int, k float64) bool {
	if len(closes) < period+squeezeLookback {
		return false
	}
	minBW := 0.0
	first := true
	var last float64
	for j := 0; j < squeezeLookback; j++ {
		end := len(closes) - j
		if end < period {
			break
		}
		w := Bandwidth(closes[:end], period, k)
		if j == 0 {
			last = w
		}
		if first || w < minBW {
			minBW = w
			first = false
		}
	}
	if first || minBW <= 0 {
		return false
	}
	return last <= 1.2*minBW
}

// bbBreakout はスクイーズ後の上抜け・下抜けを判定する。
// closes は現足を含む全終値列。バンドは現足を除いて計算する。
func bbBreakout(closes []float64, period int, k float64) (up, down bool) {
	n := len(closes)
	if n < period+squeezeLookback+1 {
		return false, false
	}
	base := closes[:n-1]
	if len(base) < period {
		return false, false
	}
	_, upper, lower := Bollinger(base[len(base)-period:], period, k)
	if upper <= lower {
		return false, false
	}
	if !squeezeBefore(base, period, k) {
		return false, false
	}
	c := closes[n-1]
	return c > upper, c < lower
}

// PercentB は終値のバンド内位置を返す（1超＝上限超え）。
func PercentB(close, upper, lower float64) float64 {
	if upper <= lower {
		return 0.5
	}
	return (close - lower) / (upper - lower)
}

// RCI は順位相関指数を返す（-100〜+100）。株の達人式。
// -80%以下で底値圏・+80%以上で高値圏。同値は平均順位で処理する。
// 例：5日 [700,750,780,750,800] → -77.5。不足時は 0。
func RCI(closes []float64, n int) float64 {
	if n <= 1 || len(closes) < n {
		return 0
	}
	seg := closes[len(closes)-n:]
	rank := make([]float64, n)
	for i := 0; i < n; i++ {
		less, equal := 0, 0
		for j := 0; j < n; j++ {
			if seg[j] < seg[i] {
				less++
			} else if seg[j] == seg[i] {
				equal++
			}
		}
		greater := n - less - equal
		rank[i] = float64(greater) + (float64(equal)+1)/2
	}
	d := 0.0
	for i := 0; i < n; i++ {
		// 時間順位は新しい順（最新＝1）。上昇相場で価格順位と一致し+100。
		diff := float64(n-i) - rank[i]
		d += diff * diff
	}
	return (1 - 6*d/float64(n*(n*n-1))) * 100
}

// RSISeries は各足を終点とする RSI 列を返す（不足部は 50）。
func RSISeries(closes []float64, n int) []float64 {
	out := make([]float64, len(closes))
	for k := range closes {
		if k < n {
			out[k] = 50
			continue
		}
		out[k] = RSI(closes[:k+1], n)
	}
	return out
}

// MACDHistSeries は各足を終点とする MACD ヒストグラム列を返す（不足部は 0）。
func MACDHistSeries(closes []float64, fast, slow, sig int) []float64 {
	out := make([]float64, len(closes))
	for k := range closes {
		if k+1 < slow+sig {
			continue
		}
		_, _, h := MACD(closes[:k+1], fast, slow, sig)
		out[k] = h
	}
	return out
}

// HistFlips は直近 n 本のヒストグラムの符号反転回数を返す。
// 微小値（終値比 eps 未満）は中立として無視する。
// 株の達人の言う「GC/DC繰り返しの保ち合い」を検出する。
func HistFlips(closes []float64, fast, slow, sig, n int) int {
	h := MACDHistSeries(closes, fast, slow, sig)
	if len(h) < n || n <= 0 {
		return 0
	}
	flips := 0
	prev := 0.0
	for _, v := range h[len(h)-n:] {
		s := 0.0
		den := closes[len(closes)-1]
		if den <= 0 {
			den = 1
		}
		if v/den >= 0.0002 {
			s = 1
		} else if v/den <= -0.0002 {
			s = -1
		}
		if s == 0 {
			continue
		}
		if prev != 0 && s != prev {
			flips++
		}
		prev = s
	}
	return flips
}

// swingLows は直近 lookback 本中のスイング安値 index（古い順）を返す。
func swingLows(closes []float64, order, lookback int) []int {
	n := len(closes)
	if lookback > n {
		lookback = n
	}
	start := n - lookback
	var out []int
	for i := start + order; i < n-order; i++ {
		ok := true
		for j := 1; j <= order; j++ {
			if closes[i] > closes[i-j] || closes[i] > closes[i+j] {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, i)
		}
	}
	return out
}

// swingHighs は直近 lookback 本中のスイング高値 index（古い順）を返す。
func swingHighs(closes []float64, order, lookback int) []int {
	n := len(closes)
	if lookback > n {
		lookback = n
	}
	start := n - lookback
	var out []int
	for i := start + order; i < n-order; i++ {
		ok := true
		for j := 1; j <= order; j++ {
			if closes[i] < closes[i-j] || closes[i] < closes[i+j] {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, i)
		}
	}
	return out
}

// BullishDivergence は強気ダイバージェンスを返す。
// 直近2スイング安値が切り下げなのに指標が切り上げたら true。
func BullishDivergence(closes []float64, ind []float64, order, lookback int) bool {
	lows := swingLows(closes, order, lookback)
	if len(lows) < 2 || len(ind) != len(closes) {
		return false
	}
	a, b := lows[len(lows)-2], lows[len(lows)-1]
	if !(closes[b] < closes[a]) {
		return false
	}
	return ind[b] > ind[a]
}

// BearishDivergence は弱気ダイバージェンスを返す（対称形）。
func BearishDivergence(closes []float64, ind []float64, order, lookback int) bool {
	highs := swingHighs(closes, order, lookback)
	if len(highs) < 2 || len(ind) != len(closes) {
		return false
	}
	a, b := highs[len(highs)-2], highs[len(highs)-1]
	if !(closes[b] > closes[a]) {
		return false
	}
	return ind[b] < ind[a]
}
