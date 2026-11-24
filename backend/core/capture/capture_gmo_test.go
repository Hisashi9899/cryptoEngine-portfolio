package capture

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func TestGmoWSSymbol(t *testing.T) {
	if got := gmoWSSymbol("BTC"); got != "BTC_JPY" {
		t.Fatalf("short must gain suffix, got %s", got)
	}
	if got := gmoWSSymbol("ETH_JPY"); got != "ETH_JPY" {
		t.Fatalf("suffixed must stay, got %s", got)
	}
	if got := gmoWSSymbol(""); got != "BTC_JPY" {
		t.Fatalf("empty must fall back, got %s", got)
	}
}

// serveGMOMulti は複数銘柄の板を流す偽GMOサーバを立てる。
// 購読メッセージを読み飛ばし、各銘柄に1冊ずつ送る。
func serveGMOMulti(t *testing.T, syms []string) string {
	t.Helper()
	srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		defer ws.Close()
		ws.SetDeadline(time.Now().Add(10 * time.Second))
		// 購読を全て読む（ペーシングされるため全受信まで待つ）。
		for range syms {
			var sub map[string]string
			if err := websocket.JSON.Receive(ws, &sub); err != nil {
				return
			}
		}
		for _, s := range syms {
			msg, _ := json.Marshal(map[string]any{
				"channel": "orderbooks", "symbol": s,
				"asks":      []map[string]string{{"price": "101", "size": "2"}},
				"bids":      []map[string]string{{"price": "100", "size": "3"}},
				"timestamp": "2026-10-05T00:00:00.000Z",
			})
			if err := websocket.Message.Send(ws, string(msg)); err != nil {
				return
			}
		}
		time.Sleep(3 * time.Second)
	}))
	t.Cleanup(srv.Close)
	return strings.Replace(srv.URL, "http", "ws", 1)
}

func TestStreamGMOBooks(t *testing.T) {
	// 1接続で複数銘柄が購読・仕分けされること（ERR-5003恒久対策）。
	old := gmoWSURL
	defer func() { gmoWSURL = old }()
	gmoWSURL = serveGMOMulti(t, []string{"BTC_JPY", "ETH_JPY"})
	stop := make(chan struct{})
	defer close(stop)
	chans, err := StreamGMOBooks([]string{"BTC", "ETH"}, 5, 50*time.Millisecond, stop)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	for _, sym := range []string{"BTC", "ETH"} {
		select {
		case snap := <-chans[sym]:
			if snap.MidPrice != 100.5 {
				t.Fatalf("%s mid = %v, want 100.5", sym, snap.MidPrice)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s got no book", sym)
		}
	}
	if _, err := StreamGMOBooks(nil, 5, time.Second, stop); err == nil {
		t.Fatal("empty symbols must error")
	}
}

// serveGMOShortFallback は `ASTR_JPY` に ERR-5106 を返し、短縮形 `ASTR` にだけ
// 板を配信する偽GMOサーバ。単一銘柄購読のフォールバックを検証する。
func serveGMOShortFallback(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		defer ws.Close()
		ws.SetDeadline(time.Now().Add(10 * time.Second))
		for {
			var sub map[string]string
			if err := websocket.JSON.Receive(ws, &sub); err != nil {
				return
			}
			if sub["symbol"] == "ASTR" {
				for i := 0; i < 20; i++ {
					msg, _ := json.Marshal(map[string]any{
						"channel": "orderbooks", "symbol": "ASTR",
						"asks":      []map[string]string{{"price": "101", "size": "2"}},
						"bids":      []map[string]string{{"price": "100", "size": "3"}},
						"timestamp": "2026-10-05T00:00:00.000Z",
					})
					if err := websocket.Message.Send(ws, string(msg)); err != nil {
						return
					}
					time.Sleep(20 * time.Millisecond)
				}
				return
			}
			// 誤った形式（ASTR_JPY）には不正銘柄エラーを返す。
			errMsg, _ := json.Marshal(map[string]string{"error": "ERR-5106 Invalid request parameter. symbol"})
			if err := websocket.Message.Send(ws, string(errMsg)); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return strings.Replace(srv.URL, "http", "ws", 1)
}

func TestReadLoopGMOShortFormFallback(t *testing.T) {
	// ERR-5106 を受けた銘柄が短縮形へ切り替わり板を受信できること。
	// これが無いと capture-daily / vt-paper が 5 銘柄で無言の 0 行になる。
	old := gmoWSURL
	defer func() { gmoWSURL = old }()
	gmoWSURL = serveGMOShortFallback(t)
	stop := make(chan struct{})
	defer close(stop)
	ch, err := Stream(Options{Symbol: "ASTR", Source: "gmo", SampleEvery: 20 * time.Millisecond}, stop)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	select {
	case snap := <-ch:
		if snap.MidPrice != 100.5 {
			t.Fatalf("mid = %v, want 100.5", snap.MidPrice)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("short-form fallback produced no book")
	}
}
