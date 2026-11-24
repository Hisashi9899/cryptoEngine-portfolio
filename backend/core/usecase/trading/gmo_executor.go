package trading

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cryptoEngine/core/domain"
	"github.com/cryptoEngine/core/infrastructure/exchange"
	"github.com/cryptoEngine/core/infrastructure/logging"
)

// GMO コイン現物への成行注文執行（Private API）。
//
//   - POST /v1/order（symbol / side / executionType=MARKET / size）。
//     symbol は現物の短縮形（BTC）。「_JPY」はレバレッジ指定になるため拒否する
//     （2026-09-15 サポート回答で確定）。
//   - 認証: API-KEY / API-TIMESTAMP / API-SIGN ヘッダ。
//     署名対象は timestamp + method + path + body の HMAC-SHA256。
//   - API キー・シークレットは環境変数（GMO_API_KEY / GMO_API_SECRET）から取得。
//     後方互換として BINANCE_API_KEY / BINANCE_API_SECRET も参照する。
//   - 最小数量ガード: 銘柄別の最小数量・刻み（gmoSizeRules）を適用。
//     未満・未登録銘柄は発注せず即時失敗する。
//   - 約定価格は executions 照会で取得し、不可時はシグナル価格で代替する。
//   - 定期メンテ（毎週土曜 09:00〜11:00 JST）は発注を拒否する。
//   - 注文失敗時は最大 MaxOrderRetries 回リトライ（業務エラーは即時失敗）。
//
// 既存の OrderExecutor インターフェースを実装するため、呼び出し側
// （AutoTradeUseCase）の変更は不要。main.go の生成箇所だけ差し替える。
const (
	DefaultGMOPrivateBaseURL = "https://api.coin.z.com/private"
	DefaultGMOTimeout        = 10 * time.Second

	// GMO 現物の銘柄別最小数量・数量刻み。
	// 取得元: GET /public/v1/symbols（2026-09-15 実測・短縮形＝現物）。
	// BTC は最小 0.00001（約 120 円〜）のため 5,000 円口座でも取引可。
	// 未登録シンボルは fail-closed（発注拒否）とする。

	// GMO 定期メンテナンス（JST）。この時間帯の発注は拒否する。
	// GMO_MAINTENANCE_CHECK=off で無効化できる（テスト用）。
	GMOMaintenanceWeekday = time.Saturday
	GMOMaintenanceStartH  = 9
	GMOMaintenanceEndH    = 11
)

// GMOOrderExecutor は GMO コイン現物への成行注文クライアント。
type GMOOrderExecutor struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string
	logger     *logging.Logger
	now        func() time.Time
	// mdClient は資金ガード用の現在値取得（Public API）。nil の場合は
	// NewGMOOrderExecutor が生成する。テストでは差し替え可能。
	mdClient gmoTickerFetcher
	// marginRules は証拠金シンボル別の数量規則（LoadMarginRules で取得）。
	marginRules map[string]gmoSizeRule
}

var _ OrderExecutor = (*GMOOrderExecutor)(nil)

// NewGMOOrderExecutor は環境変数から設定を読み込んで生成する。
// キー未設定でも生成は成功し、MarketOrder 実行時にエラーを返す（安全側）。
func NewGMOOrderExecutor(logger *logging.Logger) *GMOOrderExecutor {
	baseURL := firstEnv("GMO_API_BASE_URL", "GMO_EXCHANGE_BASE_URL")
	if baseURL == "" {
		baseURL = DefaultGMOPrivateBaseURL
	}
	timeout := DefaultGMOTimeout
	if raw := firstEnv("GMO_TIMEOUT_SEC", "BINANCE_TIMEOUT_SEC"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			timeout = time.Duration(v) * time.Second
		}
	}
	return &GMOOrderExecutor{
		httpClient: &http.Client{Timeout: timeout},
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     firstEnv("GMO_API_KEY", "BINANCE_API_KEY"),
		apiSecret:  firstEnv("GMO_API_SECRET", "BINANCE_API_SECRET"),
		logger:     logger,
		now:        time.Now,
		mdClient:   exchange.NewGMOClient(timeout),
	}
}

// firstEnv は最初に設定されている環境変数の値を返す。どれも未設定なら ""。
func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// gmoOrderRequest は POST /v1/order のリクエストボディ。
// 指値時は Price・TimeInForce を付与する（成行時は省略）。
type gmoOrderRequest struct {
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	ExecutionType string `json:"executionType"`
	Price         string `json:"price,omitempty"`
	TimeInForce   string `json:"timeInForce,omitempty"`
	Size          string `json:"size"`
}

// LimitOrderExecutor は指値注文を出せる executor の拡張。GMO が実装する。
// FAK（Fill And Kill）は即時執行の指値で、スリッページ上限付きの実質成行として使う。
type LimitOrderExecutor interface {
	LimitOrder(ctx context.Context, symbol string, side domain.TradeSide, quantity, price float64, timeInForce string) (*OrderResult, error)
}

var _ LimitOrderExecutor = (*GMOOrderExecutor)(nil)

// gmoOrderResponse は POST /v1/order のレスポンス（data は注文ID文字列）。
type gmoOrderResponse struct {
	Status int    `json:"status"`
	Data   string `json:"data"`
}

// gmoExecution は GET /v1/executions の約定1件。
type gmoExecution struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

// gmoExecutionsResponse は約定照会レスポンス。
type gmoExecutionsResponse struct {
	Status int `json:"status"`
	Data   struct {
		List []gmoExecution `json:"list"`
	} `json:"data"`
}

// gmoErrorResponse は業務エラーレスポンス。
type gmoErrorResponse struct {
	Status   int `json:"status"`
	Messages []struct {
		MessageCode   string `json:"message_code"`
		MessageString string `json:"message_string"`
	} `json:"messages"`
}

// signGMO は text の HMAC-SHA256 署名を hex で返す。
func signGMO(text, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(text))
	return hex.EncodeToString(mac.Sum(nil))
}

// inMaintenanceWindow は GMO 定期メンテ時間帯かどうかを返す（JST 判定）。
func inMaintenanceWindow(now time.Time) bool {
	jst := now.In(time.FixedZone("JST", 9*60*60))
	if jst.Weekday() != GMOMaintenanceWeekday {
		return false
	}
	h := jst.Hour()
	return h >= GMOMaintenanceStartH && h < GMOMaintenanceEndH
}

// gmoSizeRule は銘柄別の最小数量・数量刻み。
type gmoSizeRule struct {
	Min  float64
	Step float64
}

// gmoSizeRules は現物の /public/v1/symbols 実測値（2026-09-15、FCRは2026-09-16追加）。
// 現物17銘柄の全件監視に対応する（traderは全銘柄から最適1件を選定する）。
var gmoSizeRules = map[string]gmoSizeRule{
	"BTC":  {Min: 0.00001, Step: 0.00001},
	"ETH":  {Min: 0.001, Step: 0.0001},
	"BCH":  {Min: 0.01, Step: 0.001},
	"LTC":  {Min: 0.1, Step: 0.01},
	"XRP":  {Min: 1, Step: 1},
	"XLM":  {Min: 1, Step: 1},
	"DOT":  {Min: 0.1, Step: 0.1},
	"ATOM": {Min: 0.01, Step: 0.01},
	"FCR":  {Min: 1, Step: 1},
	"ADA":  {Min: 1, Step: 1},
	"LINK": {Min: 0.1, Step: 0.1},
	"DOGE": {Min: 10, Step: 1},
	"SOL":  {Min: 0.01, Step: 0.01},
	"ASTR": {Min: 10, Step: 1},
	"NAC":  {Min: 0.1, Step: 0.1},
	"WILD": {Min: 1, Step: 1},
	"SUI":  {Min: 0.1, Step: 0.1},
}

// GMOSpotUniverse は現物全17銘柄（アルファベット順）。traderの既定監視対象。
// _JPY付きレバレッジは対象外（現物ボットでは使用禁止）。
var GMOSpotUniverse = []string{
	"ADA", "ASTR", "ATOM", "BCH", "BTC", "DOGE", "DOT", "ETH",
	"FCR", "LINK", "LTC", "NAC", "SOL", "SUI", "WILD", "XLM", "XRP",
}

// roundDownSize は数量を銘柄の刻みで切り捨てる（資金超過注文の防止）。
// 刻みが 1 以上の場合は整数化される（DOGE の 1 単位等）。
func roundDownSize(qty, step float64) float64 {
	if step <= 0 {
		step = 1
	}
	return math.Floor(qty/step) * step
}

// formatSize は数量を刻みに見合う小数桁で正規化する。
// float 誤差由来の不定形（0.20000000000000004 等）を送らず、
// GMO の decimal 検証に確実に通る形にする。刻み不明時は従来通り。
func formatSize(qty, step float64) string {
	if step <= 0 {
		return strconv.FormatFloat(qty, 'f', -1, 64)
	}
	dec := 0
	s := strconv.FormatFloat(step, 'f', -1, 64)
	if i := strings.IndexByte(s, '.'); i >= 0 {
		dec = len(s) - i - 1
	}
	return strconv.FormatFloat(qty, 'f', dec, 64)
}

// sizeStepFor は執行シンボルの数量刻みを返す（不明時は 0）。
func (e *GMOOrderExecutor) sizeStepFor(gmoSymbol string) float64 {
	if r, ok := e.marginRules[gmoSymbol]; ok && r.Step > 0 {
		return r.Step
	}
	if r, ok := gmoSizeRules[gmoSymbol]; ok && r.Step > 0 {
		return r.Step
	}
	return 0
}

// MarketOrder は成行注文を実行する。GMO 形式の検証・署名・約定価格取得まで行う。
// 数量が最小制約未満、メンテ時間帯、キー未設定の場合は発注せず即時失敗する。
func (e *GMOOrderExecutor) MarketOrder(ctx context.Context, symbol string, side domain.TradeSide, quantity float64) (*OrderResult, error) {
	gmoSymbol, sideStr, qty, origSymbol, err := e.prepare(symbol, side, quantity)
	if err != nil {
		return nil, err
	}
	if err := e.checkFunds(ctx, sideStr, gmoSymbol, qty); err != nil {
		return nil, err
	}
	return e.orderLoop(ctx, gmoSymbol, sideStr, "MARKET", "", "", qty, origSymbol)
}

// LimitOrder は指値注文を実行する。timeInForce は FAK/FAS/FOK（既定 FAK）。
// FAK は即時執行のためスリッページ上限付きの実質成行として使える。
func (e *GMOOrderExecutor) LimitOrder(ctx context.Context, symbol string, side domain.TradeSide, quantity, price float64, timeInForce string) (*OrderResult, error) {
	if price <= 0 {
		return nil, fmt.Errorf("limit price must be positive: %v", price)
	}
	tif := strings.ToUpper(strings.TrimSpace(timeInForce))
	if tif == "" {
		tif = "FAK"
	}
	switch tif {
	case "FAK", "FAS", "FOK":
	default:
		return nil, fmt.Errorf("invalid timeInForce: %s (want FAK/FAS/FOK)", timeInForce)
	}
	gmoSymbol, sideStr, qty, origSymbol, err := e.prepare(symbol, side, quantity)
	if err != nil {
		return nil, err
	}
	if err := e.checkFunds(ctx, sideStr, gmoSymbol, qty); err != nil {
		return nil, err
	}
	priceStr := strconv.FormatFloat(price, 'f', -1, 64)
	return e.orderLoop(ctx, gmoSymbol, sideStr, "LIMIT", priceStr, tif, qty, origSymbol)
}

// prepare は Market/Limit 共通の事前検証（キー・メンテ・side・形式・最小数量）を行い、
// 執行に必要な正規化済み値を返す。発注は行わない。
func (e *GMOOrderExecutor) prepare(symbol string, side domain.TradeSide, quantity float64) (gmoSymbol, sideStr string, qty float64, origSymbol string, err error) {
	if symbol == "" {
		return "", "", 0, "", fmt.Errorf("symbol is required")
	}
	if quantity <= 0 {
		return "", "", 0, "", fmt.Errorf("quantity must be positive: %.8f", quantity)
	}
	if e.apiKey == "" || e.apiSecret == "" {
		return "", "", 0, "", fmt.Errorf("gmo api key/secret not configured (GMO_API_KEY / GMO_API_SECRET)")
	}
	if os.Getenv("GMO_MAINTENANCE_CHECK") != "off" && inMaintenanceWindow(e.now()) {
		return "", "", 0, "", fmt.Errorf("gmo maintenance window (Sat 09:00-11:00 JST): order skipped")
	}

	sideStr = strings.ToUpper(string(side))
	if sideStr != "BUY" && sideStr != "SELL" {
		return "", "", 0, "", fmt.Errorf("invalid side: %s", side)
	}

	// GMO 現物形式（短縮形・例 BTC）のみ受け付ける。「_JPY」付きは
	// レバレッジ指定になるため拒否する（2026-09-15 サポート回答）。
	// Binance 形式（BTCUSDT 等）も配線ミス防止のため拒否する。誤発注防止の安全弁。
	if strings.Contains(symbol, "_") {
		return "", "", 0, "", fmt.Errorf("gmo executor requires spot symbol (e.g. BTC, not BTC_JPY), got %q", symbol)
	}
	// GMO 現物体系へ変換（BTC はそのまま通る）。
	gmoSymbol, err = exchange.ToGMOSymbol(symbol)
	if err != nil {
		return "", "", 0, "", err
	}

	// 最小数量ガード（銘柄の刻みで切り捨て後に判定。未登録銘柄は拒否）。
	rule, ok := gmoSizeRules[gmoSymbol]
	if !ok {
		return "", "", 0, "", fmt.Errorf("gmo unsupported symbol for order (no size rule): %s", gmoSymbol)
	}
	qty = roundDownSize(quantity, rule.Step)
	if qty < rule.Min {
		return "", "", 0, "", fmt.Errorf("gmo quantity %v below minimum %v (%s)", qty, rule.Min, gmoSymbol)
	}
	return gmoSymbol, sideStr, qty, symbol, nil
}

// orderLoop は発注＋リトライ（ネットワーク系のみ）の共通処理。
func (e *GMOOrderExecutor) orderLoop(ctx context.Context, gmoSymbol, sideStr, execType, price, tif string, qty float64, origSymbol string) (*OrderResult, error) {
	var lastErr error
	for attempt := 1; attempt <= MaxOrderRetries; attempt++ {
		result, retryable, err := e.executeOnce(ctx, gmoSymbol, sideStr, execType, price, tif, qty, origSymbol)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !retryable || attempt == MaxOrderRetries {
			break
		}
		e.logger.Warn("gmo %s order failed (attempt %d/%d): %v; retrying in %ds",
			execType, attempt, MaxOrderRetries, err, attempt)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt) * time.Second):
		}
	}
	return nil, fmt.Errorf("gmo %s order %s %s qty=%.8f: %w", execType, sideStr, gmoSymbol, qty, lastErr)
}

func (e *GMOOrderExecutor) executeOnce(ctx context.Context, gmoSymbol, side, execType, limitPrice, timeInForce string, quantity float64, origSymbol string) (*OrderResult, bool, error) {
	timestamp := strconv.FormatInt(e.now().UnixMilli(), 10)
	body, err := json.Marshal(gmoOrderRequest{
		Symbol:        gmoSymbol,
		Side:          side,
		ExecutionType: execType,
		Price:         limitPrice,
		TimeInForce:   timeInForce,
		Size:          formatSize(quantity, e.sizeStepFor(gmoSymbol)),
	})
	if err != nil {
		return nil, false, err
	}
	path := "/v1/order"
	signature := signGMO(timestamp+"POST"+path+string(body), e.apiSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("API-KEY", e.apiKey)
	req.Header.Set("API-TIMESTAMP", timestamp)
	req.Header.Set("API-SIGN", signature)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, true, err // ネットワークエラーはリトライ可能
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}

	// GMO は業務エラーでも HTTP 200 を返すため status フィールドで判定する。
	if resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("gmo unexpected status %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}
	var raw gmoOrderResponse
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, false, fmt.Errorf("gmo unmarshal order response: %w", err)
	}
	if raw.Status != 0 {
		var gerr gmoErrorResponse
		detail := ""
		if json.Unmarshal(respBody, &gerr) == nil && len(gerr.Messages) > 0 {
			detail = gerr.Messages[0].MessageString
		}
		return nil, false, fmt.Errorf("gmo order rejected: status=%d %s (sent %s)", raw.Status, detail, truncate(string(body), 200))
	}
	if raw.Data == "" {
		return nil, false, fmt.Errorf("gmo empty order id in response")
	}

	// 平均約定価格は executions 照会で取得する。取れない場合はLT P相当の扱いとし、
	// 呼び出し側で分かるよう価格 0・status=filled で返す（後段で現在値補完を推奨）。
	price := e.fetchFillPrice(ctx, gmoSymbol, raw.Data)

	result := &OrderResult{
		Symbol:    origSymbol,
		Side:      domain.TradeSide(strings.ToLower(side)),
		Price:     price,
		Quantity:  quantity,
		OrderID:   raw.Data,
		Status:    "filled",
		Timestamp: e.now().UnixMilli(),
	}
	e.logger.Info("gmo %s order filled: symbol=%s side=%s qty=%.8f price=%.2f orderId=%s",
		execType, result.Symbol, result.Side, result.Quantity, result.Price, result.OrderID)
	return result, false, nil
}

// CancelOrder は指値待機注文を取り消す（Maker執行のタイムアウト・反対合図用）。
// 存在しない・約定済みの注文は GMO がエラーを返すため、その旨をエラーで返す。
// 呼び出し側は約定の有無を executions 照会で確定させること。
func (e *GMOOrderExecutor) CancelOrder(ctx context.Context, orderID string) error {
	if orderID == "" {
		return fmt.Errorf("order id is required")
	}
	if e.apiKey == "" || e.apiSecret == "" {
		return fmt.Errorf("gmo api key/secret not configured (GMO_API_KEY / GMO_API_SECRET)")
	}
	timestamp := strconv.FormatInt(e.now().UnixMilli(), 10)
	body, err := json.Marshal(map[string]string{"orderId": orderID})
	if err != nil {
		return err
	}
	path := "/v1/cancelOrder"
	signature := signGMO(timestamp+"POST"+path+string(body), e.apiSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("API-KEY", e.apiKey)
	req.Header.Set("API-TIMESTAMP", timestamp)
	req.Header.Set("API-SIGN", signature)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gmo cancelOrder request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("gmo cancelOrder read: %w", err)
	}
	var raw struct {
		Status int `json:"status"`
	}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return fmt.Errorf("gmo unmarshal cancelOrder: %w", err)
	}
	if raw.Status != 0 {
		var gerr gmoErrorResponse
		detail := ""
		if json.Unmarshal(respBody, &gerr) == nil && len(gerr.Messages) > 0 {
			detail = gerr.Messages[0].MessageString
		}
		return fmt.Errorf("gmo cancelOrder rejected: status=%d %s", raw.Status, detail)
	}
	e.logger.Info("gmo cancelOrder ok: orderId=%s", orderID)
	return nil
}

// GMOAsset は GET /v1/account/assets の1通貨分（残高確認用）。
type GMOAsset struct {
	Symbol    string `json:"symbol"`
	Balance   string `json:"balance"`
	Available string `json:"available"`
}

// FetchAssets は認証付きで残高一覧を取得する。発注しない読み取り専用のため、
// API キーの疎通確認（gmo-assets）に使う。キー未設定・署名失敗時はエラーを返す。
func (e *GMOOrderExecutor) FetchAssets(ctx context.Context) ([]GMOAsset, error) {
	if e.apiKey == "" || e.apiSecret == "" {
		return nil, fmt.Errorf("gmo api key/secret not configured (GMO_API_KEY / GMO_API_SECRET)")
	}
	timestamp := strconv.FormatInt(e.now().UnixMilli(), 10)
	path := "/v1/account/assets"
	signature := signGMO(timestamp+"GET"+path, e.apiSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("API-KEY", e.apiKey)
	req.Header.Set("API-TIMESTAMP", timestamp)
	req.Header.Set("API-SIGN", signature)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gmo assets request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gmo assets read: %w", err)
	}
	var raw struct {
		Status int        `json:"status"`
		Data   []GMOAsset `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("gmo unmarshal assets: %w", err)
	}
	if raw.Status != 0 {
		var gerr gmoErrorResponse
		detail := ""
		if json.Unmarshal(body, &gerr) == nil && len(gerr.Messages) > 0 {
			detail = gerr.Messages[0].MessageString
		}
		return nil, fmt.Errorf("gmo assets rejected: status=%d %s", raw.Status, detail)
	}
	return raw.Data, nil
}

// GMOActiveOrder は GET /v1/activeOrders の1件（有効注文の照会用）。
type GMOActiveOrder struct {
	OrderID       string `json:"orderId"`
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	ExecutionType string `json:"executionType"`
	Price         string `json:"price"`
	Size          string `json:"size"`
	Status        string `json:"status"`
}

// FetchActiveOrders は有効注文一覧を取得する（読み取り専用）。
// トレード系権限（有効注文一覧）がキーに効いているかの判定に使う。
// 発注は一切行わない。
func (e *GMOOrderExecutor) FetchActiveOrders(ctx context.Context, symbol string) ([]GMOActiveOrder, error) {
	if e.apiKey == "" || e.apiSecret == "" {
		return nil, fmt.Errorf("gmo api key/secret not configured (GMO_API_KEY / GMO_API_SECRET)")
	}
	gmoSymbol, err := exchange.ToGMOSymbol(symbol)
	if err != nil {
		return nil, err
	}
	timestamp := strconv.FormatInt(e.now().UnixMilli(), 10)
	// 署名対象のパスはクエリなし（GET のクエリ付き署名は invalid になることを実機確認）。
	query := "?symbol=" + gmoSymbol
	path := "/v1/activeOrders"
	signature := signGMO(timestamp+"GET"+path, e.apiSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.baseURL+path+query, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("API-KEY", e.apiKey)
	req.Header.Set("API-TIMESTAMP", timestamp)
	req.Header.Set("API-SIGN", signature)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gmo activeOrders request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gmo activeOrders read: %w", err)
	}
	var raw struct {
		Status int `json:"status"`
		Data   struct {
			List []GMOActiveOrder `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("gmo unmarshal activeOrders: %w", err)
	}
	if raw.Status != 0 {
		var gerr gmoErrorResponse
		detail := ""
		if json.Unmarshal(body, &gerr) == nil && len(gerr.Messages) > 0 {
			detail = gerr.Messages[0].MessageString
		}
		return nil, fmt.Errorf("gmo activeOrders rejected: status=%d %s", raw.Status, detail)
	}
	if raw.Data.List == nil {
		return []GMOActiveOrder{}, nil
	}
	return raw.Data.List, nil
}

// fetchFillPrice は約定照会から平均約定価格を取得する。失敗時は 0 を返す。
func (e *GMOOrderExecutor) fetchFillPrice(ctx context.Context, gmoSymbol, orderID string) float64 {
	_, avg := e.QueryFills(ctx, gmoSymbol, orderID)
	return avg
}

// QueryFills は約定照会から約定数量合計と平均約定価格を返す。
// 失敗・約定なし時は (0, 0) を返す。Maker待機注文の充足確認用。
func (e *GMOOrderExecutor) QueryFills(ctx context.Context, gmoSymbol, orderID string) (float64, float64) {
	timestamp := strconv.FormatInt(e.now().UnixMilli(), 10)
	// 署名対象のパスはクエリなし（activeOrders で実機確認した規則に合わせる）。
	query := "?symbol=" + gmoSymbol + "&orderId=" + orderID
	path := "/v1/executions"
	signature := signGMO(timestamp+"GET"+path, e.apiSecret)

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return 0, 0
			case <-time.After(time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.baseURL+path+query, nil)
		if err != nil {
			return 0, 0
		}
		req.Header.Set("API-KEY", e.apiKey)
		req.Header.Set("API-TIMESTAMP", timestamp)
		req.Header.Set("API-SIGN", signature)

		resp, err := e.httpClient.Do(req)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}
		var raw gmoExecutionsResponse
		if err := json.Unmarshal(body, &raw); err != nil || raw.Status != 0 {
			continue
		}
		var totalQty, totalQuote float64
		for _, ex := range raw.Data.List {
			var p, q float64
			fmt.Sscanf(ex.Price, "%f", &p)
			fmt.Sscanf(ex.Size, "%f", &q)
			totalQty += q
			totalQuote += p * q
		}
		if totalQty > 0 {
			return totalQty, totalQuote / totalQty
		}
	}
	return 0, 0
}
