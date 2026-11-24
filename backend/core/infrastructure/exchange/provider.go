package exchange

import (
	"time"

	"github.com/cryptoEngine/core/domain"
)

// MarketDataProvider は取引所の市場データ取得を抽象化するインターフェース。
// Binance（既存 Client）と GMO コインが実装する。スプレッド・板・ティッカーの
// 取得形式の差異を吸収し、上位層（capture / paper / backtest）は取引所を
// 意識しない。シンボル体系は各実装が正規化する（例: BTCUSDT -> BTC）。
type MarketDataProvider interface {
	FetchTicker(symbol string) (*domain.Ticker, error)
	FetchOrderBook(symbol string, limit int) (*domain.OrderBook, error)
}

// ExchangeID は対応取引所の識別子。EXCHANGE_ID 環境変数で選択する。
type ExchangeID string

const (
	ExchangeBinance ExchangeID = "binance"
	ExchangeGMO     ExchangeID = "gmo"
)

// DefaultTimeout は市場データ取得の HTTP タイムアウト。
const DefaultTimeout = 10 * time.Second

var _ MarketDataProvider = (*Client)(nil)
