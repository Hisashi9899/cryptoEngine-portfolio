package trading

import (
	"context"
	"testing"

	"github.com/cryptoEngine/core/domain"
)

// fakePositionStore は PositionStore のインメモリ実装。
type fakePositionStore struct {
	positions map[string]*domain.Position
}

func newFakePositionStore() *fakePositionStore {
	return &fakePositionStore{positions: map[string]*domain.Position{}}
}

func (f *fakePositionStore) GetPosition(_ context.Context, symbol string) (*domain.Position, error) {
	return f.positions[symbol], nil
}

func (f *fakePositionStore) UpsertPosition(_ context.Context, p *domain.Position) error {
	f.positions[p.Symbol] = p
	return nil
}

func TestApplyBuyAvgPriceRecalculation(t *testing.T) {
	store := newFakePositionStore()
	pm := NewPositionManager(store, newTestLogger())

	// 1 回目: 1 BTC @ 50000
	pos, err := pm.ApplyBuy(context.Background(), "BTCUSDT", 50000, 1.0, 1000)
	if err != nil {
		t.Fatalf("buy 1: %v", err)
	}
	if pos.Volume != 1.0 || pos.AvgPrice != 50000 {
		t.Fatalf("buy1: got volume=%v avg=%v", pos.Volume, pos.AvgPrice)
	}

	// 2 回目: 1 BTC @ 60000 → 平均 (1*50000 + 1*60000)/2 = 55000
	pos, err = pm.ApplyBuy(context.Background(), "BTCUSDT", 60000, 1.0, 2000)
	if err != nil {
		t.Fatalf("buy 2: %v", err)
	}
	if pos.Volume != 2.0 {
		t.Errorf("volume: expected 2, got %v", pos.Volume)
	}
	if pos.AvgPrice != 55000 {
		t.Errorf("avgPrice: expected 55000, got %v", pos.AvgPrice)
	}
	if pos.MarketValue != 2.0*60000 {
		t.Errorf("marketValue: expected %v, got %v", 2.0*60000, pos.MarketValue)
	}
}

func TestApplySellPnlAndRemaining(t *testing.T) {
	store := newFakePositionStore()
	pm := NewPositionManager(store, newTestLogger())
	_, _ = pm.ApplyBuy(context.Background(), "BTCUSDT", 50000, 1.0, 1000)

	// 0.4 BTC @ 55000 → pnl = (55000-50000)*0.4 = 2000 USDT
	pnl, remaining, err := pm.ApplySell(context.Background(), "BTCUSDT", 55000, 0.4, 2000)
	if err != nil {
		t.Fatalf("sell: %v", err)
	}
	if pnl != 2000 {
		t.Errorf("pnl: expected 2000, got %v", pnl)
	}
	if remaining.Volume != 0.6 {
		t.Errorf("remaining: expected 0.6, got %v", remaining.Volume)
	}

	// 全量クローズ（残 0.6）→ pnl = (55000-50000)*0.6 = 3000
	pnl, remaining, err = pm.ApplySell(context.Background(), "BTCUSDT", 55000, 0.6, 3000)
	if err != nil {
		t.Fatalf("sell all: %v", err)
	}
	if pnl != 3000 {
		t.Errorf("pnl all: expected 3000, got %v", pnl)
	}
	if remaining.Volume != 0 {
		t.Errorf("remaining after close: expected 0, got %v", remaining.Volume)
	}
}

func TestApplySellOverPositionClamped(t *testing.T) {
	store := newFakePositionStore()
	pm := NewPositionManager(store, newTestLogger())
	_, _ = pm.ApplyBuy(context.Background(), "BTCUSDT", 50000, 1.0, 1000)

	pnl, remaining, err := pm.ApplySell(context.Background(), "BTCUSDT", 50000, 5.0, 2000)
	if err != nil {
		t.Fatalf("sell: %v", err)
	}
	if pnl != 0 {
		t.Errorf("pnl: expected 0 (break-even), got %v", pnl)
	}
	if remaining.Volume != 0 {
		t.Errorf("remaining: expected 0, got %v", remaining.Volume)
	}
}

func TestApplySellWithoutPosition(t *testing.T) {
	store := newFakePositionStore()
	pm := NewPositionManager(store, newTestLogger())
	pnl, remaining, err := pm.ApplySell(context.Background(), "BTCUSDT", 50000, 0.1, 1000)
	if err != nil {
		t.Fatalf("sell: %v", err)
	}
	if pnl != 0 || remaining != nil {
		t.Errorf("expected nil/zero for empty position, got pnl=%v remaining=%v", pnl, remaining)
	}
}
