package main

import (
	"math"
	"testing"
)

// TestSummarizeVtRecords は入口→出口の対付け・勝敗・内訳を検証する。
func TestSummarizeVtRecords(t *testing.T) {
	recs := []vtSignalRecord{
		{Timestamp: 1000, Symbol: "BTC", Type: "buy", Price: 100, Pattern: "morning_star", Regime: "trend_up", Qty: 2},
		{Timestamp: 2000, Symbol: "BTC", Type: "sell", Price: 110, Pattern: "exit", Reason: "signal", Qty: 2},
		{Timestamp: 3000, Symbol: "BTC", Type: "sell", Price: 110, Pattern: "evening_star", Regime: "trend_down", Qty: 1},
		{Timestamp: 4000, Symbol: "BTC", Type: "buy", Price: 112, Pattern: "exit", Reason: "stop_loss", Qty: 1},
		{Timestamp: 5000, Symbol: "BTC", Type: "buy", Price: 115, Pattern: "hammer_low", Regime: "trend_up", Qty: 1},
	}
	sum := summarizeVtRecords(recs, "signals", 7, 1, "none")
	if sum.Total.Pairs != 2 {
		t.Fatalf("pairs = %d, want 2", sum.Total.Pairs)
	}
	if sum.Total.Wins != 1 {
		t.Fatalf("wins = %d, want 1", sum.Total.Wins)
	}
	// buy 100→110 x2 = +20、sell 110→112 x1 = -2、合計 +18
	if sum.Total.PnL != 18 {
		t.Fatalf("pnl = %v, want 18", sum.Total.PnL)
	}
	if sum.Unpaired != 1 {
		t.Fatalf("unpaired = %d, want 1", sum.Unpaired)
	}
	if sum.ByPattern["morning_star"].Pairs != 1 || sum.ByPattern["morning_star"].Wins != 1 {
		t.Fatalf("morning_star stat wrong: %+v", sum.ByPattern["morning_star"])
	}
	if sum.ByExit["signal"].Pairs != 1 || sum.ByExit["stop_loss"].Pairs != 1 {
		t.Fatalf("exit stat wrong: %+v", sum.ByExit)
	}
	if sum.ByRegime["trend_up"].Pairs != 1 || sum.ByRegime["trend_down"].Pairs != 1 {
		t.Fatalf("regime stat wrong: %+v", sum.ByRegime)
	}
	// 出口先行・hold行は無視される。
	lone := []vtSignalRecord{
		{Timestamp: 100, Symbol: "X", Type: "buy", Price: 1, Pattern: "exit", Reason: "signal", Qty: 1},
		{Timestamp: 200, Symbol: "X", Type: "hold", Price: 1},
	}
	sum2 := summarizeVtRecords(lone, "signals", 7, 1, "none")
	if sum2.Total.Pairs != 0 || sum2.Rows != 2 {
		t.Fatalf("lone exit must not pair: %+v", sum2.Total)
	}
}

func TestSummarizeSeparatesBars(t *testing.T) {
	// 足種違いは誤対付けしない（1分と5分の記録が混在しても別系列）。
	recs := []vtSignalRecord{
		{Timestamp: 1000, Symbol: "BTC", Type: "buy", Price: 100, Pattern: "a", Regime: "trend_up", Qty: 1, BarsSec: 60},
		{Timestamp: 2000, Symbol: "BTC", Type: "sell", Price: 110, Pattern: "exit", Reason: "signal", Qty: 1, BarsSec: 300},
		{Timestamp: 3000, Symbol: "BTC", Type: "sell", Price: 110, Pattern: "exit", Reason: "signal", Qty: 1, BarsSec: 60},
	}
	sum := summarizeVtRecords(recs, "signals", 7, 1, "none")
	if sum.Total.Pairs != 1 {
		t.Fatalf("pairs = %d, want 1 (cross-bars must not pair)", sum.Total.Pairs)
	}
	if sum.ByBars["60s"].Pairs != 1 {
		t.Fatalf("60s stat wrong: %+v", sum.ByBars["60s"])
	}
	if _, ok := sum.ByBars["300s"]; ok {
		t.Fatalf("300s must have no pairs: %+v", sum.ByBars["300s"])
	}
}

// TestVtScoreFeeModes は auto（買い=maker/売り=taker）の手数料とside内訳を検証する。
func TestVtScoreFeeModes(t *testing.T) {
	recs := []vtSignalRecord{
		{Timestamp: 1000, Symbol: "BTC", Type: "buy", Price: 100, Pattern: "a", Regime: "trend_up", Qty: 10},
		{Timestamp: 2000, Symbol: "BTC", Type: "sell", Price: 110, Pattern: "exit", Reason: "signal", Qty: 10},
		{Timestamp: 3000, Symbol: "BTC", Type: "sell", Price: 100, Pattern: "b", Regime: "trend_down", Qty: 10},
		{Timestamp: 4000, Symbol: "BTC", Type: "buy", Price: 90, Pattern: "exit", Reason: "signal", Qty: 10},
	}
	gross := summarizeVtRecords(recs, "signals", 7, 1, "none")
	if math.Abs(gross.Total.PnL-200) > 1e-9 || gross.Fees != 0 {
		t.Fatalf("gross = %+v", gross.Total)
	}
	sum := summarizeVtRecords(recs, "signals", 7, 1, "auto")
	if sum.BySide["buy"].Pairs != 1 || sum.BySide["sell"].Pairs != 1 {
		t.Fatalf("by_side = %+v", sum.BySide)
	}
	if math.Abs(sum.Fees-1.4) > 1e-9 {
		t.Fatalf("fees = %v, want 1.4", sum.Fees)
	}
	if math.Abs(sum.Total.PnL-198.6) > 1e-9 {
		t.Fatalf("net = %v, want 198.6", sum.Total.PnL)
	}
}
