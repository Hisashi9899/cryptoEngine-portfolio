package trading

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// gmo_account.go は GMO 証拠金・建玉の読み取り専用照会を提供する。
//
// 発注は一切行わない。レバレッジ口座の疎通確認（キー権限・証拠金・建玉）を
// 目的とし、以下を返す:
//   - GET /v1/account/margin（証拠金余力・維持率・ロスカット状態）
//   - GET /v1/openPositions?symbol=XXX_JPY（建玉一覧。対象：証拠金取引）
//
// 署名規則は既存 executor と同一（timestamp+METHOD+path、クエリなし）。

// GMOMarginStatus は GET /v1/account/margin の data 部。
type GMOMarginStatus struct {
	ActualProfitLoss string `json:"actualProfitLoss"`
	AvailableAmount  string `json:"availableAmount"`
	Margin           string `json:"margin"`
	MarginCallStatus string `json:"marginCallStatus"`
	MarginRatio      string `json:"marginRatio"`
	ProfitLoss       string `json:"profitLoss"`
	Transferable     string `json:"transferableAmount"`
}

// GMOOpenPosition は GET /v1/openPositions の建玉1件。
type GMOOpenPosition struct {
	PositionID  int64  `json:"positionId"`
	Symbol      string `json:"symbol"`
	Side        string `json:"side"`
	Size        string `json:"size"`
	OrderedSize string `json:"orderdSize"`
	Price       string `json:"price"`
	LossGain    string `json:"lossGain"`
	Leverage    string `json:"leverage"`
	Losscut     string `json:"losscutPrice"`
	Timestamp   string `json:"timestamp"`
}

// gmoGet は認証付き GET の共通処理。業務 status!=0 は即時失敗を返す。
func (e *GMOOrderExecutor) gmoGet(ctx context.Context, path, query string, out any) error {
	if e.apiKey == "" || e.apiSecret == "" {
		return fmt.Errorf("gmo api key/secret not configured (GMO_API_KEY / GMO_API_SECRET)")
	}
	timestamp := strconv.FormatInt(e.now().UnixMilli(), 10)
	signature := signGMO(timestamp+"GET"+path, e.apiSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.baseURL+path+query, nil)
	if err != nil {
		return err
	}
	req.Header.Set("API-KEY", e.apiKey)
	req.Header.Set("API-TIMESTAMP", timestamp)
	req.Header.Set("API-SIGN", signature)
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gmo get %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("gmo get %s read: %w", path, err)
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("gmo unexpected status %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var env struct {
		Status int             `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("gmo unmarshal %s response: %w", path, err)
	}
	// GMO は業務エラーでも HTTP 200 を返すため status フィールドで判定する。
	if env.Status != 0 {
		var gerr gmoErrorResponse
		detail := ""
		if json.Unmarshal(body, &gerr) == nil && len(gerr.Messages) > 0 {
			detail = gerr.Messages[0].MessageString
		}
		return fmt.Errorf("gmo %s rejected: status=%d %s", path, env.Status, detail)
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("gmo unmarshal %s data: %w", path, err)
	}
	return nil
}

// FetchMargin は証拠金状況を取得する（読み取り専用）。
func (e *GMOOrderExecutor) FetchMargin(ctx context.Context) (*GMOMarginStatus, error) {
	var st GMOMarginStatus
	if err := e.gmoGet(ctx, "/v1/account/margin", "", &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// FetchOpenPositions は指定レバレッジ銘柄の建玉一覧を取得する（読み取り専用）。
// symbol は証拠金形式（例 BTC_JPY）。空は BTC_JPY。
func (e *GMOOrderExecutor) FetchOpenPositions(ctx context.Context, symbol string) ([]GMOOpenPosition, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		symbol = "BTC_JPY"
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var raw struct {
		List []GMOOpenPosition `json:"list"`
	}
	// 署名対象のパスはクエリなし（activeOrders で実機確認した規則に合わせる）。
	if err := e.gmoGet(ctx, "/v1/openPositions", "?symbol="+symbol, &raw); err != nil {
		return nil, err
	}
	if raw.List == nil {
		return []GMOOpenPosition{}, nil
	}
	return raw.List, nil
}
