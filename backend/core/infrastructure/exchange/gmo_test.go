package exchange

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestToGMOSymbol(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"BTCUSDT", "BTC"},
		{"btcusdt", "BTC"},
		{"ETHUSDT", "ETH"},
		{"BTC", "BTC"},
		{"eth", "ETH"},
		{"XRP_JPY", ""}, // レバレッジ形式はエラー（want は使わない）
	}
	for _, tc := range cases {
		got, err := ToGMOSymbol(tc.in)
		if tc.want == "" {
			if err == nil {
				t.Errorf("ToGMOSymbol(%q): expected error, got %q", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ToGMOSymbol(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ToGMOSymbol(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	for _, in := range []string{"", "LTCUSDT", "BTCUSD", "BTC_JPY", "ETH_JPY"} {
		if _, err := ToGMOSymbol(in); err == nil {
			t.Errorf("ToGMOSymbol(%q): expected error, got nil", in)
		}
	}
}

// gmoTestServer は GMO Public API のモックサーバーを返す。
func gmoTestServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(handler))
	t.Setenv("GMO_EXCHANGE_BASE_URL", srv.URL)
	return srv
}

func writeGMOJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode mock response: %v", err)
	}
}

func TestGMOClientFetchTicker(t *testing.T) {
	srv := gmoTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/ticker" {
			t.Errorf("expected /v1/ticker, got %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("symbol"); got != "BTC" {
			t.Errorf("expected symbol=BTC, got %q", got)
		}
		writeGMOJSON(t, w, map[string]any{
			"status": 0,
			"data": []map[string]string{
				{"symbol": "BTC", "last": "15000000", "bestBid": "14999000", "bestAsk": "15001000", "volume": "123.456", "high": "15200000", "low": "14800000"},
			},
			"responsetime": "2024-01-01T00:00:00.000Z",
		})
	})
	defer srv.Close()

	c := NewGMOClient(5 * time.Second)
	got, err := c.FetchTicker("BTCUSDT")
	if err != nil {
		t.Fatalf("FetchTicker: %v", err)
	}
	if got.Symbol != "BTC" || got.Last != 15000000 || got.Bid != 14999000 || got.Ask != 15001000 {
		t.Errorf("unexpected ticker: %+v", got)
	}
	if got.Volume != 123.456 {
		t.Errorf("unexpected volume: %v", got.Volume)
	}
	if got.High != 15200000 || got.Low != 14800000 {
		t.Errorf("unexpected high/low: %+v", got)
	}
}

func TestGMOClientFetchTickerAPIError(t *testing.T) {
	srv := gmoTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeGMOJSON(t, w, map[string]any{"status": 1, "data": []any{}})
	})
	defer srv.Close()

	c := NewGMOClient(5 * time.Second)
	if _, err := c.FetchTicker("BTCUSDT"); err == nil {
		t.Fatal("expected error for status != 0, got nil")
	}
}

func TestGMOClientFetchOrderBook(t *testing.T) {
	srv := gmoTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/orderbooks" {
			t.Errorf("expected /v1/orderbooks, got %s", r.URL.Path)
		}
		writeGMOJSON(t, w, map[string]any{
			"status": 0,
			"data": map[string]any{
				"symbol": "BTC",
				"asks": []map[string]string{
					{"price": "15001000", "size": "0.1"},
					{"price": "15002000", "size": "0.2"},
					{"price": "0", "size": "0.3"},
				},
				"bids": []map[string]string{
					{"price": "14999000", "size": "0.15"},
					{"price": "14998000", "size": "0.25"},
				},
			},
		})
	})
	defer srv.Close()

	c := NewGMOClient(5 * time.Second)
	ob, err := c.FetchOrderBook("BTCUSDT", 1)
	if err != nil {
		t.Fatalf("FetchOrderBook: %v", err)
	}
	if len(ob.Asks) != 1 || len(ob.Bids) != 1 {
		t.Fatalf("limit=1 should trim to 1 level each: %+v", ob)
	}
	if ob.Asks[0].Price != 15001000 || ob.Bids[0].Price != 14999000 {
		t.Errorf("unexpected levels: %+v", ob)
	}
	if want := (15001000.0 + 14999000.0) / 2.0; ob.MidPrice != want {
		t.Errorf("mid = %v, want %v", ob.MidPrice, want)
	}
	if ob.Spread != 2000 {
		t.Errorf("spread = %v, want 2000", ob.Spread)
	}
}

func TestNewGMOWSSubscribe(t *testing.T) {
	sub, err := NewGMOWSSubscribe("BTCUSDT")
	if err != nil {
		t.Fatalf("NewGMOWSSubscribe: %v", err)
	}
	raw, _ := json.Marshal(sub)
	var decoded map[string]string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("marshal subscribe: %v", err)
	}
	if decoded["command"] != "subscribe" || decoded["channel"] != "orderbooks" || decoded["symbol"] != "BTC" {
		t.Errorf("unexpected subscribe payload: %s", raw)
	}
	if _, err := NewGMOWSSubscribe("UNKNOWN"); err == nil {
		t.Error("expected error for unknown symbol, got nil")
	}
}

func TestParseGMOWSMessage(t *testing.T) {
	raw := `{"channel":"orderbooks","symbol":"BTC",` +
		`"asks":[{"price":"15001000","size":"0.1"}],` +
		`"bids":[{"price":"14999000","size":"0.2"}],` +
		`"timestamp":"2024-06-01T12:00:00.000Z"}`
	ob, err := ParseGMOWSMessage([]byte(raw), 0)
	if err != nil {
		t.Fatalf("ParseGMOWSMessage: %v", err)
	}
	if ob.Symbol != "BTC" || len(ob.Asks) != 1 || len(ob.Bids) != 1 {
		t.Errorf("unexpected book: %+v", ob)
	}
	if ob.Timestamp != 1717243200000 {
		t.Errorf("timestamp = %d, want 1717243200000", ob.Timestamp)
	}

	// timestamp 解釈失敗時は受信時刻で代替。
	rawNoTS := strings.Replace(raw, `"timestamp":"2024-06-01T12:00:00.000Z"`, `"timestamp":"bogus"`, 1)
	ob, err = ParseGMOWSMessage([]byte(rawNoTS), 12345)
	if err != nil {
		t.Fatalf("ParseGMOWSMessage (bad ts): %v", err)
	}
	if ob.Timestamp != 12345 {
		t.Errorf("fallback timestamp = %d, want 12345", ob.Timestamp)
	}

	// 異常系: 不正 JSON / 他チャネル / 片側空板。
	for name, msg := range map[string]string{
		"invalid json":  `{oops`,
		"other channel": `{"channel":"ticker","symbol":"BTC","asks":[],"bids":[]}`,
		"empty asks":    `{"channel":"orderbooks","symbol":"BTC","asks":[],"bids":[{"price":"1","size":"1"}]}`,
		"empty bids":    `{"channel":"orderbooks","symbol":"BTC","asks":[{"price":"1","size":"1"}],"bids":[]}`,
	} {
		if _, err := ParseGMOWSMessage([]byte(msg), 0); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}
