package main

import "strings"

// vt_cost.go は「1トレード期待値 ÷ コスト」を改善するための執行ゲート群
// （純粋関数・テスト対象）。判定ロジック（EvalEntry 等）は変えず、
// 執行・記録層でのみ使う。出口には適用しない（塩漬け防止）。
//
//   - feeModeRates：執行モード別の片道手数料率。受入判定の歪みを正す。
//     taker  = GMO現物 0.05%/片道（往復0.1%）
//     maker  = エントリー -0.01%（リベート）＋イグジット 0.05%
//     conservative = 従来の 0.15%/片道（往復0.3%）
//   - costGateAllowed：ATR 比率（狙える値動きの代理）が往復コスト×k を
//     下回る入口を見送る。ATR 不明は fail-closed（測れない玉は撃たない）。
//   - cooldownBlocked：決済直後の同一銘柄への即再入を抑え、回転を落とす。
//   - corrCluster：相関クラスタ。同時保有をクラスタ単位に制限し、
//     見かけの分散を実分散へ近づけて CI 下限を締める。
//   - volScale：ボラ逆比例の等リスク数量倍率（target 未設定なら 1）。

// feeModeRates は執行モードの片道手数料率（entry, exit）を返す。
// 未知のモードは ok=false（呼び出し側は従来の --fee-rate を使う）。
func feeModeRates(mode string) (entry, exit float64, ok bool) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "taker":
		return 0.0005, 0.0005, true
	case "maker":
		return -0.0001, 0.0005, true
	case "conservative":
		return 0.0015, 0.0015, true
	}
	return 0, 0, false
}

// costGateAllowed は入口のコスト・ゲート判定を返す（純粋関数・テスト対象）。
// k<=0 は無効（常に通す）。atrRatio<=0（足不足・不明）は fail-closed。
// 往復コスト = entryRate + exitRate + spreadRatio。負（リベート優勢）は 0 扱い。
func costGateAllowed(atrRatio, spreadRatio, entryRate, exitRate, k float64) (bool, string) {
	if k <= 0 {
		return true, ""
	}
	if atrRatio <= 0 {
		return false, "cost edge unknown"
	}
	cost := entryRate + exitRate
	if spreadRatio > 0 {
		cost += spreadRatio
	}
	if cost <= 0 {
		return true, ""
	}
	if atrRatio+1e-12 >= k*cost {
		return true, ""
	}
	return false, "cost edge thin"
}

// makerCrossed は指値待機が約定したかを判定する（シミュレーション用）。
// crossBps<=0 は従来のタッチ約定（mid が指値に到達＝即約定）。
// crossBps>0 は「mid が指値を crossBps 以上貫通」したときだけ約定とみなし、
// キュー待ち・板が厚い場面での過大評価（タッチ即約定）を抑える。
// side は "buy"/"sell"（buy は指値以下、sell は指値以上で約定）。
func makerCrossed(side string, mid, limit, crossBps float64) bool {
	if limit <= 0 || mid <= 0 {
		return false
	}
	buf := limit * crossBps / 1e4
	if strings.EqualFold(strings.TrimSpace(side), "buy") {
		return mid <= limit-buf
	}
	return mid >= limit+buf
}

// cooldownBlocked は直近決済からの経過が cooldownSec 未満かを返す。
// lastExitMs<=0（未決済）・cooldownSec<=0 は偽（無効）。
func cooldownBlocked(nowMs, lastExitMs int64, cooldownSec float64) bool {
	if cooldownSec <= 0 || lastExitMs <= 0 {
		return false
	}
	return float64(nowMs-lastExitMs) < cooldownSec*1000
}

// corrCluster は銘柄の相関クラスタ名を返す（未知は "other"）。
// crypto は単一因子に引きずられやすいため、同一クラスタの同時保有を
// 1件に制限するための鍵。判定ロジックではなくポートフォリオ統制に使う。
func corrCluster(symbol string) string {
	switch strings.ToUpper(strings.TrimSpace(symbol)) {
	case "BTC", "BCH", "LTC":
		return "btc"
	case "ETH":
		return "eth"
	case "XRP", "XLM":
		return "xrp"
	case "ADA", "SOL", "DOT", "ATOM", "SUI", "LINK":
		return "smartcontract"
	case "DOGE", "NAC", "WILD":
		return "meme"
	case "ASTR", "FCR":
		return "alt"
	}
	return "other"
}

// sameCluster は2銘柄が同一相関クラスタかを返す。
func sameCluster(a, b string) bool {
	ca, cb := corrCluster(a), corrCluster(b)
	return ca != "other" && ca == cb
}

// volScale は ATR 逆比例の等リスク数量倍率を返す（0.5〜1.5 にクランプ）。
// target<=0 または atrRatio<=0 は 1（無効＝従来数量）。
func volScale(atrRatio, target float64) float64 {
	if target <= 0 || atrRatio <= 0 {
		return 1
	}
	s := target / atrRatio
	if s < 0.5 {
		return 0.5
	}
	if s > 1.5 {
		return 1.5
	}
	return s
}
