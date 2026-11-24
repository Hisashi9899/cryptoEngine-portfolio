package capture

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cryptoEngine/core/infrastructure/exchange"
)

// tape.go は約定テープ（executions / aggTrades）の定期収集を提供する。
//
// 板スナップショットが「置かれた注文」なのに対し、テープは「誰が食ったか」
// （テイカー売買）で、CVD・吸収検知の素になる。公開 REST ポーリングで
// trade_tape_<SYM>_YYYYMMDD.csv（timestamp,price,qty,side）に追記する。
// 重複排除はキー集合で行う（GMO に約定IDが無いため）。

// TapeOptions はテープ収集のパラメータ。
type TapeOptions struct {
	Symbol     string
	Source     string // ""/binance または gmo
	PollEvery  time.Duration
	Duration   time.Duration
	Limit      int
	OutputPath string
	Append     bool
}

// tapeSeenCap は重複排除キーの保持上限。
const tapeSeenCap = 10000

// tapeKey は重複排除キーを作る。
func tapeKey(ts int64, price, qty float64, side string) string {
	return fmt.Sprintf("%d|%.8f|%.8f|%s", ts, price, qty, side)
}

// RunTape はテープを定期取得して CSV へ追記し、新規行数を返す。
func RunTape(opts TapeOptions, fetcher exchange.TradeTapeFetcher) (int, error) {
	if opts.Symbol == "" {
		return 0, fmt.Errorf("capture-tape: symbol is required")
	}
	if opts.PollEvery <= 0 {
		opts.PollEvery = 15 * time.Second
	}
	if opts.Duration <= 0 {
		return 0, fmt.Errorf("capture-tape: duration must be positive")
	}
	if opts.OutputPath == "" {
		return 0, fmt.Errorf("capture-tape: output path required")
	}
	if fetcher == nil {
		return 0, fmt.Errorf("capture-tape: fetcher is required")
	}
	if err := os.MkdirAll(filepath.Dir(opts.OutputPath), 0o755); err != nil {
		return 0, fmt.Errorf("capture-tape: mkdir: %w", err)
	}
	f, writeHeader, err := openOutput(opts.OutputPath, opts.Append)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if writeHeader {
		if _, err := f.WriteString("timestamp,price,qty,side\n"); err != nil {
			return 0, fmt.Errorf("capture-tape: write header: %w", err)
		}
	}

	seen := map[string]bool{}
	seenOrder := make([]string, 0, tapeSeenCap)
	remember := func(k string) bool {
		if seen[k] {
			return false
		}
		seen[k] = true
		seenOrder = append(seenOrder, k)
		if len(seenOrder) > tapeSeenCap {
			delete(seen, seenOrder[0])
			seenOrder = seenOrder[1:]
		}
		return true
	}

	rows := 0
	deadline := time.Now().Add(opts.Duration)
	ticker := time.NewTicker(opts.PollEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if time.Now().After(deadline) {
				return rows, nil
			}
			execs, err := fetcher.FetchExecutions(opts.Symbol, opts.Limit)
			if err != nil {
				continue
			}
			for _, e := range execs {
				if e.Timestamp <= 0 || e.Price <= 0 || e.Qty <= 0 {
					continue
				}
				if !remember(tapeKey(e.Timestamp, e.Price, e.Qty, string(e.Side))) {
					continue
				}
				line := fmt.Sprintf("%d,%v,%v,%s\n", e.Timestamp, e.Price, e.Qty, e.Side)
				if _, err := f.WriteString(line); err != nil {
					return rows, fmt.Errorf("capture-tape: write row: %w", err)
				}
				rows++
			}
		}
		if time.Now().After(deadline) {
			break
		}
	}
	return rows, nil
}
