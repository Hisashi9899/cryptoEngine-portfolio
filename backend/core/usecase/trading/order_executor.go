package trading

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cryptoEngine/core/domain"
	"github.com/cryptoEngine/core/infrastructure/logging"
)

// 自動売買の成行注文執行（Binance spot API）。
//
// - MARKET BUY / MARKET SELL（/api/v3/order）
// - HMAC-SHA256 による署名（timestamp + query の sign）
// - API キー・シークレットは環境変数（BINANCE_API_KEY / BINANCE_API_SECRET）から取得
// - 注文失敗時は最大 MaxOrderRetries 回リトライ（4xx はリトライしない）
//
// ネットワーク境界を分離するため OrderExecutor インターフェースを定義し、
// テストでは httptest サーバーを差し込めるようにしている。
const (
	DefaultBinanceBaseURL = "https://api.binance.com"
	DefaultBinanceTimeout = 10 * time.Second
	MaxOrderRetries       = 3
	// Binance 側の受付上限（5 秒）。署名に含める。
	defaultRecvWindow = 5000
)

// OrderResult は成行注文の約定結果。
type OrderResult struct {
	Symbol    string           `json:"symbol"`
	Side      domain.TradeSide `json:"side"`
	Price     float64          `json:"price"`    // 平均約定価格（cummulativeQuoteQty / executedQty）
	Quantity  float64          `json:"quantity"` // 約定数量（base asset）
	OrderID   string           `json:"order_id"`
	Status    string           `json:"status"` // filled / error
	Timestamp int64            `json:"timestamp"`
}

// OrderExecutor は成行注文の実行を抽象化するインターフェース。
// 実装は BinanceOrderExecutor（本ファイル）。テストではモックを注入できる。
type OrderExecutor interface {
	MarketOrder(ctx context.Context, symbol string, side domain.TradeSide, quantity float64) (*OrderResult, error)
}

// BinanceOrderExecutor は Binance spot への成行注文クライアント。
type BinanceOrderExecutor struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string
	logger     *logging.Logger
}

// NewBinanceOrderExecutor は環境変数から設定を読み込んで生成する。
// キー未設定でも生成は成功し、MarketOrder 実行時にエラーを返す（安全側）。
func NewBinanceOrderExecutor(logger *logging.Logger) *BinanceOrderExecutor {
	baseURL := os.Getenv("BINANCE_BASE_URL")
	if baseURL == "" {
		baseURL = DefaultBinanceBaseURL
	}
	timeout := DefaultBinanceTimeout
	if raw := os.Getenv("BINANCE_TIMEOUT_SEC"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			timeout = time.Duration(v) * time.Second
		}
	}
	return &BinanceOrderExecutor{
		httpClient: &http.Client{Timeout: timeout},
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     os.Getenv("BINANCE_API_KEY"),
		apiSecret:  os.Getenv("BINANCE_API_SECRET"),
		logger:     logger,
	}
}

// binanceOrderResponse は /api/v3/order のレスポンス（必要な項目のみ）。
type binanceOrderResponse struct {
	Symbol              string `json:"symbol"`
	OrderID             int64  `json:"orderId"`
	Status              string `json:"status"`
	ExecutedQty         string `json:"executedQty"`
	CummulativeQuoteQty string `json:"cummulativeQuoteQty"`
	TransactTime        int64  `json:"transactTime"`
	Code                int    `json:"code"`
	Msg                 string `json:"msg"`
}

func (r *binanceOrderResponse) executedQty() float64 {
	var v float64
	fmt.Sscanf(r.ExecutedQty, "%f", &v)
	return v
}

func (r *binanceOrderResponse) quoteQty() float64 {
	var v float64
	fmt.Sscanf(r.CummulativeQuoteQty, "%f", &v)
	return v
}

// MarketOrder は成行注文を実行する。5xx・ネットワークエラー時は
// 最大 MaxOrderRetries 回リトライする（4xx は即時失敗）。
func (e *BinanceOrderExecutor) MarketOrder(ctx context.Context, symbol string, side domain.TradeSide, quantity float64) (*OrderResult, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}
	if quantity <= 0 {
		return nil, fmt.Errorf("quantity must be positive: %.8f", quantity)
	}
	if e.apiKey == "" || e.apiSecret == "" {
		return nil, fmt.Errorf("binance api key/secret not configured (BINANCE_API_KEY / BINANCE_API_SECRET)")
	}

	sideStr := strings.ToUpper(string(side))
	if sideStr != "BUY" && sideStr != "SELL" {
		return nil, fmt.Errorf("invalid side: %s", side)
	}

	var lastErr error
	for attempt := 1; attempt <= MaxOrderRetries; attempt++ {
		result, retryable, err := e.executeOnce(ctx, symbol, sideStr, quantity)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !retryable || attempt == MaxOrderRetries {
			break
		}
		e.logger.Warn("market order failed (attempt %d/%d): %v; retrying in %ds",
			attempt, MaxOrderRetries, err, attempt)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt) * time.Second):
		}
	}
	return nil, fmt.Errorf("market order %s %s qty=%.8f: %w", sideStr, symbol, quantity, lastErr)
}

func (e *BinanceOrderExecutor) executeOnce(ctx context.Context, symbol, side string, quantity float64) (*OrderResult, bool, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("side", side)
	params.Set("type", "MARKET")
	params.Set("quantity", strconv.FormatFloat(quantity, 'f', 8, 64))
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	params.Set("recvWindow", strconv.Itoa(defaultRecvWindow))

	query := params.Encode()
	signature := signHMACSHA256(query, e.apiSecret)
	body := query + "&signature=" + signature

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/api/v3/order", strings.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-MBX-APIKEY", e.apiKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, true, err // ネットワークエラーはリトライ可能
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}

	if resp.StatusCode >= 500 {
		// Binance 側の一時障害はリトライ可能。
		return nil, true, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}
	if resp.StatusCode != http.StatusOK {
		// 4xx（認証・資金不足・数量エラー等）はリトライしない。
		return nil, false, fmt.Errorf("order rejected (status %d): %s", resp.StatusCode, truncate(string(respBody), 200))
	}

	var raw binanceOrderResponse
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, false, fmt.Errorf("unmarshal order response: %w", err)
	}
	if raw.Code != 0 {
		return nil, false, fmt.Errorf("order rejected: code=%d msg=%s", raw.Code, raw.Msg)
	}

	qty := raw.executedQty()
	price := 0.0
	if quote := raw.quoteQty(); quote > 0 && qty > 0 {
		price = quote / qty
	}
	status := "filled"
	if raw.Status != "FILLED" {
		status = strings.ToLower(raw.Status)
	}

	result := &OrderResult{
		Symbol:    raw.Symbol,
		Side:      domain.TradeSide(strings.ToLower(side)),
		Price:     price,
		Quantity:  qty,
		OrderID:   strconv.FormatInt(raw.OrderID, 10),
		Status:    status,
		Timestamp: raw.TransactTime,
	}
	if result.Timestamp == 0 {
		result.Timestamp = time.Now().UnixMilli()
	}
	e.logger.Info("market order filled: symbol=%s side=%s qty=%.8f price=%.2f orderId=%s",
		result.Symbol, result.Side, result.Quantity, result.Price, result.OrderID)
	return result, false, nil
}

// signHMACSHA256 は message を apiSecret で HMAC-SHA256 署名し hex 文字列を返す。
// Binance API の署名形式（/api/v3/order など）に使う。
func signHMACSHA256(message, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
