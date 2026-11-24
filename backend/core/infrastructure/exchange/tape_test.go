package exchange

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cryptoEngine/core/domain"
)

func TestGMOProxyFetchExecutions(t *testing.T) {
	srv := gmoTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/trades" {
			t.Errorf("expected /v1/trades, got %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("symbol"); got != "BTC" {
			t.Errorf("expected symbol=BTC, got %q", got)
		}
		// 実 API 同様に同一マッチが BUY/SELL 両建てで返る。方向は
		// ティックルールで付く（100→101 上向き＝買い、101→101 継承）。
		writeGMOJSON(t, w, map[string]any{
			"status": 0,
			"data": map[string]any{
				"list": []any{
					map[string]any{"price": "100", "size": "0.5", "side": "SELL", "timestamp": "2026-09-21T00:00:01.000Z"},
					map[string]any{"price": "101", "size": "0.25", "side": "SELL", "timestamp": "2026-09-21T00:00:02.000Z"},
					map[string]any{"price": "101", "size": "0.1", "side": "BUY", "timestamp": "2026-09-21T00:00:03.000Z"},
					map[string]any{"price": "0", "size": "1", "side": "BUY", "timestamp": ""},
				},
			},
		})
	})
	defer srv.Close()
	c := NewGMOClient(5 * time.Second)
	got, err := c.FetchExecutions("BTC", 10)
	if err != nil {
		t.Fatalf("FetchExecutions: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 valid executions, got %d", len(got))
	}
	if got[0].Side != domain.TradeBuy || got[0].Price != 100 || got[0].Qty != 0.5 {
		t.Fatalf("unexpected first execution: %+v", got[0])
	}
	// 100→101 の上ティックで買い（side フィールドの SELL は無視される）。
	if got[1].Side != domain.TradeBuy {
		t.Fatalf("expected tick-rule buy, got %+v", got[1])
	}
	// 同値は直前を継承する。
	if got[2].Side != domain.TradeBuy {
		t.Fatalf("expected carried buy, got %+v", got[2])
	}
	if got[0].Timestamp <= 0 {
		t.Fatalf("expected parsed timestamp, got %+v", got[0])
	}
}

func TestBinanceFetchExecutions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/aggTrades" {
			t.Errorf("expected /api/v3/aggTrades, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"p":"100","q":"0.5","T":1700000000000,"m":false},{"p":"101","q":"0.25","T":1700000001000,"m":true}]`))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, 5*time.Second)
	got, err := c.FetchExecutions("BTCUSDT", 10)
	if err != nil {
		t.Fatalf("FetchExecutions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 executions, got %d", len(got))
	}
	// m=false → buyer aggressor → buy.
	if got[0].Side != domain.TradeBuy {
		t.Fatalf("expected buy aggressor, got %+v", got[0])
	}
	// m=true → seller aggressor → sell.
	if got[1].Side != domain.TradeSell {
		t.Fatalf("expected sell aggressor, got %+v", got[1])
	}
}

func TestClampTapeLimit(t *testing.T) {
	if clampTapeLimit(0) != 100 || clampTapeLimit(-5) != 100 {
		t.Fatal("expected default 100")
	}
	if clampTapeLimit(5000) != 1000 {
		t.Fatal("expected cap 1000")
	}
	if clampTapeLimit(50) != 50 {
		t.Fatal("expected passthrough 50")
	}
}
