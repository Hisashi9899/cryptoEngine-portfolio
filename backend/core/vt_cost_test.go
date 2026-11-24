package main

import (
	"math"
	"testing"

	vt "github.com/cryptoEngine/core/usecase/trading/vt"
)

func almostEq(a, b, eps float64) bool { return math.Abs(a-b) <= eps }

func TestFeeModeRates(t *testing.T) {
	cases := []struct {
		mode        string
		entry, exit float64
		ok          bool
	}{
		{"taker", 0.0005, 0.0005, true},
		{"maker", -0.0001, 0.0005, true},
		{"conservative", 0.0015, 0.0015, true},
		{"", 0, 0, false},
		{"bogus", 0, 0, false},
	}
	for _, c := range cases {
		e, x, ok := feeModeRates(c.mode)
		if ok != c.ok || (!ok && (e != 0 || x != 0)) {
			t.Fatalf("feeModeRates(%q) ok=%v e=%v x=%v", c.mode, ok, e, x)
		}
		if ok && (e != c.entry || x != c.exit) {
			t.Fatalf("feeModeRates(%q) = (%v,%v), want (%v,%v)", c.mode, e, x, c.entry, c.exit)
		}
	}
}

func TestCostGateAllowed(t *testing.T) {
	// k=0 は無効（常に通す）。
	if ok, _ := costGateAllowed(0, 0.0002, 0.0005, 0.0005, 0); !ok {
		t.Fatal("k=0 must be disabled")
	}
	// ATR不明（0）は fail-closed（測れない玉は撃たない）。
	if ok, _ := costGateAllowed(0, 0, 0.0005, 0.0005, 3); ok {
		t.Fatal("atr=0 must fail-closed")
	}
	// コスト = 0.0005+0.0005+0.0002 = 0.0012。k=3 → 0.0036。
	if ok, why := costGateAllowed(0.0035, 0.0002, 0.0005, 0.0005, 3); ok {
		t.Fatalf("thin edge must be blocked, got ok (%s)", why)
	}
	if ok, _ := costGateAllowed(0.0036, 0.0002, 0.0005, 0.0005, 3); !ok {
		t.Fatal("edge exactly k*cost must pass")
	}
	if ok, _ := costGateAllowed(0.0040, 0.0002, 0.0005, 0.0005, 3); !ok {
		t.Fatal("sufficient edge must pass")
	}
	// Maker リベートはコストを下げる（-0.0001+0.0005+0.0002=0.0006）。
	if ok, _ := costGateAllowed(0.0018, 0.0002, -0.0001, 0.0005, 3); !ok {
		t.Fatal("maker rebate should lower cost and pass")
	}
	// 負コストは 0 扱い＝常に通す。
	if ok, _ := costGateAllowed(0.0001, 0, -0.001, -0.001, 3); !ok {
		t.Fatal("net-negative cost must pass")
	}
}

func TestCooldownBlocked(t *testing.T) {
	if cooldownBlocked(1000, 0, 300) {
		t.Fatal("no previous exit must not block")
	}
	if cooldownBlocked(1000, 900, 0) {
		t.Fatal("disabled cooldown must not block")
	}
	if !cooldownBlocked(1000, 900, 1) { // 100ms < 1s
		t.Fatal("within cooldown must block")
	}
	if cooldownBlocked(400000, 1000, 300) {
		t.Fatal("after cooldown must pass")
	}
}

func TestCorrClusters(t *testing.T) {
	if !sameCluster("BTC", "BCH") {
		t.Fatal("BTC/BCH must share cluster")
	}
	if !sameCluster("XRP", "XLM") {
		t.Fatal("XRP/XLM must share cluster")
	}
	if sameCluster("BTC", "ETH") {
		t.Fatal("BTC/ETH must differ")
	}
	if sameCluster("BTC", "UNKNOWN") {
		t.Fatal("unknown cluster must not match")
	}
	if corrCluster("UNKNOWN") != "other" {
		t.Fatal("unknown must be other")
	}
}

func TestVolScale(t *testing.T) {
	if v := volScale(0.004, 0); v != 1 {
		t.Fatalf("target 0 must be 1, got %v", v)
	}
	if v := volScale(0, 0.004); v != 1 {
		t.Fatalf("atr 0 must be 1, got %v", v)
	}
	if v := volScale(0.004, 0.004); v != 1 {
		t.Fatalf("at target must be 1, got %v", v)
	}
	if v := volScale(0.001, 0.004); v != 1.5 {
		t.Fatalf("low vol must clamp 1.5, got %v", v)
	}
	if v := volScale(0.02, 0.004); v != 0.5 {
		t.Fatalf("high vol must clamp 0.5, got %v", v)
	}
}

func TestPairNetPnlFeesRatesMakerRebate(t *testing.T) {
	recs := []vtSignalRecord{
		{Timestamp: 1, Symbol: "BTC", Type: "buy", Price: 100, Qty: 1, Pattern: "p"},
		{Timestamp: 2, Symbol: "BTC", Type: "sell", Price: 110, Qty: 1, Pattern: "exit"},
	}
	// 従来の対称 0.05%（入口価格基準・往復2倍）: fee=0.1, net=9.9。
	nets, fees := pairNetPnlFees(recs, 0.0005)
	if len(nets) != 1 || !almostEq(nets[0], 9.9, 1e-9) || !almostEq(fees, 0.1, 1e-9) {
		t.Fatalf("symmetric: nets=%v fees=%v", nets, fees)
	}
	// maker: entry -0.01% (rebate -0.01), exit 0.05% (0.055) → fee 0.045, net 9.955
	netsM, feesM := pairNetPnlFeesRates(recs, -0.0001, 0.0005)
	if len(netsM) != 1 || !almostEq(netsM[0], 9.955, 1e-9) || !almostEq(feesM, 0.045, 1e-9) {
		t.Fatalf("maker: nets=%v fees=%v", netsM, feesM)
	}
	if netsM[0] <= nets[0] {
		t.Fatal("maker rebate must improve net vs taker")
	}
}

func mkCand(sym string, conf float64) vtLiveCandidate {
	return vtLiveCandidate{symbol: sym, sig: &vt.Signal{Type: vt.Buy, Confidence: conf, PercentB: 0.2, Pattern: "p"}}
}

func TestPickTopKCorrIsolation(t *testing.T) {
	pending := []vtLiveCandidate{mkCand("BTC", 0.9), mkCand("BCH", 0.8), mkCand("ETH", 0.7)}
	// 統制なし：3件すべて選べる。
	picks, _ := pickTopK(pending, 5, map[string]bool{}, 3, 0, 0, 2000)
	if len(picks) != 3 {
		t.Fatalf("no-isolation must pick 3, got %d", len(picks))
	}
	// 統制あり：BTC と BCH は同一クラスタなので片方のみ。ETH は別。
	picks, _ = pickTopKCorr(pending, 5, map[string]bool{}, 3, 0, 0, 2000, true)
	if len(picks) != 2 {
		t.Fatalf("isolation must pick 2, got %d (%v)", len(picks), picks)
	}
	seen := map[string]bool{}
	for _, p := range picks {
		seen[p.symbol] = true
	}
	if seen["BTC"] && seen["BCH"] {
		t.Fatal("must not pick both BTC and BCH")
	}
	if !seen["ETH"] {
		t.Fatal("ETH must be picked")
	}
	// 既存建玉と同一クラスタは除外。
	picks, _ = pickTopKCorr(pending, 5, map[string]bool{"BTC": true}, 3, 1, 0, 2000, true)
	for _, p := range picks {
		if p.symbol == "BCH" {
			t.Fatal("BCH shares open BTC cluster and must be skipped")
		}
	}
}
