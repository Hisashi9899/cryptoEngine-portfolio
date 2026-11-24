package trading

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/cryptoEngine/core/domain"
)

// fakeOrderExecutor は OrderExecutor のモック。エラーを注入できる。
type fakeOrderExecutor struct {
	err     error
	orders  int
	lastQty float64
}

func (f *fakeOrderExecutor) MarketOrder(_ context.Context, symbol string, side domain.TradeSide, quantity float64) (*OrderResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.orders++
	f.lastQty = quantity
	return &OrderResult{
		Symbol: symbol, Side: side, Price: 55000, Quantity: quantity,
		OrderID: fmt.Sprintf("fake-%d", f.orders), Status: "filled", Timestamp: 1700000000000,
	}, nil
}

func TestAutoTradeDisabledSkips(t *testing.T) {
	t.Setenv("TRADE_SIZE_USDT", "50")
	store := newFakePositionStore()
	log := NewTradeLogTo(filepath.Join(t.TempDir(), "log.csv"))
	exec := &fakeOrderExecutor{}
	risk := NewRiskManager(newTestLogger())
	risk.SetEnabled(false)

	at := NewAutoTradeUseCase(exec, store, risk, log, newTestLogger())
	out, err := at.ExecuteOrder(context.Background(), ExecuteOrderInput{
		Symbol: "BTCUSDT", Side: domain.TradeBuy, Price: 55000, Reason: ReasonSignal,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != "skipped" {
		t.Errorf("expected skipped, got %s", out.Status)
	}
	if exec.orders != 0 {
		t.Errorf("expected no orders when disabled, got %d", exec.orders)
	}
}

func TestAutoTradeBuyFilledAndPosition(t *testing.T) {
	t.Setenv("TRADE_SIZE_USDT", "50")
	t.Setenv("MAX_POSITION_SIZE_USDT", "100")
	store := newFakePositionStore()
	log := NewTradeLogTo(filepath.Join(t.TempDir(), "log.csv"))
	exec := &fakeOrderExecutor{}
	risk := NewRiskManager(newTestLogger())
	risk.SetEnabled(true)

	at := NewAutoTradeUseCase(exec, store, risk, log, newTestLogger())
	out, err := at.ExecuteOrder(context.Background(), ExecuteOrderInput{
		Symbol: "BTCUSDT", Side: domain.TradeBuy, Price: 50000, Reason: ReasonSignal,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != "filled" {
		t.Errorf("expected filled, got %s (%s)", out.Status, out.Message)
	}
	// quantity = 50 / 50000 = 0.001
	if out.Order.Quantity != 0.001 {
		t.Errorf("quantity: expected 0.001, got %v", out.Order.Quantity)
	}
	if out.Position == nil || out.Position.Volume != 0.001 {
		t.Errorf("expected position 0.001 BTC, got %+v", out.Position)
	}
	if out.PnlUsdt != 0 {
		t.Errorf("expected no pnl on buy, got %v", out.PnlUsdt)
	}

	// trade log に BUY が記録されている。
	entries, _ := log.Read(10)
	if len(entries) != 1 || entries[0].Action != "BUY" {
		t.Fatalf("expected 1 BUY log entry, got %+v", entries)
	}
}

func TestAutoTradeLimitUnsupportedExecutor(t *testing.T) {
	t.Setenv("TRADE_SIZE_USDT", "50")
	t.Setenv("MAX_POSITION_SIZE_USDT", "100")
	store := newFakePositionStore()
	log := NewTradeLogTo(filepath.Join(t.TempDir(), "log.csv"))
	// fakeOrderExecutor は LimitOrder を持たない。
	exec := &fakeOrderExecutor{}
	risk := NewRiskManager(newTestLogger())
	risk.SetEnabled(true)

	at := NewAutoTradeUseCase(exec, store, risk, log, newTestLogger())
	_, err := at.ExecuteOrder(context.Background(), ExecuteOrderInput{
		Symbol: "BTC", Side: domain.TradeBuy, Price: 12000000,
		Reason: ReasonSignal, OrderType: "limit", LimitPrice: 11900000,
	})
	if err == nil {
		t.Fatal("expected limit-unsupported error, got nil")
	}

	// 指値価格なしも即時失敗する。
	_, err = at.ExecuteOrder(context.Background(), ExecuteOrderInput{
		Symbol: "BTC", Side: domain.TradeBuy, Price: 12000000,
		Reason: ReasonSignal, OrderType: "limit",
	})
	if err == nil {
		t.Fatal("expected missing-price error, got nil")
	}
	if exec.orders != 0 {
		t.Errorf("no orders should be placed, got %d", exec.orders)
	}
}

func TestAutoTradeBuyRejectedOverMaxPosition(t *testing.T) {
	t.Setenv("TRADE_SIZE_USDT", "50")
	t.Setenv("MAX_POSITION_SIZE_USDT", "100")
	store := newFakePositionStore()
	// 保有 80 USDT 相当のポジションを作る（volume 0.0016 @ 50000 = 80）。
	if err := store.UpsertPosition(context.Background(), &domain.Position{
		Symbol: "BTCUSDT", Side: domain.TradeBuy, Volume: 0.0016, AvgPrice: 50000, LastPrice: 50000,
		MarketValue: 80, Timestamp: 1,
	}); err != nil {
		t.Fatalf("seed position: %v", err)
	}
	log := NewTradeLogTo(filepath.Join(t.TempDir(), "log.csv"))
	exec := &fakeOrderExecutor{}
	risk := NewRiskManager(newTestLogger())
	risk.SetEnabled(true)

	at := NewAutoTradeUseCase(exec, store, risk, log, newTestLogger())
	out, err := at.ExecuteOrder(context.Background(), ExecuteOrderInput{
		Symbol: "BTCUSDT", Side: domain.TradeBuy, Price: 50000, Reason: ReasonSignal,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 80 + 50 = 130 > 100 → 拒否。
	if out.Status != "rejected" {
		t.Errorf("expected rejected, got %s", out.Status)
	}
	if exec.orders != 0 {
		t.Errorf("expected no order on rejection, got %d", exec.orders)
	}
	// 拒否理由が trade log に記録される。
	entries, _ := log.Read(10)
	if len(entries) != 1 || entries[0].Status != "rejected" || entries[0].Reason != ReasonRisk {
		t.Fatalf("expected rejected log entry, got %+v", entries)
	}
}

func TestAutoTradeSellFilledPnlAndLog(t *testing.T) {
	t.Setenv("TRADE_SIZE_USDT", "50")
	store := newFakePositionStore()
	// 0.001 BTC @ 50000 を保有している前提。
	if err := store.UpsertPosition(context.Background(), &domain.Position{
		Symbol: "BTCUSDT", Side: domain.TradeBuy, Volume: 0.001, AvgPrice: 50000, LastPrice: 50000,
		MarketValue: 50, Timestamp: 1,
	}); err != nil {
		t.Fatalf("seed position: %v", err)
	}
	log := NewTradeLogTo(filepath.Join(t.TempDir(), "log.csv"))
	exec := &fakeOrderExecutor{}
	risk := NewRiskManager(newTestLogger())
	risk.SetEnabled(true)

	at := NewAutoTradeUseCase(exec, store, risk, log, newTestLogger())
	out, err := at.ExecuteOrder(context.Background(), ExecuteOrderInput{
		Symbol: "BTCUSDT", Side: domain.TradeSell, Price: 50000, Reason: ReasonSignal,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != "filled" {
		t.Errorf("expected filled, got %s (%s)", out.Status, out.Message)
	}
	if out.Position == nil || out.Position.Volume != 0 {
		t.Errorf("expected position closed, got %+v", out.Position)
	}
	// 約定価格 55000（フェイク）で売ったため 粗 pnl = (55000-50000)*0.001 = 5。
	// 手数料控除会計：fake executor は非GMO扱い（片道 0.001）のため
	// 往復 2*0.001*55000*0.001 = 0.11 を控除し、報告は 4.89 になる。
	if out.PnlUsdt != 4.89 {
		t.Errorf("expected net pnl 4.89, got %v", out.PnlUsdt)
	}
	entries, _ := log.Read(10)
	if len(entries) != 1 || entries[0].Action != "SELL" || entries[0].Pnl == nil {
		t.Fatalf("expected SELL log with pnl, got %+v", entries)
	}
}

func TestAutoTradeOrderErrorLogged(t *testing.T) {
	t.Setenv("TRADE_SIZE_USDT", "50")
	store := newFakePositionStore()
	log := NewTradeLogTo(filepath.Join(t.TempDir(), "log.csv"))
	exec := &fakeOrderExecutor{err: fmt.Errorf("binance down")}
	risk := NewRiskManager(newTestLogger())
	risk.SetEnabled(true)

	at := NewAutoTradeUseCase(exec, store, risk, log, newTestLogger())
	_, err := at.ExecuteOrder(context.Background(), ExecuteOrderInput{
		Symbol: "BTCUSDT", Side: domain.TradeBuy, Price: 50000, Reason: ReasonSignal,
	})
	if err == nil {
		t.Fatal("expected error when order fails")
	}
	entries, _ := log.Read(10)
	if len(entries) != 1 || entries[0].Status != "error" {
		t.Fatalf("expected error log entry, got %+v", entries)
	}
}
