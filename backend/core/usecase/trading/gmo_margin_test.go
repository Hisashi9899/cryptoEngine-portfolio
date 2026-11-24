package trading

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/cryptoEngine/core/domain"
)

func TestToMarginSymbol(t *testing.T) {
	if got, err := toMarginSymbol("xrp"); err != nil || got != "XRP_JPY" {
		t.Fatalf("got %q, %v", got, err)
	}
	if got, err := toMarginSymbol("BTC_JPY"); err != nil || got != "BTC_JPY" {
		t.Fatalf("passthrough: got %q, %v", got, err)
	}
	if _, err := toMarginSymbol("XRPUSDT"); err == nil {
		t.Fatal("binance form must fail")
	}
	if _, err := toMarginSymbol(""); err == nil {
		t.Fatal("empty must fail")
	}
}

func TestParseMarginSymbols(t *testing.T) {
	body := []byte(`{"status":0,"data":[` +
		`{"symbol":"XRP","minOrderSize":"1","sizeStep":"1","tickSize":"0.001","takerFee":"0.0005","makerFee":"-0.0001"},` +
		`{"symbol":"XRP_JPY","minOrderSize":"10","sizeStep":"10","tickSize":"0.001","takerFee":"0","makerFee":"0"},` +
		`{"symbol":"BTC_JPY","minOrderSize":"0.001","sizeStep":"0.001","tickSize":"1","takerFee":"0","makerFee":"0"}]}`)
	rules, err := parseMarginSymbols(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("want only XXX_JPY rules, got %v", rules)
	}
	if rules["XRP_JPY"] != (gmoSizeRule{Min: 10, Step: 10}) {
		t.Fatalf("bad XRP_JPY rule: %+v", rules["XRP_JPY"])
	}
	if _, err := parseMarginSymbols([]byte(`{"status":1,"data":[]}`)); err == nil {
		t.Fatal("status!=0 must fail")
	}
}

func TestJstSixAMCuts(t *testing.T) {
	mk := func(h, m int, day int) int64 {
		return time.Date(2026, 9, day, h, m, 0, 0, time.UTC).UnixMilli()
	}
	// JST 05:00→07:00 は 6:00 を1回跨ぐ。同一時刻・逆転は 0。
	if got := jstSixAMCuts(mk(20, 0, 22), mk(22, 0, 22)); got != 1 {
		t.Fatalf("cross once = %d, want 1", got)
	}
	if got := jstSixAMCuts(mk(22, 0, 22), mk(22, 0, 22)); got != 0 {
		t.Fatalf("same time = %d, want 0", got)
	}
	// JST 07:00→翌05:00 は跨がない。
	if got := jstSixAMCuts(mk(22, 0, 22), mk(20, 0, 23)); got != 0 {
		t.Fatalf("no cross = %d, want 0", got)
	}
	if got := MarginFee(10000, mk(20, 0, 22), mk(22, 0, 22)); got != 4 {
		t.Fatalf("fee = %v, want 4", got)
	}
}

func marginMock(t *testing.T) (string, func()) {
	t.Helper()
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if serveGMOCommon(t, w, r) {
			return
		}
		switch r.URL.Path {
		case "/v1/account/margin":
			writeGMOJSON(t, w, map[string]any{"status": 0, "data": map[string]string{
				"actualProfitLoss": "0", "availableAmount": "5000", "margin": "0",
				"marginCallStatus": "NORMAL", "marginRatio": "0",
				"profitLoss": "0", "transferableAmount": "5000",
			}})
		case "/v1/order":
			writeGMOJSON(t, w, map[string]any{"status": 0, "data": "m1"})
		case "/v1/openPositions":
			if r.URL.Query().Get("symbol") != "XRP_JPY" {
				t.Errorf("unexpected positions query: %s", r.URL.RawQuery)
			}
			writeGMOJSON(t, w, map[string]any{"status": 0, "data": map[string]any{"list": []any{}}})
		case "/v1/closeOrder":
			writeGMOJSON(t, w, map[string]any{"status": 0, "data": "c1"})
		case "/v1/executions":
			writeGMOJSON(t, w, map[string]any{"status": 0, "data": map[string]any{
				"list": []map[string]string{{"price": "220", "size": "10"}},
			}})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	return srv.URL, srv.Close
}

func TestMarginMarketOrder(t *testing.T) {
	url, done := marginMock(t)
	defer done()
	e := newTestGMOExecutor(t, url)
	e.marginRules = map[string]gmoSizeRule{"XRP_JPY": {Min: 10, Step: 10}}
	// 余力 5000 に対して 10 枚×220 = 2200（1x 以内）→ filled。
	out, err := e.MarginMarketOrder(context.Background(), "XRP", domain.TradeSell, 10)
	if err != nil {
		t.Fatalf("MarginMarketOrder: %v", err)
	}
	if out.Status != "filled" || out.Quantity != 10 {
		t.Fatalf("unexpected result: %+v", out)
	}
	// 最小未満は拒否。
	if _, err := e.MarginMarketOrder(context.Background(), "XRP", domain.TradeSell, 5); err == nil {
		t.Fatal("below-min must fail")
	}
	// 証拠金シンボル無し（XLM）は拒否。
	if _, err := e.MarginMarketOrder(context.Background(), "XLM", domain.TradeSell, 10); err == nil {
		t.Fatal("no-margin-symbol must fail")
	}
	// 余力超過（1x 超）は拒否。
	if _, err := e.MarginMarketOrder(context.Background(), "XRP", domain.TradeBuy, 30); err == nil {
		t.Fatal("over-margin must fail (30*220=6600 > 5000)")
	}
}

func TestCloseMarginPosition(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/openPositions":
			writeGMOJSON(t, w, map[string]any{"status": 0, "data": map[string]any{
				"list": []map[string]any{{
					"positionId": 99, "symbol": "XRP_JPY", "side": "SELL", "size": "10",
					"orderdSize": "0", "price": "220", "lossGain": "0",
					"leverage": "2", "losscutPrice": "1",
					"timestamp": "2026-09-24T00:00:00.000Z",
				}},
			}})
		case "/v1/closeOrder":
			writeGMOJSON(t, w, map[string]any{"status": 0, "data": "c9"})
		case "/v1/executions":
			writeGMOJSON(t, w, map[string]any{"status": 0, "data": map[string]any{
				"list": []map[string]string{{"price": "219", "size": "10"}},
			}})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer srv.Close()
	e := newTestGMOExecutor(t, srv.URL)
	e.marginRules = map[string]gmoSizeRule{"XRP_JPY": {Min: 10, Step: 10}}
	out, err := e.CloseMarginPosition(context.Background(), "XRP", domain.TradeSell, 10)
	if err != nil {
		t.Fatalf("CloseMarginPosition: %v", err)
	}
	if out.OrderID != "c9" || out.Price != 219 {
		t.Fatalf("unexpected close result: %+v", out)
	}
	// 建玉なしは失敗する。
	srv2 := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeGMOJSON(t, w, map[string]any{"status": 0, "data": map[string]any{"list": []any{}}})
	})
	defer srv2.Close()
	e3 := newTestGMOExecutor(t, srv2.URL)
	e3.marginRules = map[string]gmoSizeRule{"XRP_JPY": {Min: 10, Step: 10}}
	if _, err := e3.CloseMarginPosition(context.Background(), "XRP", domain.TradeSell, 10); err == nil {
		t.Fatal("no position must fail")
	}
}

func TestCloseMarginPositionMulti(t *testing.T) {
	// 2026-09-26 の BCH 孤児化の回帰：1発注が複数 positionId に分割されても
	// 全量決済する。照会が毎回同じ一覧を返しても（stale）処理済み ID を
	// 飛ばして終了すること。
	closes := 0
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/openPositions":
			writeGMOJSON(t, w, map[string]any{"status": 0, "data": map[string]any{
				"list": []map[string]any{
					{"positionId": 1, "symbol": "XRP_JPY", "side": "SELL", "size": "10",
						"orderdSize": "0", "price": "220", "lossGain": "0",
						"leverage": "2", "losscutPrice": "1",
						"timestamp": "2026-09-24T00:00:00.000Z"},
					{"positionId": 2, "symbol": "XRP_JPY", "side": "SELL", "size": "10",
						"orderdSize": "0", "price": "221", "lossGain": "0",
						"leverage": "2", "losscutPrice": "1",
						"timestamp": "2026-09-24T00:00:00.000Z"},
					{"positionId": 3, "symbol": "XRP_JPY", "side": "SELL", "size": "10",
						"orderdSize": "0", "price": "222", "lossGain": "0",
						"leverage": "2", "losscutPrice": "1",
						"timestamp": "2026-09-24T00:00:00.000Z"},
				},
			}})
		case "/v1/closeOrder":
			closes++
			writeGMOJSON(t, w, map[string]any{"status": 0, "data": "c9"})
		case "/v1/executions":
			writeGMOJSON(t, w, map[string]any{"status": 0, "data": map[string]any{
				"list": []map[string]string{{"price": "219", "size": "10"}},
			}})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer srv.Close()
	e := newTestGMOExecutor(t, srv.URL)
	out, err := e.CloseMarginPosition(context.Background(), "XRP", domain.TradeSell, 30)
	if err != nil {
		t.Fatalf("multi close: %v", err)
	}
	if out.Quantity != 30 {
		t.Fatalf("must close all 30, got %v", out.Quantity)
	}
	if closes != 3 {
		t.Fatalf("must issue 3 close orders, got %d", closes)
	}
	// 部分数量は最初の玉だけ崩す。
	closes = 0
	out, err = e.CloseMarginPosition(context.Background(), "XRP", domain.TradeSell, 15)
	if err != nil {
		t.Fatalf("partial close: %v", err)
	}
	if out.Quantity != 15 {
		t.Fatalf("must close 15, got %v", out.Quantity)
	}
	if closes != 2 {
		t.Fatalf("partial must issue 2 close orders, got %d", closes)
	}
}

func TestMarginSizeFor(t *testing.T) {
	e := newTestGMOExecutor(t, "http://127.0.0.1:1")
	e.marginRules = map[string]gmoSizeRule{"XRP_JPY": {Min: 10, Step: 10}}
	// 余力5000×0.2=1000基準、XRP220・conf0.8 → 上限付きフォールバックで10枚。
	// 旧実装の余力いっぱい（20枚＝余力88%の hogging）は上限で抑える。
	qty, ok := e.MarginSizeFor("XRP", 220, 0.8, 5000, 0.2, 0, 2.0)
	if !ok || qty != 10 {
		t.Fatalf("capped fallback sizing: qty=%v ok=%v, want 10/true", qty, ok)
	}
	// 余力2500では基準に届かず最小数量（買える場合のみ）で10枚。
	qty, ok = e.MarginSizeFor("XRP", 220, 1.0, 2500, 0.2, 0, 2.0)
	if !ok || qty != 10 {
		t.Fatalf("clamped sizing: qty=%v ok=%v, want 10/true", qty, ok)
	}
	// 余力300では最小数量も買えず見送り。
	if _, ok := e.MarginSizeFor("XRP", 220, 1.0, 300, 0.2, 0, 2.0); ok {
		t.Fatal("unaffordable min must fail")
	}
	// ルール未登録は fail-closed。
	if _, ok := e.MarginSizeFor("XLM", 34, 0.8, 5000, 0.2, 0, 2.0); ok {
		t.Fatal("no-margin-symbol must fail")
	}
}

func TestMarginSizeForFallbackCap(t *testing.T) {
	e := newTestGMOExecutor(t, "http://127.0.0.1:1")
	e.marginRules = map[string]gmoSizeRule{"BCH_JPY": {Min: 0.1, Step: 0.1}}
	// 2026-09-26 の実績：余力24500・BCH53029・conf0.57・広スプレッドで
	// 通常経路が脱落し、旧実装は余力いっぱいの 0.4枚（約87%）を発注した。
	// 上限付きでは確信度連動の最小数量 0.1枚になる。
	qty, ok := e.MarginSizeFor("BCH", 53029.33, 0.57, 24500, 0.2, 0.01, 2.0)
	if !ok || qty != 0.1 {
		t.Fatalf("hogging must cap at 0.1: qty=%v ok=%v", qty, ok)
	}
	// 倍率0でフォールバック無効：低確信度で基準未満は見送る（現物と同等）。
	if _, ok := e.MarginSizeFor("BCH", 53029.33, 0.3, 24500, 0.2, 0.01, 0); ok {
		t.Fatal("disabled fallback must fail below minimum")
	}
	// 通常経路（基準で足りる）は倍率の影響を受けない。
	e.marginRules["XRP_JPY"] = gmoSizeRule{Min: 10, Step: 10}
	qty, ok = e.MarginSizeFor("XRP", 220, 0.8, 20000, 0.2, 0, 2.0)
	if !ok || qty != 20 {
		t.Fatalf("normal path must work: qty=%v ok=%v, want 20/true", qty, ok)
	}
}

func TestFormatSize(t *testing.T) { // float 誤差があっても刻み通りの表記になる。
	if got := formatSize(0.1+0.1, 0.1); got != "0.2" {
		t.Fatalf("got %q, want 0.2", got)
	}
	if got := formatSize(7*0.00001, 0.00001); got != "0.00007" {
		t.Fatalf("got %q, want 0.00007", got)
	}
	if got := formatSize(10.0, 1); got != "10" {
		t.Fatalf("got %q, want 10", got)
	}
	if got := formatSize(2.5, 0); got != "2.5" {
		t.Fatalf("unknown step keeps short form, got %q", got)
	}
	e := newTestGMOExecutor(t, "http://127.0.0.1:1")
	e.marginRules = map[string]gmoSizeRule{"XRP_JPY": {Min: 10, Step: 10}}
	if got := e.sizeStepFor("XRP_JPY"); got != 10 {
		t.Fatalf("margin step = %v, want 10", got)
	}
	if got := e.sizeStepFor("BTC"); got != 0.00001 {
		t.Fatalf("spot step = %v, want 0.00001", got)
	}
	if got := e.sizeStepFor("UNKNOWN"); got != 0 {
		t.Fatalf("unknown step = %v, want 0", got)
	}
}

type stubTicker struct {
	t   *domain.Ticker
	err error
}

func (s stubTicker) FetchTicker(symbol string) (*domain.Ticker, error) {
	return s.t, s.err
}

func TestSpotMid(t *testing.T) {
	e := newTestGMOExecutor(t, "http://127.0.0.1:1")
	e.mdClient = stubTicker{t: &domain.Ticker{Bid: 100, Ask: 102, Last: 101}}
	if got, err := e.SpotMid("XRP"); err != nil || got != 101 {
		t.Fatalf("mid = %v, %v; want 101", got, err)
	}
	e.mdClient = stubTicker{t: &domain.Ticker{Last: 55}}
	if got, err := e.SpotMid("XRP"); err != nil || got != 55 {
		t.Fatalf("last fallback = %v, %v; want 55", got, err)
	}
	e.mdClient = stubTicker{t: &domain.Ticker{}}
	if _, err := e.SpotMid("XRP"); err == nil {
		t.Fatal("empty ticker must fail")
	}
}
