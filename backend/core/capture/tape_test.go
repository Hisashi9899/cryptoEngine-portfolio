package capture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cryptoEngine/core/domain"
	"github.com/cryptoEngine/core/infrastructure/exchange"
)

// fakeTapeFetcher は固定テープを返す。
type fakeTapeFetcher struct {
	execs []domain.Execution
	calls int
}

func (f *fakeTapeFetcher) FetchExecutions(symbol string, limit int) ([]domain.Execution, error) {
	f.calls++
	if symbol == "" {
		return nil, errTapeTest
	}
	return f.execs, nil
}

var errTapeTest = tapeTestErr{}

type tapeTestErr struct{}

func (tapeTestErr) Error() string { return "tape test error" }

var _ exchange.TradeTapeFetcher = (*fakeTapeFetcher)(nil)

func TestRunTape(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "trade_tape_BTC_20260921.csv")
	fetcher := &fakeTapeFetcher{execs: []domain.Execution{
		{Symbol: "BTC", Side: domain.TradeBuy, Price: 100, Qty: 0.5, Timestamp: 1700000000000},
		{Symbol: "BTC", Side: domain.TradeSell, Price: 101, Qty: 0.25, Timestamp: 1700000001000},
		{Symbol: "BTC", Side: domain.TradeBuy, Price: 0, Qty: 1, Timestamp: 1700000002000}, // 不正行
	}}
	rows, err := RunTape(TapeOptions{
		Symbol: "BTC", PollEvery: 10 * time.Millisecond, Duration: 50 * time.Millisecond,
		Limit: 100, OutputPath: out, Append: true,
	}, fetcher)
	if err != nil {
		t.Fatalf("RunTape: %v", err)
	}
	if rows != 2 {
		t.Fatalf("expected 2 rows (dup/invalid excluded), got %d", rows)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.HasPrefix(text, "timestamp,price,qty,side\n") {
		t.Fatalf("missing header: %q", text[:40])
	}
	if !strings.Contains(text, ",buy\n") || !strings.Contains(text, ",sell\n") {
		t.Fatalf("missing sides: %q", text)
	}
	if fetcher.calls == 0 {
		t.Fatal("fetcher never polled")
	}
}

func TestRunTapeRequires(t *testing.T) {
	fetcher := &fakeTapeFetcher{}
	if _, err := RunTape(TapeOptions{}, fetcher); err == nil {
		t.Fatal("expected error without symbol")
	}
	if _, err := RunTape(TapeOptions{Symbol: "BTC"}, nil); err == nil {
		t.Fatal("expected error without fetcher")
	}
}
