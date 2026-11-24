package trading

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cryptoEngine/core/domain"
	"github.com/cryptoEngine/core/infrastructure/exchange"
	"github.com/cryptoEngine/core/infrastructure/logging"
)

// newTestGMOExecutor はモックサーバー向けの GMO executor を返す。
// now は JST 換算でメンテ時間帯外（平日）に固定する。
// 資金ガード用の ticker 取得も同一モックに向ける。
func newTestGMOExecutor(t *testing.T, baseURL string) *GMOOrderExecutor {
	t.Helper()
	t.Setenv("GMO_EXCHANGE_BASE_URL", strings.TrimRight(baseURL, "/"))
	weekday := time.Date(2024, 6, 3, 12, 0, 0, 0, time.UTC) // 月曜
	return &GMOOrderExecutor{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     "test-api-key",
		apiSecret:  "test-api-secret",
		logger:     logging.NewLoggerTo(io.Discard),
		now:        func() time.Time { return weekday },
		mdClient:   exchange.NewGMOClient(5 * time.Second),
	}
}

// serveGMOCommon は資金ガード用の共通モック（assets＋ticker）。処理したら true。
func serveGMOCommon(t *testing.T, w http.ResponseWriter, r *http.Request) bool {
	t.Helper()
	switch r.URL.Path {
	case "/v1/account/assets":
		writeGMOJSON(t, w, map[string]any{"status": 0, "data": []map[string]string{
			{"symbol": "JPY", "balance": "100000", "available": "100000"},
			{"symbol": "BTC", "balance": "1", "available": "1"},
			{"symbol": "XRP", "balance": "1000", "available": "1000"},
		}})
		return true
	case "/v1/ticker":
		sym := r.URL.Query().Get("symbol")
		price := "15000000"
		if sym == "XRP" {
			price = "220"
		}
		writeGMOJSON(t, w, map[string]any{"status": 0, "data": []map[string]string{
			{"symbol": sym, "last": price, "bestBid": price, "bestAsk": price, "volume": "1"},
		}})
		return true
	}
	return false
}

func TestSignGMOFormat(t *testing.T) {
	// 署名対象 = timestamp + method + path + body の HMAC-SHA256 hex。
	got := signGMO("1680000000000POST/v1/order{}", "secret")
	if len(got) != 64 {
		t.Fatalf("signature should be 64 hex chars, got %q", got)
	}
	if got != signGMO("1680000000000POST/v1/order{}", "secret") {
		t.Fatal("signature is not deterministic")
	}
	if got == signGMO("1680000000000POST/v1/order{}", "other") {
		t.Fatal("signature should depend on secret")
	}
	if got == signGMO("1680000000001POST/v1/order{}", "secret") {
		t.Fatal("signature should depend on payload")
	}
}

func TestInMaintenanceWindow(t *testing.T) {
	jst := time.FixedZone("JST", 9*60*60)
	cases := []struct {
		name string
		when time.Time
		want bool
	}{
		{"sat 10:00 JST", time.Date(2024, 6, 1, 10, 0, 0, 0, jst), true},
		{"sat 08:59 JST", time.Date(2024, 6, 1, 8, 59, 0, 0, jst), false},
		{"sat 11:00 JST", time.Date(2024, 6, 1, 11, 0, 0, 0, jst), false},
		{"mon 10:00 JST", time.Date(2024, 6, 3, 10, 0, 0, 0, jst), false},
		{"sat 01:00 UTC (=10:00 JST)", time.Date(2024, 6, 1, 1, 0, 0, 0, time.UTC), true},
	}
	for _, tc := range cases {
		if got := inMaintenanceWindow(tc.when); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestGMOOrderEnvFallback(t *testing.T) {
	t.Setenv("GMO_API_KEY", "gmo-key")
	t.Setenv("GMO_API_SECRET", "gmo-secret")
	e := NewGMOOrderExecutor(logging.NewLoggerTo(io.Discard))
	if e.apiKey != "gmo-key" || e.apiSecret != "gmo-secret" {
		t.Errorf("GMO_* should take precedence: %+v", e)
	}

	t.Setenv("GMO_API_KEY", "")
	t.Setenv("GMO_API_SECRET", "")
	t.Setenv("BINANCE_API_KEY", "legacy-key")
	t.Setenv("BINANCE_API_SECRET", "legacy-secret")
	e = NewGMOOrderExecutor(logging.NewLoggerTo(io.Discard))
	if e.apiKey != "legacy-key" || e.apiSecret != "legacy-secret" {
		t.Errorf("BINANCE_* fallback broken: %+v", e)
	}
}

func TestGMOMarketOrderValidation(t *testing.T) {
	e := newTestGMOExecutor(t, "http://127.0.0.1:1")
	ctx := context.Background()
	for name, tc := range map[string]struct {
		symbol string
		side   domain.TradeSide
		qty    float64
	}{
		"empty symbol":   {"", domain.TradeBuy, 0.001},
		"zero quantity":  {"BTC", domain.TradeBuy, 0},
		"bad side":       {"BTC", "hold", 0.001},
		"unknown symbol": {"LTCUSDT", domain.TradeBuy, 0.001},
		"below min":      {"BTC", domain.TradeBuy, 0.000001},
	} {
		if _, err := e.MarketOrder(ctx, tc.symbol, tc.side, tc.qty); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}

	// レバレッジ形式（_JPY）は誤発注防止のため明示的に拒否する（現物形式を要求）。
	// Binance 形式（BTCUSDT）は現物への変換機能として許容する。
	_, err := e.MarketOrder(ctx, "BTC_JPY", domain.TradeBuy, 0.001)
	if err == nil || !strings.Contains(err.Error(), "spot symbol") {
		t.Errorf("expected leverage rejection, got %v", err)
	}

	// キー未設定は発注前に失敗する。
	bare := &GMOOrderExecutor{logger: logging.NewLoggerTo(io.Discard), now: time.Now}
	if _, err := bare.MarketOrder(ctx, "BTC", domain.TradeBuy, 0.001); err == nil ||
		!strings.Contains(err.Error(), "GMO_API_KEY") {
		t.Errorf("expected missing-key error, got %v", err)
	}
}

func TestGMOMarketOrderMaintenance(t *testing.T) {
	e := newTestGMOExecutor(t, "http://127.0.0.1:1")
	sat := time.Date(2024, 6, 1, 10, 0, 0, 0, time.FixedZone("JST", 9*60*60))
	e.now = func() time.Time { return sat }
	_, err := e.MarketOrder(context.Background(), "BTC", domain.TradeBuy, 0.001)
	if err == nil || !strings.Contains(err.Error(), "maintenance") {
		t.Fatalf("expected maintenance refusal, got %v", err)
	}
}

func TestGMOMarketOrderFilled(t *testing.T) {
	var gotOrderBody map[string]string
	var gotHeaders http.Header
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if serveGMOCommon(t, w, r) {
			return
		}
		switch r.URL.Path {
		case "/v1/order":
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			gotHeaders = r.Header.Clone()
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &gotOrderBody); err != nil {
				t.Errorf("unmarshal order body: %v", err)
			}
			writeGMOJSON(t, w, map[string]any{"status": 0, "data": "682749", "responsetime": "x"})
		case "/v1/executions":
			if r.Method != http.MethodGet {
				t.Errorf("expected GET, got %s", r.Method)
			}
			if r.URL.Query().Get("symbol") != "BTC" || r.URL.Query().Get("orderId") != "682749" {
				t.Errorf("unexpected executions query: %s", r.URL.RawQuery)
			}
			writeGMOJSON(t, w, map[string]any{
				"status": 0,
				"data": map[string]any{
					"list": []map[string]string{
						{"price": "15000000", "size": "0.001"},
						{"price": "15002000", "size": "0.001"},
					},
				},
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer srv.Close()

	e := newTestGMOExecutor(t, srv.URL)
	out, err := e.MarketOrder(context.Background(), "BTC", domain.TradeBuy, 0.002)
	if err != nil {
		t.Fatalf("MarketOrder: %v", err)
	}
	if gotOrderBody["symbol"] != "BTC" || gotOrderBody["side"] != "BUY" || gotOrderBody["executionType"] != "MARKET" {
		t.Errorf("unexpected order body: %v", gotOrderBody)
	}
	if gotHeaders.Get("API-KEY") != "test-api-key" || gotHeaders.Get("API-TIMESTAMP") == "" || gotHeaders.Get("API-SIGN") == "" {
		t.Errorf("missing auth headers: %v", gotHeaders)
	}
	if len(gotHeaders.Get("API-SIGN")) != 64 {
		t.Errorf("API-SIGN should be 64 hex chars: %q", gotHeaders.Get("API-SIGN"))
	}
	if out.OrderID != "682749" || out.Status != "filled" {
		t.Errorf("unexpected result: %+v", out)
	}
	wantPrice := (15000000.0*0.001 + 15002000.0*0.001) / 0.002
	if diff := out.Price - wantPrice; diff < -1e-6 || diff > 1e-6 {
		t.Errorf("fill price = %v, want %v", out.Price, wantPrice)
	}
	if out.Symbol != "BTC" || out.Quantity != 0.002 {
		t.Errorf("unexpected result echo: %+v", out)
	}
}

func TestGMOMarketOrderRejected(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if serveGMOCommon(t, w, r) {
			return
		}
		writeGMOJSON(t, w, map[string]any{
			"status": 100,
			"messages": []map[string]string{
				{"message_code": "ERR-100", "message_string": "validation error"},
			},
		})
	})
	defer srv.Close()

	e := newTestGMOExecutor(t, srv.URL)
	_, err := e.MarketOrder(context.Background(), "BTC", domain.TradeSell, 0.001)
	if err == nil || !strings.Contains(err.Error(), "validation error") {
		t.Fatalf("expected business error detail, got %v", err)
	}
}

func TestGMOMarketOrderNoFillYet(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if serveGMOCommon(t, w, r) {
			return
		}
		if r.URL.Path == "/v1/order" {
			writeGMOJSON(t, w, map[string]any{"status": 0, "data": "1"})
			return
		}
		writeGMOJSON(t, w, map[string]any{"status": 0, "data": map[string]any{"list": []any{}}})
	})
	defer srv.Close()

	e := newTestGMOExecutor(t, srv.URL)
	out, err := e.MarketOrder(context.Background(), "BTC", domain.TradeBuy, 0.001)
	if err != nil {
		t.Fatalf("MarketOrder: %v", err)
	}
	if out.Price != 0 || out.Status != "filled" {
		t.Errorf("unfilled should return price 0 + filled: %+v", out)
	}
}

func TestGMOMarketOrderRetryOnHTTP500(t *testing.T) {
	var calls int
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if serveGMOCommon(t, w, r) {
			return
		}
		if r.URL.Path == "/v1/executions" {
			writeGMOJSON(t, w, map[string]any{"status": 0, "data": map[string]any{"list": []any{}}})
			return
		}
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("boom"))
			return
		}
		writeGMOJSON(t, w, map[string]any{"status": 0, "data": "9"})
	})
	defer srv.Close()

	e := newTestGMOExecutor(t, srv.URL)
	out, err := e.MarketOrder(context.Background(), "BTC", domain.TradeBuy, 0.001)
	if err != nil {
		t.Fatalf("MarketOrder after 500: %v", err)
	}
	if calls != 2 || out.OrderID != "9" {
		t.Errorf("expected 1 retry then success: calls=%d out=%+v", calls, out)
	}
}

func TestGMOFetchAssets(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/account/assets" {
			t.Errorf("expected /v1/account/assets, got %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("assets must be read-only GET, got %s", r.Method)
		}
		if r.Header.Get("API-KEY") != "test-api-key" || r.Header.Get("API-SIGN") == "" {
			t.Errorf("missing auth headers: %v", r.Header)
		}
		writeGMOJSON(t, w, map[string]any{
			"status": 0,
			"data": []map[string]string{
				{"symbol": "JPY", "balance": "5000", "available": "5000"},
				{"symbol": "BTC", "balance": "0.0001", "available": "0.0001"},
			},
		})
	})
	defer srv.Close()

	e := newTestGMOExecutor(t, srv.URL)
	assets, err := e.FetchAssets(context.Background())
	if err != nil {
		t.Fatalf("FetchAssets: %v", err)
	}
	if len(assets) != 2 || assets[0].Symbol != "JPY" || assets[1].Available != "0.0001" {
		t.Errorf("unexpected assets: %+v", assets)
	}

	// キー未設定は即時失敗。
	bare := &GMOOrderExecutor{logger: logging.NewLoggerTo(io.Discard), now: time.Now}
	if _, err := bare.FetchAssets(context.Background()); err == nil {
		t.Error("expected missing-key error, got nil")
	}
}

func TestGMOStepRounding(t *testing.T) {
	// BTC 刻み 0.00001: 0.000019 -> 0.00001。
	if got := roundDownSize(0.000019, 0.00001); got != 0.00001 {
		t.Errorf("roundDownSize(0.000019, 0.00001) = %v, want 0.00001", got)
	}
	// XRP 刻み 1: 15.7 -> 15。
	if got := roundDownSize(15.7, 1); got != 15 {
		t.Errorf("roundDownSize(15.7, 1) = %v, want 15", got)
	}
}

func TestGMOMarketOrderSizeRules(t *testing.T) {
	e := newTestGMOExecutor(t, "http://127.0.0.1:1")
	ctx := context.Background()

	// 未対応シンボルは fail-closed（形式段階で拒否）。
	_, err := e.MarketOrder(ctx, "ZZZ", domain.TradeBuy, 1)
	if err == nil || !strings.Contains(err.Error(), "unsupported symbol") {
		t.Errorf("expected unsupported-symbol error, got %v", err)
	}
	// FCR は現物17銘柄として size rule あり（Min 1 Step 1）。
	// 0.5 は最小未満で拒否（HTTP 前に失敗する）。
	_, err = e.MarketOrder(ctx, "FCR", domain.TradeBuy, 0.5)
	if err == nil || !strings.Contains(err.Error(), "below minimum") {
		t.Errorf("expected below-minimum error, got %v", err)
	}
	// BTC 0.000001 は最小 0.00001 未満で拒否（HTTP 前に失敗する）。
	_, err = e.MarketOrder(ctx, "BTC", domain.TradeBuy, 0.000001)
	if err == nil || !strings.Contains(err.Error(), "below minimum") {
		t.Errorf("expected below-minimum error, got %v", err)
	}
}

func TestGMOMarketOrderXRPFilled(t *testing.T) {
	var gotSize string
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if serveGMOCommon(t, w, r) {
			return
		}
		switch r.URL.Path {
		case "/v1/order":
			body, _ := io.ReadAll(r.Body)
			var req map[string]string
			if err := json.Unmarshal(body, &req); err != nil {
				t.Errorf("unmarshal order body: %v", err)
			}
			if req["symbol"] != "XRP" {
				t.Errorf("expected XRP, got %v", req)
			}
			gotSize = req["size"]
			writeGMOJSON(t, w, map[string]any{"status": 0, "data": "77"})
		case "/v1/executions":
			writeGMOJSON(t, w, map[string]any{"status": 0, "data": map[string]any{"list": []any{}}})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer srv.Close()

	e := newTestGMOExecutor(t, srv.URL)
	// XRP は刻み 1 のため 15 はそのまま。
	out, err := e.MarketOrder(context.Background(), "XRP", domain.TradeBuy, 15)
	if err != nil {
		t.Fatalf("MarketOrder XRP: %v", err)
	}
	if gotSize != "15" {
		t.Errorf("order size = %q, want %q", gotSize, "15")
	}
	if out.Quantity != 15 {
		t.Errorf("quantity = %v, want 15", out.Quantity)
	}
}

func TestGMOLimitOrderBody(t *testing.T) {
	var gotBody map[string]string
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if serveGMOCommon(t, w, r) {
			return
		}
		switch r.URL.Path {
		case "/v1/order":
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &gotBody); err != nil {
				t.Errorf("unmarshal order body: %v", err)
			}
			writeGMOJSON(t, w, map[string]any{"status": 0, "data": "55"})
		case "/v1/executions":
			writeGMOJSON(t, w, map[string]any{"status": 0, "data": map[string]any{"list": []any{}}})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer srv.Close()

	e := newTestGMOExecutor(t, srv.URL)
	out, err := e.LimitOrder(context.Background(), "XRP", domain.TradeBuy, 10, 220.5, "")
	if err != nil {
		t.Fatalf("LimitOrder: %v", err)
	}
	if gotBody["executionType"] != "LIMIT" || gotBody["price"] != "220.5" || gotBody["timeInForce"] != "FAK" {
		t.Errorf("unexpected limit body: %v", gotBody)
	}
	if gotBody["size"] != "10" || out.Status != "filled" {
		t.Errorf("unexpected size/status: %v %+v", gotBody, out)
	}

	// 不正入力は発注前に失敗する。
	for name, tc := range map[string]struct {
		price float64
		tif   string
	}{
		"zero price":     {0, "FAK"},
		"negative price": {-1, "FAK"},
		"bad tif":        {220.5, "GTC"},
	} {
		if _, err := e.LimitOrder(context.Background(), "XRP", domain.TradeBuy, 10, tc.price, tc.tif); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

func TestGMOCancelOrder(t *testing.T) {
	var gotBody map[string]string
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if serveGMOCommon(t, w, r) {
			return
		}
		if r.URL.Path != "/v1/cancelOrder" {
			t.Errorf("expected /v1/cancelOrder, got %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Errorf("unmarshal cancel body: %v", err)
		}
		writeGMOJSON(t, w, map[string]any{"status": 0})
	})
	defer srv.Close()

	e := newTestGMOExecutor(t, srv.URL)
	if err := e.CancelOrder(context.Background(), "682749"); err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
	if gotBody["orderId"] != "682749" {
		t.Errorf("cancel orderId = %q, want 682749", gotBody["orderId"])
	}
	if err := e.CancelOrder(context.Background(), ""); err == nil {
		t.Error("empty order id must fail")
	}
}

func TestGMOFetchActiveOrders(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/activeOrders" {
			t.Errorf("expected /v1/activeOrders, got %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("symbol"); got != "XRP" {
			t.Errorf("expected symbol=XRP, got %q", got)
		}
		writeGMOJSON(t, w, map[string]any{
			"status": 0,
			"data": map[string]any{
				"list": []map[string]string{
					{"orderId": "1", "symbol": "XRP", "side": "BUY", "executionType": "LIMIT", "price": "200", "size": "10", "status": "ACTIVE"},
				},
			},
		})
	})
	defer srv.Close()

	e := newTestGMOExecutor(t, srv.URL)
	orders, err := e.FetchActiveOrders(context.Background(), "XRP")
	if err != nil {
		t.Fatalf("FetchActiveOrders: %v", err)
	}
	if len(orders) != 1 || orders[0].OrderID != "1" || orders[0].Status != "ACTIVE" {
		t.Errorf("unexpected orders: %+v", orders)
	}

	// 業務エラー時はエラーを返す。
	srv2 := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeGMOJSON(t, w, map[string]any{"status": 1, "messages": []map[string]string{{"message_code": "E", "message_string": "restricted"}}})
	})
	defer srv2.Close()
	e2 := newTestGMOExecutor(t, srv2.URL)
	if _, err := e2.FetchActiveOrders(context.Background(), "XRP"); err == nil ||
		!strings.Contains(err.Error(), "restricted") {
		t.Errorf("expected restricted error, got %v", err)
	}
}

func TestGMOCheckFunds(t *testing.T) {
	// 残高不足モック（JPY 500 のみ）。
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/account/assets":
			writeGMOJSON(t, w, map[string]any{"status": 0, "data": []map[string]string{
				{"symbol": "JPY", "balance": "500", "available": "500"},
				{"symbol": "XRP", "balance": "0", "available": "0"},
			}})
		case "/v1/ticker":
			writeGMOJSON(t, w, map[string]any{"status": 0, "data": []map[string]string{
				{"symbol": "XRP", "last": "220", "bestBid": "219.9", "bestAsk": "220.1", "volume": "1"},
			}})
		default:
			t.Errorf("no order HTTP expected, got %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer srv.Close()

	e := newTestGMOExecutor(t, srv.URL)
	// BUY 10 XRP（約 2,201 円）は残高 500 円で拒否。発注 HTTP は飛ばない。
	_, err := e.MarketOrder(context.Background(), "XRP", domain.TradeBuy, 10)
	if err == nil || !strings.Contains(err.Error(), "insufficient JPY") {
		t.Errorf("expected insufficient-JPY error, got %v", err)
	}
	// SELL は XRP 保有 0 で拒否。
	_, err = e.MarketOrder(context.Background(), "XRP", domain.TradeSell, 10)
	if err == nil || !strings.Contains(err.Error(), "insufficient XRP") {
		t.Errorf("expected insufficient-XRP error, got %v", err)
	}
}

func TestCheckAffordability(t *testing.T) {
	assets := []GMOAsset{
		{Symbol: "JPY", Balance: "5000", Available: "5000"},
	}
	prices := map[string]float64{"BTC": 12000000, "XRP": 220}
	fetch := func(_ context.Context, symbol string) (*domain.Ticker, error) {
		return &domain.Ticker{Symbol: symbol, Last: prices[symbol]}, nil
	}
	report, err := CheckAffordability(assets, fetch)
	if err != nil {
		t.Fatalf("CheckAffordability: %v", err)
	}
	if report.JPYAvailable != 5000 {
		t.Errorf("jpy = %v, want 5000", report.JPYAvailable)
	}
	bySymbol := map[string]SymbolAffordability{}
	for _, s := range report.Symbols {
		bySymbol[s.Symbol] = s
	}
	// BTC 最小 0.00001（約 120 円）、XRP 最小 1（約 220 円）は 5000 円で可。
	if !bySymbol["BTC"].Affordable {
		t.Error("BTC should be affordable with 5000 JPY")
	}
	if !bySymbol["XRP"].Affordable {
		t.Error("XRP should be affordable with 5000 JPY")
	}
	wantMinJPY := 1.0 * 220 * 1.0005
	if diff := bySymbol["XRP"].MinJPY - wantMinJPY; diff < -1e-9 || diff > 1e-9 {
		t.Errorf("XRP minJPY = %v, want %v", bySymbol["XRP"].MinJPY, wantMinJPY)
	}
	// 余力 100 円では両方不可。
	poor := []GMOAsset{{Symbol: "JPY", Balance: "100", Available: "100"}}
	report, err = CheckAffordability(poor, fetch)
	if err != nil {
		t.Fatalf("CheckAffordability poor: %v", err)
	}
	for _, s := range report.Symbols {
		if s.Symbol == "BTC" || s.Symbol == "XRP" {
			if s.Affordable {
				t.Errorf("%s should be unaffordable with 100 JPY", s.Symbol)
			}
		}
	}
	// ティッカー失敗時は価格なしの行になる（落とさない）。
	broken := func(_ context.Context, symbol string) (*domain.Ticker, error) {
		return nil, fmt.Errorf("down")
	}
	report, err = CheckAffordability(assets, broken)
	if err != nil {
		t.Fatalf("CheckAffordability broken ticker: %v", err)
	}
	if len(report.Symbols) != len(gmoSizeRules) {
		t.Errorf("all symbols should be listed: %d", len(report.Symbols))
	}
}

func writeGMOJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode mock response: %v", err)
	}
}

func TestGMOQueryFills(t *testing.T) {
	// 約定数量合計と平均価格を返す。約定なしは (0,0)。
	mk := func(list []map[string]string) *httptest.Server {
		return testServer(t, func(w http.ResponseWriter, r *http.Request) {
			if serveGMOCommon(t, w, r) {
				return
			}
			if r.URL.Path != "/v1/executions" {
				t.Errorf("expected /v1/executions, got %s", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
				return
			}
			writeGMOJSON(t, w, map[string]any{"status": 0, "data": map[string]any{"list": list}})
		})
	}
	srv := mk([]map[string]string{
		{"price": "100", "size": "2"},
		{"price": "110", "size": "2"},
	})
	defer srv.Close()
	e := newTestGMOExecutor(t, srv.URL)
	qty, avg := e.QueryFills(context.Background(), "BTC", "1")
	if qty != 4 || avg != 105 {
		t.Fatalf("fills = (%v,%v), want (4,105)", qty, avg)
	}
	srv2 := mk(nil)
	defer srv2.Close()
	e2 := newTestGMOExecutor(t, srv2.URL)
	if q, a := e2.QueryFills(context.Background(), "BTC", "2"); q != 0 || a != 0 {
		t.Fatalf("empty fills = (%v,%v), want (0,0)", q, a)
	}
}
