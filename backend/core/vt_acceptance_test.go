package main

import (
	"os"
	"path/filepath"
	"testing"
)

func floatNear(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}

func TestDryrunNetsFeeModes(t *testing.T) {
	// 入口 maker、価格100×10、出口 taker 価格102×10、グロス=+20。
	trades := []dryrunTrade{{symbol: "BCH", entry: 100, qty: 10, exit: 102, pnl: 20, maker: true}}
	taker, feeT := dryrunNets(trades, "taker")
	// cost = 0.0005*1000 + 0.0005*1020 = 1.01 → net 18.99
	if !floatNear(taker[0], 20-1.01) || !floatNear(feeT, 1.01) {
		t.Fatalf("taker: net=%v fees=%v", taker[0], feeT)
	}
	maker, feeM := dryrunNets(trades, "maker")
	// cost = -0.0001*1000 + 0.0005*1020 = 0.41 → net 19.59
	if !floatNear(maker[0], 20-0.41) || !floatNear(feeM, 0.41) {
		t.Fatalf("maker: net=%v fees=%v", maker[0], feeM)
	}
	perleg, feeP := dryrunNets(trades, "perleg")
	// maker入口 → 0.41
	if !floatNear(perleg[0], 20-0.41) || !floatNear(feeP, 0.41) {
		t.Fatalf("perleg(maker): net=%v fees=%v", perleg[0], feeP)
	}
	// 入口 taker の perleg は taker と同じ。
	trades[0].maker = false
	perleg2, _ := dryrunNets(trades, "perleg")
	if !floatNear(perleg2[0], 20-1.01) {
		t.Fatalf("perleg(taker): net=%v", perleg2[0])
	}
}

func TestDropShortOversold(t *testing.T) {
	recs := []vtSignalRecord{
		{Timestamp: 1000, Symbol: "BCH", Type: "sell", Price: 100, Qty: 1, Pattern: "s1", RSI: 30},
		{Timestamp: 2000, Symbol: "BCH", Type: "buy", Price: 99, Qty: 1, Pattern: "exit"},
		{Timestamp: 3000, Symbol: "BCH", Type: "sell", Price: 100, Qty: 1, Pattern: "s2", RSI: 50},
		{Timestamp: 4000, Symbol: "BCH", Type: "buy", Price: 101, Qty: 1, Pattern: "exit"},
		{Timestamp: 5000, Symbol: "BCH", Type: "buy", Price: 100, Qty: 1, Pattern: "b1", RSI: 20},
		{Timestamp: 6000, Symbol: "BCH", Type: "sell", Price: 102, Qty: 1, Pattern: "exit"},
	}
	out := dropShortOversold(recs, 40)
	if len(out) != 4 {
		t.Fatalf("want 4 records (s2+b1 pairs), got %d: %+v", len(out), out)
	}
	if out[0].Pattern != "s2" || out[2].Pattern != "b1" {
		t.Fatalf("wrong survivors: %+v", out)
	}
}

func TestDropPatterns(t *testing.T) {
	recs := []vtSignalRecord{
		{Timestamp: 1000, Symbol: "BCH", Type: "buy", Price: 100, Qty: 1, Pattern: "keep"},
		{Timestamp: 2000, Symbol: "BCH", Type: "sell", Price: 101, Qty: 1, Pattern: "exit"},
		{Timestamp: 3000, Symbol: "BCH", Type: "sell", Price: 100, Qty: 1, Pattern: "bad"},
		{Timestamp: 4000, Symbol: "BCH", Type: "buy", Price: 99, Qty: 1, Pattern: "exit"},
		{Timestamp: 5000, Symbol: "BCH", Type: "buy", Price: 100, Qty: 1, Pattern: "keep2"},
		{Timestamp: 6000, Symbol: "BCH", Type: "sell", Price: 102, Qty: 1, Pattern: "exit"},
	}
	out := dropPatterns(recs, "bad, KEEP2")
	if len(out) != 2 {
		t.Fatalf("want 2 records (keep pair only), got %d: %+v", len(out), out)
	}
	if out[0].Pattern != "keep" || out[1].Pattern != "exit" {
		t.Fatalf("wrong survivors: %+v", out)
	}
}

func TestPairNetPnlFeesAuto(t *testing.T) {
	// 買い(現物Maker)と売り(証拠金成行)を混在させ、買いだけrebateになることを検証する。
	recs := []vtSignalRecord{
		{Timestamp: 1000, Symbol: "BCH", Type: "buy", Price: 100, Qty: 10, Pattern: "x"},
		{Timestamp: 2000, Symbol: "BCH", Type: "sell", Price: 110, Qty: 10, Pattern: "exit", Reason: "tp"},
		{Timestamp: 3000, Symbol: "BCH", Type: "sell", Price: 100, Qty: 10, Pattern: "y"},
		{Timestamp: 4000, Symbol: "BCH", Type: "buy", Price: 90, Qty: 10, Pattern: "exit", Reason: "tp"},
	}
	nets, fees := pairNetPnlFeesAuto(recs, -0.0001, 0.0005)
	if len(nets) != 2 {
		t.Fatalf("want 2 nets, got %d", len(nets))
	}
	// 買い: 100 - (0.45) = 99.55、売り: 100 - (0.95) = 99.05
	if !floatNear(nets[0], 99.55) || !floatNear(nets[1], 99.05) {
		t.Fatalf("nets=%v", nets)
	}
	if !floatNear(fees, 1.4) {
		t.Fatalf("fees=%v, want 1.4", fees)
	}
}

func TestLoadDryrunTrades(t *testing.T) {
	dir := t.TempDir()
	body := `{"action":"open","symbol":"BCH","price":100,"qty":10,"maker":true,"ts":1000}
{"action":"open","symbol":"LINK","price":200,"qty":1,"ts":1001}
{"action":"open-maker","symbol":"LTC","price":50,"qty":2,"ts":1002}
{"action":"close","symbol":"BCH","price":102,"qty":10,"pnl":20,"reason":"tp","ts":2000}
{"action":"close","symbol":"LINK","price":198,"qty":1,"pnl":-2,"reason":"sl","ts":2001}
`
	if err := os.WriteFile(filepath.Join(dir, "vt_live_dryrun_20261018.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	trades := loadDryrunTrades(dir, 0)
	if len(trades) != 2 {
		t.Fatalf("want 2 trades, got %d", len(trades))
	}
	if trades[0].symbol != "BCH" || !trades[0].maker || !floatNear(trades[0].pnl, 20) {
		t.Fatalf("trade0=%+v", trades[0])
	}
	if trades[1].symbol != "LINK" || trades[1].maker {
		t.Fatalf("trade1=%+v", trades[1])
	}
}
