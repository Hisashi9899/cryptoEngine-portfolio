package trading

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// TradeLog は自動売買の注文履歴を CSV（backend/core/trade_logs/trade_log.csv）に
// 追記・読み戻す。列は timestamp, action, symbol, price, amount, pnl, reason,
// order_id, status。pnl は USDT（SELL の確定損益。BUY では空欄）。
//
// パスは環境変数 TRADE_LOG_PATH で上書きできる。未設定時は実行ディレクトリの
// trade_logs/trade_log.csv（コンテナでは docker-compose で /app/backend/core/... を指定）。
const (
	DefaultTradeLogPath = "trade_logs/trade_log.csv"
	// クローズ要因（reason）。リスク停止（risk）もここに記録する。
	ReasonSignal = "signal"
	ReasonTP     = "tp"
	ReasonSL     = "sl"
	ReasonRisk   = "risk"
)

// TradeLogEntry は trade log CSV の 1 行。
type TradeLogEntry struct {
	Timestamp string   `json:"timestamp"`
	Action    string   `json:"action"` // BUY / SELL
	Symbol    string   `json:"symbol"`
	Price     float64  `json:"price"`
	Amount    float64  `json:"amount"`
	Pnl       *float64 `json:"pnl"` // USDT。BUY では null
	Reason    string   `json:"reason"`
	OrderID   string   `json:"order_id"`
	Status    string   `json:"status"` // filled / error / rejected
}

type TradeLog struct {
	path string
	mu   sync.Mutex
}

// NewTradeLog は TRADE_LOG_PATH（未設定時は DefaultTradeLogPath）へ書き込む logger。
func NewTradeLog() *TradeLog {
	path := os.Getenv("TRADE_LOG_PATH")
	if path == "" {
		path = DefaultTradeLogPath
	}
	return &TradeLog{path: path}
}

// NewTradeLogTo は明示的にパスを指定して生成する（テスト用）。
func NewTradeLogTo(path string) *TradeLog {
	return &TradeLog{path: path}
}

// Path はログファイルのパスを返す。
func (l *TradeLog) Path() string { return l.path }

// Append は 1 行を追記する。ファイルが無ければヘッダ行を作成する。
func (l *TradeLog) Append(entry TradeLogEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("mkdir trade log dir: %w", err)
	}

	exists := false
	if _, err := os.Stat(l.path); err == nil {
		exists = true
	}

	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open trade log: %w", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if !exists {
		if err := w.Write([]string{
			"timestamp", "action", "symbol", "price", "amount", "pnl", "reason", "order_id", "status",
		}); err != nil {
			return err
		}
	}
	row := []string{
		entry.Timestamp,
		entry.Action,
		entry.Symbol,
		strconv.FormatFloat(entry.Price, 'f', 2, 64),
		strconv.FormatFloat(entry.Amount, 'f', 8, 64),
	}
	if entry.Pnl != nil {
		row = append(row, strconv.FormatFloat(*entry.Pnl, 'f', 6, 64))
	} else {
		row = append(row, "")
	}
	row = append(row, entry.Reason, entry.OrderID, entry.Status)

	if err := w.Write(row); err != nil {
		return err
	}
	w.Flush()
	return w.Error()
}

// Read はログを新しい順で最大 limit 行読み込む。
func (l *TradeLog) Read(limit int) ([]TradeLogEntry, error) {
	f, err := os.Open(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []TradeLogEntry{}, nil
		}
		return nil, fmt.Errorf("open trade log: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read trade log: %w", err)
	}
	if len(records) == 0 {
		return []TradeLogEntry{}, nil
	}

	// ヘッダ行はスキップ。
	rows := records[1:]
	entries := make([]TradeLogEntry, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		entry := TradeLogEntry{
			Timestamp: col(row, 0),
			Action:    col(row, 1),
			Symbol:    col(row, 2),
			Price:     parseFloat(col(row, 3)),
			Amount:    parseFloat(col(row, 4)),
			Reason:    col(row, 6),
			OrderID:   col(row, 7),
			Status:    col(row, 8),
		}
		if raw := col(row, 5); raw != "" {
			if v := parseFloat(raw); v != 0 || raw != "0" {
				pnl := v
				entry.Pnl = &pnl
			}
		}
		entries = append(entries, entry)
	}
	if len(entries) > limit && limit > 0 {
		entries = entries[:limit]
	}
	return entries, nil
}

func col(row []string, i int) string {
	if i < len(row) {
		return row[i]
	}
	return ""
}

func parseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
