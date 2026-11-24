package trading

import (
	"context"
	"fmt"
	"math"

	"github.com/cryptoEngine/core/domain"
	"github.com/cryptoEngine/core/infrastructure/logging"
)

// PositionStore はポジション永続化の抽象化。repository.PositionRepository が実装。
// テストではインメモリのフェイクを注入できる。
type PositionStore interface {
	GetPosition(ctx context.Context, symbol string) (*domain.Position, error)
	UpsertPosition(ctx context.Context, position *domain.Position) error
}

// PositionManager はポジションの増減と平均取得価格の再計算を行う。
//
// - BUY: (保有数量 × 平均価格 + 買数量 × 約定価格) / (保有数量 + 買数量) で平均価格を再計算
// - SELL: 保有数量を減らし、確定損益（USDT）を計算
//
// 時価・評価損益は注文時点の約定価格で計算して保存する（表示用途）。
type PositionManager struct {
	store  PositionStore
	logger *logging.Logger
}

func NewPositionManager(store PositionStore, logger *logging.Logger) *PositionManager {
	return &PositionManager{store: store, logger: logger}
}

// Current は現在のポジションを返す。未保有なら nil。
func (pm *PositionManager) Current(ctx context.Context, symbol string) (*domain.Position, error) {
	return pm.store.GetPosition(ctx, symbol)
}

// MarketValue は現在ポジションの評価額（数量 × 現在価格）を返す。
func (pm *PositionManager) MarketValue(ctx context.Context, symbol string, currentPrice float64) (float64, error) {
	pos, err := pm.store.GetPosition(ctx, symbol)
	if err != nil {
		return 0, err
	}
	if pos == nil {
		return 0, nil
	}
	return pos.Volume * currentPrice, nil
}

// ApplyBuy は BUY 約定をポジションに反映する。平均取得価格を再計算し、DB に保存する。
func (pm *PositionManager) ApplyBuy(ctx context.Context, symbol string, price, quantity float64, ts int64) (*domain.Position, error) {
	if quantity <= 0 {
		return nil, fmt.Errorf("buy quantity must be positive: %.8f", quantity)
	}
	if price <= 0 {
		return nil, fmt.Errorf("buy price must be positive (fill price unknown): %.8f", price)
	}
	current, err := pm.store.GetPosition(ctx, symbol)
	if err != nil {
		return nil, err
	}

	newPos := &domain.Position{
		Symbol:    symbol,
		Side:      domain.TradeBuy,
		Volume:    quantity,
		AvgPrice:  price,
		Timestamp: ts,
	}
	if current != nil && current.Volume > 0 {
		totalQty := current.Volume + quantity
		avg := (current.Volume*current.AvgPrice + quantity*price) / totalQty
		newPos.Volume = totalQty
		newPos.AvgPrice = avg
	}

	pm.fillValuation(newPos, price)
	if err := pm.store.UpsertPosition(ctx, newPos); err != nil {
		return nil, fmt.Errorf("upsert position after buy: %w", err)
	}
	pm.logger.Info("position updated (buy): symbol=%s volume=%.8f avgPrice=%.2f", symbol, newPos.Volume, newPos.AvgPrice)
	return newPos, nil
}

// ApplySell は SELL 約定をポジションに反映する。確定損益（USDT）を返す。
// 保有数量を超える SELL は保有全量としてクローズする。
func (pm *PositionManager) ApplySell(ctx context.Context, symbol string, price, quantity float64, ts int64) (pnlUsdt float64, remaining *domain.Position, err error) {
	if quantity <= 0 {
		return 0, nil, fmt.Errorf("sell quantity must be positive: %.8f", quantity)
	}
	if price <= 0 {
		return 0, nil, fmt.Errorf("sell price must be positive (fill price unknown): %.8f", price)
	}
	current, err := pm.store.GetPosition(ctx, symbol)
	if err != nil {
		return 0, nil, err
	}

	// ポジションが無い SELL は損益 0 で返す（エントリー無し）。
	if current == nil || current.Volume <= 0 {
		return 0, nil, nil
	}

	sellQty := math.Min(quantity, current.Volume)
	pnlUsdt = (price - current.AvgPrice) * sellQty

	remainingQty := current.Volume - sellQty
	remaining = &domain.Position{
		Symbol:    symbol,
		Side:      domain.TradeSell,
		Volume:    remainingQty,
		AvgPrice:  current.AvgPrice,
		Timestamp: ts,
	}
	pm.fillValuation(remaining, price)
	if err := pm.store.UpsertPosition(ctx, remaining); err != nil {
		return 0, nil, fmt.Errorf("upsert position after sell: %w", err)
	}
	pm.logger.Info("position updated (sell): symbol=%s sellQty=%.8f pnl=%.6f USDT remaining=%.8f",
		symbol, sellQty, pnlUsdt, remainingQty)
	return round6(pnlUsdt), remaining, nil
}

// fillValuation は評価額・評価損益を約定価格ベースで埋める。
func (pm *PositionManager) fillValuation(pos *domain.Position, lastPrice float64) {
	pos.LastPrice = lastPrice
	pos.MarketValue = round2(pos.Volume * lastPrice)
	cost := pos.Volume * pos.AvgPrice
	if cost > 0 {
		pos.UnrealizedPnl = round2((lastPrice - pos.AvgPrice) * pos.Volume)
		pos.UnrealizedPnlRate = round2((lastPrice - pos.AvgPrice) / pos.AvgPrice * 100)
	}
}

func round6(v float64) float64 {
	return float64(int64(v*1e6+0.5)) / 1e6
}
