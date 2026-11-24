package trading

import (
	"testing"

	"github.com/cryptoEngine/core/domain"
)

func testSignal(symbol string, typ domain.SignalType, conf, price float64) *domain.Signal {
	return &domain.Signal{Symbol: symbol, Type: typ, Confidence: conf, MidPrice: price, Reason: "test"}
}

func TestSelectBest(t *testing.T) {
	cands := []Candidate{
		{Symbol: "BTC", Signal: testSignal("BTC", domain.Hold, 0, 100), HasOpenPos: false},
		{Symbol: "XRP", Signal: testSignal("XRP", domain.Buy, 0.7, 220), HasOpenPos: false},
		{Symbol: "ETH", Signal: testSignal("ETH", domain.Buy, 0.9, 380000), HasOpenPos: true},  // ナンピン除外
		{Symbol: "SOL", Signal: testSignal("SOL", domain.Sell, 0.8, 15000), HasOpenPos: false}, // 空売り除外
		{Symbol: "DOGE", Signal: testSignal("DOGE", domain.Sell, 0.85, 20), HasOpenPos: true},
	}
	got := SelectBest(cands, 0)
	if got == nil || got.Symbol != "DOGE" || got.Side != domain.TradeSell {
		t.Fatalf("expected DOGE sell, got %+v", got)
	}

	// 信頼度ゲート。
	if got := SelectBest(cands, 0.9); got != nil {
		t.Errorf("minConfidence=0.9 should filter all, got %+v", got)
	}

	// 全 HOLD は nil。
	hold := []Candidate{{Symbol: "X", Signal: testSignal("X", domain.Hold, 0, 1)}}
	if got := SelectBest(hold, 0); got != nil {
		t.Errorf("all-hold should return nil, got %+v", got)
	}

	// nil シグナルは無視。
	mixed := append(hold, Candidate{Symbol: "Y"})
	if got := SelectBest(mixed, 0); got != nil {
		t.Errorf("nil signals should return nil, got %+v", got)
	}
}

func TestSizeForNotional(t *testing.T) {
	// XRP: 想定 2200 円では最小 10 枚（2200 円）に満たないため引き上げ。
	qty, ok := SizeForNotional(1000, 220, 10, 10, 10000, 5000)
	if !ok || qty != 10 {
		t.Errorf("got qty=%v ok=%v, want 10 true", qty, ok)
	}
	// 余力不足は見送り。
	if _, ok := SizeForNotional(1000, 220, 10, 10, 10000, 1000); ok {
		t.Error("insufficient balance should return ok=false")
	}
	// 上限超過は見送り。
	if _, ok := SizeForNotional(1000, 220, 10, 10, 2000, 5000); ok {
		t.Error("over max position should return ok=false")
	}
	// BTC: 0.001 刻み。
	qty, ok = SizeForNotional(12000, 12000000, 0.001, 0.001, 0, 0)
	if !ok || qty != 0.001 {
		t.Errorf("got qty=%v ok=%v, want 0.001 true", qty, ok)
	}
	// 想定額 0 は最小額面への自動引き上げ（正常）。
	qty, ok = SizeForNotional(0, 220, 10, 10, 0, 0)
	if !ok || qty != 10 {
		t.Errorf("got qty=%v ok=%v, want 10 true", qty, ok)
	}
	// 不正入力。
	for _, tc := range [][6]float64{{100, 0, 1, 1, 0, 0}, {100, 1, 0, 1, 0, 0}, {100, 1, 1, 0, 0, 0}} {
		if _, ok := SizeForNotional(tc[0], tc[1], tc[2], tc[3], tc[4], tc[5]); ok {
			t.Errorf("invalid input %v should return ok=false", tc)
		}
	}
}

func TestSizeForSignal(t *testing.T) {
	base := GearParams{TradeSize: 2200, MaxPos: 10000, Balance: 5000, RiskFraction: 0.2, SpreadRef: 0.0005}
	// XRP: 下限 2201 → 信頼度 0.8 で 1.3 倍 → 2861 → 余力上限 1000 超で見送り。
	if _, ok := SizeForSignal(2200, 220, 1, 1, 0.8, 0.0001, base); ok {
		t.Error("over risk-fraction should return ok=false")
	}
	// 余力率を上げれば約定サイズ 13。
	big := base
	big.RiskFraction = 1.0
	qty, ok := SizeForSignal(2200, 220, 1, 1, 0.8, 0.0001, big)
	if !ok || qty != 13 {
		t.Errorf("got qty=%v ok=%v, want 13 true", qty, ok)
	}
	// スプレッド悪化で減速: spread 0.002 → mult 0.25 → 2201*1.3*0.25=715 → 3 枚。
	qty, ok = SizeForSignal(2200, 220, 1, 1, 0.8, 0.002, big)
	if !ok || qty != 3 {
		t.Errorf("got qty=%v ok=%v, want 3 true", qty, ok)
	}
	// 信頼度 0 → 0.5 倍。
	qty, ok = SizeForSignal(2200, 220, 1, 1, 0, 0.0001, big)
	if !ok || qty != 5 {
		t.Errorf("got qty=%v, want 5", qty)
	}
	// 上限・不正入力。
	if _, ok := SizeForSignal(20000, 220, 1, 1, 0.8, 0.0001, big); ok {
		t.Error("over maxPos should return ok=false")
	}
	if _, ok := SizeForSignal(2200, 0, 1, 1, 0.5, 0.0001, big); ok {
		t.Error("zero price should return ok=false")
	}
}

func TestGMOSymbolRule(t *testing.T) {
	min, step, ok := GMOSymbolRule("XRP")
	if !ok || min != 1 || step != 1 {
		t.Errorf("XRP rule = %v/%v/%v", min, step, ok)
	}
	min, step, ok = GMOSymbolRule("BTC")
	if !ok || min != 0.00001 || step != 0.00001 {
		t.Errorf("BTC rule = %v/%v/%v", min, step, ok)
	}
	if _, _, ok := GMOSymbolRule("YYY"); ok {
		t.Error("unknown symbol should return ok=false")
	}
}
