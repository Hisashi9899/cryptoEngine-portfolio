package main

import (
	"strings"

	vt "github.com/cryptoEngine/core/usecase/trading/vt"
)

// vt_entry_gate.go は入口候補の採用ゲートを提供する（純粋関数群）。
//
// テクニカル判定（EvalEntry のパターン・指標ロジック）自体には触らず、
// 執行・記録層（live/paper/replay）でのみ使う。出口には一切適用しない
// （離脱を止めると塩漬けになるため）。
//   - minConf > 0：確信度が下限未満の入口を見送る（低確信の誤射削減）。
//     paper 実績で確信度 0.58 未満の勝率25%に対し全体35%前後の実績あり。
//   - shortRSIFloor > 0：売られ過ぎ圏（RSI 下限未満）でのショート入口を
//     見送る（底値売りの逆行回避）。ロングには適用しない。
//   - tobiitaBlocked：飛び板（スプレッド異常拡大）での新規を見送る。
//     ロング・ショート対称に適用する（片側だけ止めると逆側に偏るため）。
//     既定の上限 1%（100bps）は通常の10倍超で、LINK実測16bps前後では
//     発火しない。真の板飛びでのみ止まる安全網。
// 境界値は微差許容（1e-9）で通す。両方 0 で無効（従来動作を完全維持）。

// tobiitaSpreadMax は飛び板とみなすスプレッド率の上限（比率）。
// 0.01＝1%＝100bps。通常（数〜十数bps）では発火しない。
const tobiitaSpreadMax = 0.01

// tobiitaBlocked は飛び板注意で新規を見送るべきかを返す（純粋関数・テスト対象）。
// spreadRatio<=0（不明・ゼロ）は通す（fail-open。冷間始動を殺さない）。
// ロング・ショートの区別なく対称に適用すること（呼び出し側で両sideに使う）。
func tobiitaBlocked(spreadRatio float64) bool {
	if spreadRatio <= 0 {
		return false
	}
	return spreadRatio > tobiitaSpreadMax
}

// entryAllowed は入口候補の採用可否を返す（純粋関数・テスト対象）。
// 戻り値は (ok, reason)。reason はスキップ時のログ・監査用。
func entryAllowed(sig *vt.Signal, minConf, shortRSIFloor float64) (bool, string) {
	if sig == nil {
		return false, "nil signal"
	}
	if minConf > 0 && sig.Confidence+1e-9 < minConf {
		return false, "low confidence"
	}
	if shortRSIFloor > 0 && sig.Type == vt.Sell && sig.RSI < shortRSIFloor {
		return false, "oversold short"
	}
	return true, ""
}

// orphanFailLimit はゾンビ建玉と断定する連続回数（約5秒間隔で60秒相当）。
// 取引所にポジションが無いのにローカル建玉が残る乖離を、無限リトライの前に
// 終端させる。ロング・ショートの区別なく対称に適用する（ closeOpen の共有
// 経路で両sideに使う）。12回未満では従来通り保持＋再試行する（回帰防止）。
const orphanFailLimit = 12

// isOrphanNoPosition は決済失敗が取引所側のポジション不在かを返す
// （純粋関数・テスト対象）。GMO証拠金の実績文言 "no open ... position" を
// 検出する。不明・空は偽（fail-closed。誤クリアを防ぐ）。
func isOrphanNoPosition(msg string) bool {
	m := strings.ToLower(strings.TrimSpace(msg))
	if m == "" {
		return false
	}
	return strings.Contains(m, "no open") && strings.Contains(m, "position")
}

// orphanShouldClear は連続失敗回数が終端域かを返す（純粋関数・テスト対象）。
func orphanShouldClear(consec int) bool {
	return consec >= orphanFailLimit
}

// topWallImb は最良3段の板厚imb ((b-a)/(b+a)) を返す。
// bids/asks は全段の価格・数量列。最良側（bidsは高価格・asksは低価格）から
// 3段を合計する。段数が3未満でもある分で計算する。合計ゼロ・空時は
// ok=false を返し、呼び出し側は AddWall を見送る（不明扱い・fail-open）。
// ロング・ショートの区別なく対称に使う（符号で方向を読むのは Engine 側）。
// top3Sum は最良側から3段の数量合計を返す（desc=trueで高価格順）。
// 挿入ソートで上位3段を選ぶ（段数は高々30のため十分）。
func top3Sum(px, vol []float64, desc bool) float64 {
	n := len(px)
	if len(vol) < n {
		n = len(vol)
	}
	if n <= 0 {
		return 0
	}
	if n > 3 {
		idx := make([]int, n)
		for i := range idx {
			idx[i] = i
		}
		for i := 1; i < n; i++ {
			for j := i; j > 0; j-- {
				a, c := idx[j-1], idx[j]
				swap := px[c] > px[a]
				if !desc {
					swap = px[c] < px[a]
				}
				if !swap {
					break
				}
				idx[j-1], idx[j] = idx[j], idx[j-1]
			}
		}
		sum := 0.0
		for k := 0; k < 3; k++ {
			if v := vol[idx[k]]; v > 0 {
				sum += v
			}
		}
		return sum
	}
	sum := 0.0
	for _, v := range vol[:n] {
		if v > 0 {
			sum += v
		}
	}
	return sum
}

func topWallImb(bidPx, bidVol, askPx, askVol []float64) (float64, bool) {
	b := top3Sum(bidPx, bidVol, true)
	a := top3Sum(askPx, askVol, false)
	if b+a <= 0 {
		return 0, false
	}
	return (b - a) / (b + a), true
}

// topWallSizes は最良3段の買い・売り数量合計を返す。
// topWallImb と同じ段選択（最良側から3段）。壁の出現・消失・
// 食い尽くしのライフサイクル追跡用。両side対称にそのまま使う。
func topWallSizes(bidPx, bidVol, askPx, askVol []float64) (bid3, ask3 float64, ok bool) {
	bid3 = top3Sum(bidPx, bidVol, true)
	ask3 = top3Sum(askPx, askVol, false)
	if bid3+ask3 <= 0 {
		return 0, 0, false
	}
	return bid3, ask3, true
}
