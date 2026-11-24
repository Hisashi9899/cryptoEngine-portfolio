package trading

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTradeLogAppendAndRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trade_log.csv")
	log := NewTradeLogTo(path)

	pnl := -0.55
	if err := log.Append(TradeLogEntry{
		Timestamp: "2026-08-06T09:00:00Z", Action: "SELL", Symbol: "BTCUSDT",
		Price: 55000, Amount: 0.001, Pnl: &pnl, Reason: ReasonSignal, OrderID: "100", Status: "filled",
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := log.Append(TradeLogEntry{
		Timestamp: "2026-08-06T09:01:00Z", Action: "BUY", Symbol: "BTCUSDT",
		Price: 55010, Amount: 0.001, Pnl: nil, Reason: ReasonSignal, OrderID: "101", Status: "filled",
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	entries, err := log.Read(10)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// 新しい順で返る。
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Action != "BUY" || entries[0].Amount != 0.001 {
		t.Errorf("first entry: expected BUY 0.001, got %s %.6f", entries[0].Action, entries[0].Amount)
	}
	if entries[1].Action != "SELL" || entries[1].Pnl == nil || *entries[1].Pnl != -0.55 {
		t.Errorf("second entry: expected SELL pnl=-0.55, got %s %v", entries[1].Action, entries[1].Pnl)
	}

	// limit を効かせる。
	limited, _ := log.Read(1)
	if len(limited) != 1 {
		t.Errorf("expected limit=1, got %d", len(limited))
	}
}

func TestTradeLogHeaderWrittenOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trade_log.csv")
	log := NewTradeLogTo(path)
	_ = log.Append(TradeLogEntry{Timestamp: "2026-08-06T09:00:00Z", Action: "BUY", Symbol: "BTCUSDT", Price: 1, Amount: 1, Status: "filled"})
	_ = log.Append(TradeLogEntry{Timestamp: "2026-08-06T09:01:00Z", Action: "SELL", Symbol: "BTCUSDT", Price: 1, Amount: 1, Status: "filled"})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	if lines != 3 {
		t.Errorf("expected header + 2 rows (3 lines), got %d lines", lines)
	}
}

func TestTradeLogReadMissingFile(t *testing.T) {
	log := NewTradeLogTo(filepath.Join(t.TempDir(), "nope.csv"))
	entries, err := log.Read(10)
	if err != nil {
		t.Fatalf("expected empty result for missing file, got err=%v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}
