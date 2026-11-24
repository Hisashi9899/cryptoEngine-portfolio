package capture

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

// serveFragments は完全文書を指定バイト数ずつに割って送る WS サーバを立てる。
func serveFragments(t *testing.T, full string, chunk int) (*websocket.Conn, func()) {
	t.Helper()
	srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		defer ws.Close()
		for i := 0; i < len(full); i += chunk {
			end := i + chunk
			if end > len(full) {
				end = len(full)
			}
			if err := websocket.Message.Send(ws, full[i:end]); err != nil {
				return
			}
		}
		time.Sleep(2 * time.Second)
	}))
	t.Cleanup(srv.Close)
	url := strings.Replace(srv.URL, "http", "ws", 1)
	ws, err := websocket.Dial(url, "", "http://localhost")
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	ws.SetDeadline(time.Now().Add(10 * time.Second))
	return ws, func() { ws.Close() }
}

func TestRecvJSONMessageFragmented(t *testing.T) {
	full := `{"channel":"orderbooks","symbol":"LINK","asks":[{"price":"1860","size":"10"}],"bids":[{"price":"1859","size":"5"}],"timestamp":"2026-09-18T00:00:00.000Z"}`
	ws, done := serveFragments(t, full, 40)
	defer done()
	raw, err := recvJSONMessage(ws)
	if err != nil {
		t.Fatalf("recv failed: %v", err)
	}
	if string(raw) != full {
		t.Fatalf("reassembly mismatch:\n got: %s\nwant: %s", string(raw), full)
	}
}

func TestRecvJSONMessageSingleFrame(t *testing.T) {
	// 分割なし（Binance 互換の小文書）は従来通り1回で読める。
	full := `{"e":"depthUpdate","s":"BTCUSDT"}`
	ws, done := serveFragments(t, full, len(full))
	defer done()
	raw, err := recvJSONMessage(ws)
	if err != nil {
		t.Fatalf("recv failed: %v", err)
	}
	if string(raw) != full {
		t.Fatalf("mismatch: got %s want %s", string(raw), full)
	}
}

func TestRecvJSONMessageTimeout(t *testing.T) {
	// 無言サーバー＋デッドラインでタイムアウトする（half-open 検出の機構）。
	srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		defer ws.Close()
		select {}
	}))
	t.Cleanup(srv.Close)
	url := strings.Replace(srv.URL, "http", "ws", 1)
	ws, err := websocket.Dial(url, "", "http://localhost")
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer ws.Close()
	ws.SetDeadline(time.Now().Add(100 * time.Millisecond))
	if _, err := recvJSONMessage(ws); err == nil {
		t.Fatal("silent server must time out")
	}
}
