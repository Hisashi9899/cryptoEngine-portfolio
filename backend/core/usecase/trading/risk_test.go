package trading

import (
	"io"
	"testing"

	"github.com/cryptoEngine/core/infrastructure/logging"
)

func newTestLogger() *logging.Logger {
	return logging.NewLoggerTo(io.Discard)
}

func TestRiskAllowBuyLimit(t *testing.T) {
	t.Setenv("MAX_POSITION_SIZE_USDT", "100")
	t.Setenv("TRADE_SIZE_USDT", "50")
	r := NewRiskManager(newTestLogger())
	r.SetEnabled(true)

	// 保有 0 + 50 ≤ 100 → OK
	if ok, msg := r.AllowBuy(0); !ok {
		t.Fatalf("expected allow for 0 position, got %q", msg)
	}
	// 保有 60 + 50 = 110 > 100 → 拒否
	if ok, _ := r.AllowBuy(60); ok {
		t.Fatal("expected reject for position near limit")
	}
	// 保有 50 + 50 = 100 ≤ 100 → OK（境界）
	if ok, _ := r.AllowBuy(50); !ok {
		t.Fatal("expected allow at boundary")
	}
}

func TestRiskDailyLossStopsAndDisables(t *testing.T) {
	t.Setenv("MAX_DAILY_LOSS_USDT", "20")
	t.Setenv("STOP_ON_LOSS", "true")
	r := NewRiskManager(newTestLogger())
	r.SetEnabled(true)

	if stopped, _ := r.RecordTrade(-5); stopped {
		t.Fatal("not expected to stop at -5")
	}
	if !r.Enabled() {
		t.Fatal("expected enabled after -5 loss")
	}
	// 累積 -19 まで OK。
	if stopped, _ := r.RecordTrade(-14); stopped {
		t.Fatal("not expected to stop at -19 (< 20)")
	}
	// -5 追加で -24 ≥ 20 → 停止 + 自動 OFF。
	stopped, reason := r.RecordTrade(-5)
	if !stopped {
		t.Fatal("expected stop at -24")
	}
	if reason == "" {
		t.Fatal("expected stop reason")
	}
	if r.Enabled() {
		t.Fatal("STOP_ON_LOSS should disable auto trade")
	}
	if !r.Stopped() {
		t.Fatal("expected stopped flag")
	}
}

func TestRiskStopOnLossFalseKeepsEnabled(t *testing.T) {
	t.Setenv("MAX_DAILY_LOSS_USDT", "10")
	t.Setenv("STOP_ON_LOSS", "false")
	r := NewRiskManager(newTestLogger())
	r.SetEnabled(true)
	if stopped, _ := r.RecordTrade(-50); !stopped {
		t.Fatal("expected stopped at -50")
	}
	// STOP_ON_LOSS=false なら enabled は維持（シグナル生成は継続）。
	if !r.Enabled() {
		t.Fatal("STOP_ON_LOSS=false should keep enabled")
	}
	if !r.Stopped() {
		t.Fatal("expected stopped flag")
	}
}

func TestRiskEnabledGate(t *testing.T) {
	r := NewRiskManager(newTestLogger())
	r.SetEnabled(false)
	if r.Enabled() {
		t.Fatal("expected disabled")
	}
	r.SetEnabled(true)
	if !r.Enabled() {
		t.Fatal("expected enabled after SetEnabled(true)")
	}
	if r.Stopped() {
		t.Fatal("expected stopped cleared on enable")
	}
}

func TestRiskStatusSnapshot(t *testing.T) {
	t.Setenv("MAX_POSITION_SIZE_USDT", "200")
	t.Setenv("MAX_DAILY_LOSS_USDT", "30")
	t.Setenv("TRADE_SIZE_USDT", "50")
	r := NewRiskManager(newTestLogger())
	status := r.Status(75.5)
	if status.MaxPositionSizeUsdt != 200 || status.MaxDailyLossUsdt != 30 || status.TradeSizeUsdt != 50 {
		t.Errorf("unexpected status values: %+v", status)
	}
	if status.PositionValueUsdt != 75.5 {
		t.Errorf("positionValue: expected 75.5, got %v", status.PositionValueUsdt)
	}
}
