package exchange

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/cryptoEngine/core/domain"
)

// defaultTickerPath は Binance 公開 REST の 24h ticker エンドポイント。
// EXCHANGE_TICKER_PATH 環境変数で上書きできる。
const defaultTickerPath = "/api/v3/ticker/24hr"

type Client struct {
	httpClient *http.Client
	baseURL    string
}

func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: timeout},
		baseURL:    baseURL,
	}
}

// publicTickerResponse は ticker 応答。旧 mock 形式（last_price/bid/ask）と
// Binance 形式（lastPrice/bidPrice/askPrice）の両方を受け付ける。
type publicTickerResponse struct {
	LastPrice string `json:"last_price"`
	Volume    string `json:"volume"`
	Bid       string `json:"bid"`
	Ask       string `json:"ask"`

	BinanceLastPrice string `json:"lastPrice"`
	BinanceBidPrice  string `json:"bidPrice"`
	BinanceAskPrice  string `json:"askPrice"`
}

type publicOrderBookResponse struct {
	Bids [][]string `json:"bids"`
	Asks [][]string `json:"asks"`
}

func (c *Client) FetchTicker(symbol string) (*domain.Ticker, error) {
	tickerPath := os.Getenv("EXCHANGE_TICKER_PATH")
	if tickerPath == "" {
		tickerPath = defaultTickerPath
	}
	url := fmt.Sprintf("%s%s?symbol=%s", c.baseURL, tickerPath, symbol)
	body, err := c.doGet(url)
	if err != nil {
		return nil, fmt.Errorf("fetch ticker: %w", err)
	}

	var raw publicTickerResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal ticker: %w", err)
	}

	ticker := &domain.Ticker{
		Symbol: symbol,
	}

	lastPrice := raw.LastPrice
	bid := raw.Bid
	ask := raw.Ask
	if lastPrice == "" {
		lastPrice = raw.BinanceLastPrice
	}
	if bid == "" {
		bid = raw.BinanceBidPrice
	}
	if ask == "" {
		ask = raw.BinanceAskPrice
	}

	fmt.Sscanf(lastPrice, "%f", &ticker.Last)
	fmt.Sscanf(bid, "%f", &ticker.Bid)
	fmt.Sscanf(ask, "%f", &ticker.Ask)
	fmt.Sscanf(raw.Volume, "%f", &ticker.Volume)

	return ticker, nil
}

func (c *Client) FetchOrderBook(symbol string, limit int) (*domain.OrderBook, error) {
	url := fmt.Sprintf("%s/public/orderbook?symbol=%s&limit=%d", c.baseURL, symbol, limit)
	body, err := c.doGet(url)
	if err != nil {
		return nil, fmt.Errorf("fetch orderbook: %w", err)
	}

	var raw publicOrderBookResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal orderbook: %w", err)
	}

	ob := &domain.OrderBook{
		Symbol:    symbol,
		Timestamp: time.Now().UnixMilli(),
	}

	for _, bid := range raw.Bids {
		if len(bid) >= 2 {
			var price, volume float64
			fmt.Sscanf(bid[0], "%f", &price)
			fmt.Sscanf(bid[1], "%f", &volume)
			ob.Bids = append(ob.Bids, domain.Order{Price: price, Volume: volume})
		}
	}

	for _, ask := range raw.Asks {
		if len(ask) >= 2 {
			var price, volume float64
			fmt.Sscanf(ask[0], "%f", &price)
			fmt.Sscanf(ask[1], "%f", &volume)
			ob.Asks = append(ob.Asks, domain.Order{Price: price, Volume: volume})
		}
	}

	if len(ob.Bids) > 0 && len(ob.Asks) > 0 {
		bestBid := ob.Bids[0].Price
		bestAsk := ob.Asks[0].Price
		ob.MidPrice = (bestBid + bestAsk) / 2.0
		ob.Spread = bestAsk - bestBid
	}

	return ob, nil
}

func (c *Client) doGet(url string) ([]byte, error) {
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}
