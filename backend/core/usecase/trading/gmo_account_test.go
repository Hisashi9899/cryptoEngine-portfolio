package trading

import (
	"context"
	"net/http"
	"testing"
)

// TestFetchMargin は証拠金照会のパースを検証する（読み取り専用・発注なし）。
func TestFetchMargin(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/account/margin" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		writeGMOJSON(t, w, map[string]any{"status": 0, "data": map[string]string{
			"actualProfitLoss": "0", "availableAmount": "4915", "margin": "0",
			"marginCallStatus": "NORMAL", "marginRatio": "0",
			"profitLoss": "0", "transferableAmount": "4915",
		}})
	})
	defer srv.Close()

	e := newTestGMOExecutor(t, srv.URL)
	st, err := e.FetchMargin(context.Background())
	if err != nil {
		t.Fatalf("FetchMargin: %v", err)
	}
	if st.AvailableAmount != "4915" || st.MarginCallStatus != "NORMAL" {
		t.Fatalf("unexpected margin: %+v", st)
	}
}

// TestFetchMarginRejected は業務エラーの即時失敗を検証する。
func TestFetchMarginRejected(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeGMOJSON(t, w, map[string]any{"status": 1,
			"messages": []map[string]string{{"message_code": "E", "message_string": "restricted"}}})
	})
	defer srv.Close()

	e := newTestGMOExecutor(t, srv.URL)
	if _, err := e.FetchMargin(context.Background()); err == nil {
		t.Fatal("expected error on status=1")
	}
}

// TestFetchOpenPositions は建玉照会のパースを検証する（読み取り専用）。
func TestFetchOpenPositions(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/openPositions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("symbol") != "BTC_JPY" {
			t.Errorf("unexpected symbol query: %s", r.URL.RawQuery)
		}
		writeGMOJSON(t, w, map[string]any{"status": 0, "data": map[string]any{
			"list": []map[string]any{{
				"positionId": 1234567, "symbol": "BTC_JPY", "side": "SELL", "size": "0.01",
				"orderdSize": "0", "price": "13500000", "lossGain": "100",
				"leverage": "2", "losscutPrice": "27000000",
				"timestamp": "2026-09-24T00:00:00.000Z",
			}},
		}})
	})
	defer srv.Close()

	e := newTestGMOExecutor(t, srv.URL)
	positions, err := e.FetchOpenPositions(context.Background(), "")
	if err != nil {
		t.Fatalf("FetchOpenPositions: %v", err)
	}
	if len(positions) != 1 || positions[0].Symbol != "BTC_JPY" || positions[0].Side != "SELL" {
		t.Fatalf("unexpected positions: %+v", positions)
	}
}

func TestFundsShortDust(t *testing.T) {
	// 2026-09-26 の SUI 決済不能の回帰：保有 14.6 に対し計算数量
	// 14.600000000000001（SizeForSignal の floor ダスト）で不足扱いに
	// なり、5秒毎の失敗スパム＋塩漬けになった。相対 1e-9 の微差は
	// 不足とみなさないこと。
	if fundsShort(14.6, 14.600000000000001) {
		t.Fatal("float dust must not count as shortfall")
	}
	if fundsShort(0.019, 0.019000000000000003) {
		t.Fatal("small-qty dust must not count as shortfall")
	}
	// 実不足は正しく検出すること（刻み1単位の欠落・空残高）。
	if !fundsShort(14.5, 14.6) {
		t.Fatal("real shortfall must be detected")
	}
	if !fundsShort(0, 0.04) {
		t.Fatal("zero balance must be detected")
	}
	if fundsShort(14.6, 14.6) {
		t.Fatal("exact balance must pass")
	}
	if fundsShort(100, 0) {
		t.Fatal("zero need must pass")
	}
}
