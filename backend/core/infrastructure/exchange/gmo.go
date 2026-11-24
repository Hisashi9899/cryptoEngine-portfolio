package exchange

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cryptoEngine/core/domain"
)

// GMO コインの市場データプロバイダ（Public API）。
//
//   - REST: https://api.coin.z.com/public
//   - WebSocket: wss://api.coin.z.com/ws/public/v1（capture 側で使用）
//   - シンボル体系: 現物は短縮形（BTC）。「_JPY」付きはレバレッジ取引のため
//     現物ボットでは使用禁止（2026-09-15 サポート回答で確定）。
//     ToGMOSymbol で Binance 形式等から変換する。
//   - 手数料目安（現物取引所・BTC）: Maker -0.01% / Taker 0.05%。
//     backtest の --fee-bps には 0.0005 を指定すること。
//   - 最小数量・刻み（現物取引所）: 銘柄別。例: BTC は最小 0.00001・刻み 0.00001、
//     XRP は最小 1・刻み 1（/public/v1/symbols 実測・2026-09-15）。
const (
	DefaultGMOBaseURL   = "https://api.coin.z.com/public"
	DefaultGMOWSURL     = "wss://api.coin.z.com/ws/public/v1"
	DefaultGMOSymbol    = "BTC"
	GMOOrderBookChannel = "orderbooks"
	GMOTakerFeeBps      = 0.0005
	// NOTE: 銘柄別最小数量・刻みは usecase/trading の gmoSizeRules が正本
	// （現物の値。レバレッジ「_JPY」の値を混ぜないこと）。
)

// gmoSymbolMap は主要ペアの Binance 形式→GMO 現物形式の変換表。
// 現物は短縮形（BTC）。「_JPY」はレバレッジのため扱わない。
var gmoSymbolMap = map[string]string{
	"BTCUSDT":  "BTC",
	"ETHUSDT":  "ETH",
	"XRPUSDT":  "XRP",
	"SOLUSDT":  "SOL",
	"DOGEUSDT": "DOGE",
}

// gmoSpotSymbols は現物で扱う短縮シンボルの集合。
var gmoSpotSymbols = map[string]bool{
	"BTC": true, "ETH": true, "BCH": true, "LTC": true, "XRP": true,
	"XLM": true, "DOT": true, "ATOM": true, "FCR": true, "ADA": true,
	"LINK": true, "DOGE": true, "SOL": true, "ASTR": true, "NAC": true,
	"WILD": true, "SUI": true,
}

// ToGMOSymbol はシンボルを GMO 現物形式（BTC）に正規化する。
// 既に現物形式の場合はそのまま返す。「_JPY」付き（レバレッジ）と
// 未対応銘柄はエラーを返す。
func ToGMOSymbol(symbol string) (string, error) {
	s := strings.ToUpper(strings.TrimSpace(symbol))
	if s == "" {
		return "", fmt.Errorf("gmo: symbol is required")
	}
	if strings.Contains(s, "_") {
		return "", fmt.Errorf("gmo: leverage symbols not supported (use spot, e.g. BTC not BTC_JPY): %s", symbol)
	}
	if gmoSpotSymbols[s] {
		return s, nil
	}
	if mapped, ok := gmoSymbolMap[s]; ok {
		return mapped, nil
	}
	return "", fmt.Errorf("gmo: unsupported symbol: %s", symbol)
}

// GMOClient は GMO コイン Public API のクライアント。
type GMOClient struct {
	httpClient *http.Client
	baseURL    string
}

// NewGMOClient は環境変数から設定を読み込んで生成する。
// GMO_EXCHANGE_BASE_URL があればそちらを優先する（既定は本番 Public API）。
func NewGMOClient(timeout time.Duration) *GMOClient {
	baseURL := os.Getenv("GMO_EXCHANGE_BASE_URL")
	if baseURL == "" {
		baseURL = DefaultGMOBaseURL
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &GMOClient{
		httpClient: &http.Client{Timeout: timeout},
		baseURL:    strings.TrimRight(baseURL, "/"),
	}
}

var _ MarketDataProvider = (*GMOClient)(nil)

// gmoAPIResponse は GMO API 共通レスポンス形式（status: 0 が正常）。
type gmoAPIResponse struct {
	Status  int             `json:"status"`
	Data    json.RawMessage `json:"data"`
	Message string          `json:"responsetime"`
}

func (c *GMOClient) doGet(path string) (json.RawMessage, error) {
	resp, err := c.httpClient.Get(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("gmo http get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gmo unexpected status: %d", resp.StatusCode)
	}
	var raw gmoAPIResponse
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("gmo unmarshal envelope: %w", err)
	}
	if raw.Status != 0 {
		return nil, fmt.Errorf("gmo api error: status=%d", raw.Status)
	}
	return raw.Data, nil
}

// gmoTicker は /v1/ticker の data 配列要素。
type gmoTicker struct {
	Symbol  string `json:"symbol"`
	High    string `json:"high"`
	Low     string `json:"low"`
	Last    string `json:"last"`
	BestBid string `json:"bestBid"`
	BestAsk string `json:"bestAsk"`
	Volume  string `json:"volume"`
}

// FetchTicker は GMO ティッカーを取得する。
func (c *GMOClient) FetchTicker(symbol string) (*domain.Ticker, error) {
	sym, err := ToGMOSymbol(symbol)
	if err != nil {
		return nil, err
	}
	data, err := c.doGet("/v1/ticker?symbol=" + sym)
	if err != nil {
		return nil, fmt.Errorf("gmo fetch ticker: %w", err)
	}
	var tickers []gmoTicker
	if err := json.Unmarshal(data, &tickers); err != nil {
		return nil, fmt.Errorf("gmo unmarshal ticker: %w", err)
	}
	if len(tickers) == 0 {
		return nil, fmt.Errorf("gmo empty ticker for %s", sym)
	}
	t := tickers[0]
	out := &domain.Ticker{Symbol: sym}
	fmt.Sscanf(t.Last, "%f", &out.Last)
	fmt.Sscanf(t.BestBid, "%f", &out.Bid)
	fmt.Sscanf(t.BestAsk, "%f", &out.Ask)
	fmt.Sscanf(t.Volume, "%f", &out.Volume)
	fmt.Sscanf(t.High, "%f", &out.High)
	fmt.Sscanf(t.Low, "%f", &out.Low)
	return out, nil
}

// gmoLevel は板の1レベル（price/size は文字列）。
type gmoLevel struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

// gmoOrderBook は /v1/orderbooks の data。
type gmoOrderBook struct {
	Asks   []gmoLevel `json:"asks"`
	Bids   []gmoLevel `json:"bids"`
	Symbol string     `json:"symbol"`
}

// FetchOrderBook は GMO 板情報を取得する。limit は無視される
// （GMO 側が全板返却のため、呼び出し側で必要深度に切る）。
func (c *GMOClient) FetchOrderBook(symbol string, limit int) (*domain.OrderBook, error) {
	sym, err := ToGMOSymbol(symbol)
	if err != nil {
		return nil, err
	}
	data, err := c.doGet("/v1/orderbooks?symbol=" + sym)
	if err != nil {
		return nil, fmt.Errorf("gmo fetch orderbook: %w", err)
	}
	var raw gmoOrderBook
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("gmo unmarshal orderbook: %w", err)
	}
	ob := &domain.OrderBook{
		Symbol:    sym,
		Timestamp: time.Now().UnixMilli(),
	}
	for _, b := range raw.Bids {
		var price, volume float64
		fmt.Sscanf(b.Price, "%f", &price)
		fmt.Sscanf(b.Size, "%f", &volume)
		if price > 0 && volume > 0 {
			ob.Bids = append(ob.Bids, domain.Order{Price: price, Volume: volume})
		}
	}
	for _, a := range raw.Asks {
		var price, volume float64
		fmt.Sscanf(a.Price, "%f", &price)
		fmt.Sscanf(a.Size, "%f", &volume)
		if price > 0 && volume > 0 {
			ob.Asks = append(ob.Asks, domain.Order{Price: price, Volume: volume})
		}
	}
	if len(ob.Bids) > limit && limit > 0 {
		ob.Bids = ob.Bids[:limit]
	}
	if len(ob.Asks) > limit && limit > 0 {
		ob.Asks = append([]domain.Order(nil), ob.Asks[:limit]...)
	}
	if len(ob.Bids) > 0 && len(ob.Asks) > 0 {
		ob.MidPrice = (ob.Bids[0].Price + ob.Asks[0].Price) / 2.0
		ob.Spread = ob.Asks[0].Price - ob.Bids[0].Price
	}
	return ob, nil
}

// gmoWSOrderBook は Public WS orderbooks チャネルのメッセージ。
type gmoWSOrderBook struct {
	Channel   string     `json:"channel"`
	Symbol    string     `json:"symbol"`
	Asks      []gmoLevel `json:"asks"`
	Bids      []gmoLevel `json:"bids"`
	Timestamp string     `json:"timestamp"`
}

// GMOWSSubscribe は Public WS の購読リクエスト形式。
type GMOWSSubscribe struct {
	Command string `json:"command"`
	Channel string `json:"channel"`
	Symbol  string `json:"symbol"`
}

// NewGMOWSSubscribe は orderbooks 購読リクエストを生成する。
func NewGMOWSSubscribe(symbol string) (GMOWSSubscribe, error) {
	sym, err := ToGMOSymbol(symbol)
	if err != nil {
		return GMOWSSubscribe{}, err
	}
	return GMOWSSubscribe{Command: "subscribe", Channel: GMOOrderBookChannel, Symbol: sym}, nil
}

// ParseGMOWSMessage は WS メッセージを domain.OrderBook に変換する。
// timestamp は ISO8601 文字列。解釈失敗時は受信時刻で代替する。
func ParseGMOWSMessage(raw []byte, receivedAt int64) (*domain.OrderBook, error) {
	var msg gmoWSOrderBook
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, fmt.Errorf("gmo unmarshal ws: %w", err)
	}
	if msg.Channel != "" && msg.Channel != GMOOrderBookChannel {
		return nil, fmt.Errorf("gmo unexpected ws channel: %s", msg.Channel)
	}
	ts := receivedAt
	if msg.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339, msg.Timestamp); err == nil {
			ts = t.UnixMilli()
		} else if t, err := time.Parse("2006-01-02T15:04:05.000Z", msg.Timestamp); err == nil {
			ts = t.UnixMilli()
		}
	}
	ob := &domain.OrderBook{Symbol: msg.Symbol, Timestamp: ts}
	for _, b := range msg.Bids {
		var price, volume float64
		fmt.Sscanf(b.Price, "%f", &price)
		fmt.Sscanf(b.Size, "%f", &volume)
		if price > 0 && volume > 0 {
			ob.Bids = append(ob.Bids, domain.Order{Price: price, Volume: volume})
		}
	}
	for _, a := range msg.Asks {
		var price, volume float64
		fmt.Sscanf(a.Price, "%f", &price)
		fmt.Sscanf(a.Size, "%f", &volume)
		if price > 0 && volume > 0 {
			ob.Asks = append(ob.Asks, domain.Order{Price: price, Volume: volume})
		}
	}
	if len(ob.Bids) == 0 || len(ob.Asks) == 0 {
		return nil, fmt.Errorf("gmo empty book side")
	}
	ob.MidPrice = (ob.Bids[0].Price + ob.Asks[0].Price) / 2.0
	ob.Spread = ob.Asks[0].Price - ob.Bids[0].Price
	return ob, nil
}
