package market

import (
	"fmt"
	"time"

	"github.com/cryptoEngine/core/domain"
	"github.com/cryptoEngine/core/infrastructure/exchange"
	"github.com/cryptoEngine/core/infrastructure/logging"
)

type UseCase struct {
	client exchange.MarketDataProvider
	logger *logging.Logger
}

func NewUseCase(client exchange.MarketDataProvider, logger *logging.Logger) *UseCase {
	return &UseCase{
		client: client,
		logger: logger,
	}
}

type FetchOrderBookInput struct {
	Symbol string
	Limit  int
}

type FetchOrderBookOutput struct {
	OrderBook *domain.OrderBook `json:"order_book"`
	LatencyMs int64             `json:"latency_ms"`
}

func (uc *UseCase) FetchOrderBook(input FetchOrderBookInput) (*FetchOrderBookOutput, error) {
	start := time.Now()
	uc.logger.Info("FetchOrderBook: symbol=%s limit=%d", input.Symbol, input.Limit)

	ob, err := uc.client.FetchOrderBook(input.Symbol, input.Limit)
	if err != nil {
		uc.logger.Error("FetchOrderBook failed: %v", err)
		return nil, fmt.Errorf("market usecase: %w", err)
	}

	latency := time.Since(start).Milliseconds()
	uc.logger.InfoWithElapsed(start, "FetchOrderBook completed: symbol=%s spread=%.8f", input.Symbol, ob.Spread)

	return &FetchOrderBookOutput{
		OrderBook: ob,
		LatencyMs: latency,
	}, nil
}

type FetchTickerInput struct {
	Symbol string
}

type FetchTickerOutput struct {
	Ticker    *domain.Ticker `json:"ticker"`
	LatencyMs int64          `json:"latency_ms"`
}

func (uc *UseCase) FetchTicker(input FetchTickerInput) (*FetchTickerOutput, error) {
	start := time.Now()
	uc.logger.Info("FetchTicker: symbol=%s", input.Symbol)

	ticker, err := uc.client.FetchTicker(input.Symbol)
	if err != nil {
		uc.logger.Error("FetchTicker failed: %v", err)
		return nil, fmt.Errorf("market usecase: %w", err)
	}

	latency := time.Since(start).Milliseconds()
	uc.logger.InfoWithElapsed(start, "FetchTicker completed: symbol=%s last=%.2f", input.Symbol, ticker.Last)

	return &FetchTickerOutput{
		Ticker:    ticker,
		LatencyMs: latency,
	}, nil
}
