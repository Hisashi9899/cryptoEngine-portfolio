package exchange

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cryptoEngine/core/domain"
)

// tape.go は約定テープ（executions / aggTrades）の取得を提供する。
//
// 板スナップショットが「置かれている注文」なのに対し、テープは
// 「誰が食ったか」（テイカー売買）で、CVD・吸収検知の素になる。
// どちらも公開 REST（キー不要）で、MarketDataProvider とは別口の
// TradeTapeFetcher として定義する（既存実装・モックを壊さない）。

// TradeTapeFetcher は約定テープ取得の抽象。
type TradeTapeFetcher interface {
	FetchExecutions(symbol string, limit int) ([]domain.Execution, error)
}

var (
	_ TradeTapeFetcher = (*GMOClient)(nil)
	_ TradeTapeFetcher = (*Client)(nil)
)

// clampTapeLimit は取得件数を 1〜1000 に丸める。
func clampTapeLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}

// gmoExecution は /public/v1/trades の data.list 要素。
// 注意：同一マッチが BUY/SELL 両建てで2行出る（テイカー不明）のため、
// 方向は値段のティックルールで付ける（下記）。side フィールドは使わない。
// timestamp は ISO8601。
type gmoExecution struct {
	Price     string `json:"price"`
	Size      string `json:"size"`
	Side      string `json:"side"`
	Timestamp string `json:"timestamp"`
}

type gmoExecutionsData struct {
	List []gmoExecution `json:"list"`
}

// parseGMOTime は GMO 時刻文字列を UnixMilli に変換する。
func parseGMOTime(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	for _, layout := range []string{
		time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}

// FetchExecutions は GMO 公開約定履歴を取得する（キー不要）。
// /v1/executions ではなく /public/v1/trades を使う（前者は Private 用）。
// 方向はティックルールで付与する：直前値より上なら買い食い、下なら売り食い、
// 同値なら直前を継承する（Lee-Ready の簡易版）。初行のみ side を種にする。
func (c *GMOClient) FetchExecutions(symbol string, limit int) ([]domain.Execution, error) {
	sym, err := ToGMOSymbol(symbol)
	if err != nil {
		return nil, err
	}
	data, err := c.doGet("/v1/trades?symbol=" + sym)
	if err != nil {
		return nil, fmt.Errorf("gmo fetch executions: %w", err)
	}
	var raw gmoExecutionsData
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("gmo unmarshal executions: %w", err)
	}
	limit = clampTapeLimit(limit)
	out := make([]domain.Execution, 0, len(raw.List))
	prevPrice := 0.0
	prevBuy := true
	for _, e := range raw.List {
		if len(out) >= limit {
			break
		}
		var price, qty float64
		fmt.Sscanf(e.Price, "%f", &price)
		fmt.Sscanf(e.Size, "%f", &qty)
		if price <= 0 || qty <= 0 {
			continue
		}
		buy := prevBuy
		if prevPrice > 0 {
			if price > prevPrice {
				buy = true
			} else if price < prevPrice {
				buy = false
			}
		}
		prevPrice, prevBuy = price, buy
		side := domain.TradeBuy
		if !buy {
			side = domain.TradeSell
		}
		out = append(out, domain.Execution{
			Symbol: sym, Side: side, Price: price, Qty: qty,
			Timestamp: parseGMOTime(e.Timestamp),
		})
	}
	return out, nil
}

// binanceAggTrade は /api/v3/aggTrades の1件。
// m (isBuyerMaker) が true なら売りテイカーが食った＝売り約定。
type binanceAggTrade struct {
	Price        string `json:"p"`
	Qty          string `json:"q"`
	Timestamp    int64  `json:"T"`
	IsBuyerMaker bool   `json:"m"`
}

// FetchExecutions は Binance 公開 aggTrades を取得する（キー不要）。
// シンボルは Binance 形式（BTCUSDT）。GMO 短縮形も ToGMOSymbol 逆引きなしに
// そのまま通す（Binance 側の形式を要求するため変換しない）。
func (c *Client) FetchExecutions(symbol string, limit int) ([]domain.Execution, error) {
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	if sym == "" {
		return nil, fmt.Errorf("binance fetch executions: symbol is required")
	}
	limit = clampTapeLimit(limit)
	url := fmt.Sprintf("%s/api/v3/aggTrades?symbol=%s&limit=%d", c.baseURL, sym, limit)
	body, err := c.doGet(url)
	if err != nil {
		return nil, fmt.Errorf("binance fetch executions: %w", err)
	}
	var raw []binanceAggTrade
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("binance unmarshal executions: %w", err)
	}
	out := make([]domain.Execution, 0, len(raw))
	for _, t := range raw {
		var price, qty float64
		fmt.Sscanf(t.Price, "%f", &price)
		fmt.Sscanf(t.Qty, "%f", &qty)
		if price <= 0 || qty <= 0 || t.Timestamp <= 0 {
			continue
		}
		side := domain.TradeBuy
		if t.IsBuyerMaker {
			side = domain.TradeSell
		}
		out = append(out, domain.Execution{
			Symbol: sym, Side: side, Price: price, Qty: qty,
			Timestamp: t.Timestamp,
		})
	}
	return out, nil
}
