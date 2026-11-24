package trading

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cryptoEngine/core/domain"
	"github.com/cryptoEngine/core/infrastructure/logging"
)

// testServer は Binance /api/v3/order のモックサーバー。
// handler はリクエストのクエリ（署名含む）を検証する。
func testServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(handler))
}

func newTestExecutor(baseURL string) *BinanceOrderExecutor {
	return &BinanceOrderExecutor{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     "test-api-key",
		apiSecret:  "test-api-secret",
		logger:     logging.NewLoggerTo(io.Discard),
	}
}

func TestSignHMACSHA256(t *testing.T) {
	// Binance 公式ドキュメントの例と同等の形式（symbol 等を含む query 文字列）。
	got := signHMACSHA256("symbol=LTCBTC&side=BUY&type=LIMIT&timeInForce=GTC&quantity=1&price=0.1&recvWindow=5000&timestamp=1499827319559", "NhqPtmdSJYdKjVHjA7PZj4Mge3R5YNiP1e3UZjInClVN65XAbvqqM6A7H5fATj0j")
	want := "c8db56825ae71d6d79447849e617115f4a920fa2acdcab2b053c4b2838bd6b71"
	if got != want {
		t.Fatalf("signature mismatch:\n got=%s\nwant=%s", got, want)
	}
}

func TestMarketOrderFilled(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v3/order" {
			t.Errorf("expected /api/v3/order, got %s", r.URL.Path)
		}
		if got := r.Header.Get("X-MBX-APIKEY"); got != "test-api-key" {
			t.Errorf("expected api key header, got %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		q := string(body)
		if !strings.Contains(q, "side=BUY") || !strings.Contains(q, "type=MARKET") {
			t.Errorf("unexpected body: %s", q)
		}
		// 署名が付与されていること。
		if !strings.Contains(q, "signature=") {
			t.Errorf("missing signature in body: %s", q)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"symbol":"BTCUSDT","orderId":12345,"status":"FILLED","executedQty":"0.00100000","cummulativeQuoteQty":"55.25000000","transactTime":1700000000000}`))
	})
	defer srv.Close()

	ex := newTestExecutor(srv.URL)
	res, err := ex.MarketOrder(context.Background(), "BTCUSDT", domain.TradeBuy, 0.001)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OrderID != "12345" {
		t.Errorf("orderId: expected 12345, got %s", res.OrderID)
	}
	if res.Status != "filled" {
		t.Errorf("status: expected filled, got %s", res.Status)
	}
	if res.Price != 55250 {
		t.Errorf("price: expected 55250, got %.2f", res.Price)
	}
}

func TestMarketOrderRetryOn5xx(t *testing.T) {
	var calls int32
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"code":-1,"msg":"temporary"}`))
			return
		}
		_, _ = w.Write([]byte(`{"symbol":"BTCUSDT","orderId":99,"status":"FILLED","executedQty":"0.00100000","cummulativeQuoteQty":"55.25000000","transactTime":1700000000000}`))
	})
	defer srv.Close()

	ex := newTestExecutor(srv.URL)
	res, err := ex.MarketOrder(context.Background(), "BTCUSDT", domain.TradeBuy, 0.001)
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if res.OrderID != "99" {
		t.Errorf("expected orderId 99, got %s", res.OrderID)
	}
	if calls != 3 {
		t.Errorf("expected 3 attempts, got %d", calls)
	}
}

func TestMarketOrderRejectedOn4xxNoRetry(t *testing.T) {
	var calls int32
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":-2015,"msg":"Invalid API-key"}`))
	})
	defer srv.Close()

	ex := newTestExecutor(srv.URL)
	_, err := ex.MarketOrder(context.Background(), "BTCUSDT", domain.TradeBuy, 0.001)
	if err == nil {
		t.Fatal("expected error for 4xx")
	}
	if !strings.Contains(err.Error(), "order rejected") {
		t.Errorf("unexpected error message: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected no retry on 4xx, got %d calls", calls)
	}
}

func TestMarketOrderMissingKeys(t *testing.T) {
	ex := newTestExecutor("http://example.com")
	ex.apiKey = ""
	ex.apiSecret = ""
	_, err := ex.MarketOrder(context.Background(), "BTCUSDT", domain.TradeBuy, 0.001)
	if err == nil {
		t.Fatal("expected error when api key not configured")
	}
}

func TestMarketOrderBadQuantity(t *testing.T) {
	ex := newTestExecutor("http://example.com")
	_, err := ex.MarketOrder(context.Background(), "BTCUSDT", domain.TradeBuy, 0)
	if err == nil {
		t.Fatal("expected error for zero quantity")
	}
}
