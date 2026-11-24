package trading

import (
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/cryptoEngine/core/infrastructure/logging"
)

// RiskManager は自動売買の暴走を防ぐためのリスク管理。
//
// - AUTO_TRADE_ENABLED（デフォルト false）: 自動売買の ON/OFF
// - MAX_POSITION_SIZE_USDT（デフォルト 100）: 最大ポジションサイズを超える BUY は拒否
// - MAX_DAILY_LOSS_USDT（デフォルト 20）: 1 日の損失がこの値を超えたら自動停止
// - STOP_ON_LOSS（デフォルト true）: 自動停止時に AUTO_TRADE_ENABLED を false に自動切替
// - TRADE_SIZE_USDT（デフォルト 50）: 1 回の BUY で使用する USDT 想定額（数量の計算に使う）
//
// 1 日の損失は trade log CSV（今日分の SELL 確定損益）から起動時に復元し、
// プロセス内で累積する。日付が変わるとリセットする。
const (
	DefaultMaxPositionSizeUSDT = 100.0
	DefaultMaxDailyLossUSDT    = 20.0
	DefaultTradeSizeUSDT       = 50.0
	defaultStopOnLoss          = true
)

// RiskStatus は API / UI 向けのリスク管理スナップショット。
type RiskStatus struct {
	Enabled             bool    `json:"enabled"`
	Stopped             bool    `json:"stopped"` // 日次損失リミットによる停止中
	StopOnLoss          bool    `json:"stop_on_loss"`
	MaxPositionSizeUsdt float64 `json:"max_position_size_usdt"`
	MaxDailyLossUsdt    float64 `json:"max_daily_loss_usdt"`
	TradeSizeUsdt       float64 `json:"trade_size_usdt"`
	DailyLossUsdt       float64 `json:"daily_loss_usdt"`
	PositionValueUsdt   float64 `json:"position_value_usdt"`
}

type RiskManager struct {
	mu sync.Mutex

	enabled          bool
	stopped          bool
	stopOnLoss       bool
	maxPositionUsdt  float64
	tradeSizeUsdt    float64
	maxDailyLossUsdt float64

	dailyLossUsdt float64
	dailyDate     string // YYYY-MM-DD。日付が変わったら dailyLoss をリセット

	logger *logging.Logger
}

// NewRiskManager は環境変数からリスク管理設定を読み込んで生成する。
func NewRiskManager(logger *logging.Logger) *RiskManager {
	return &RiskManager{
		enabled:          envBool("AUTO_TRADE_ENABLED", false),
		stopped:          false,
		stopOnLoss:       envBool("STOP_ON_LOSS", defaultStopOnLoss),
		maxPositionUsdt:  envPositiveFloat("MAX_POSITION_SIZE_USDT", DefaultMaxPositionSizeUSDT),
		tradeSizeUsdt:    envPositiveFloat("TRADE_SIZE_USDT", DefaultTradeSizeUSDT),
		maxDailyLossUsdt: envPositiveFloat("MAX_DAILY_LOSS_USDT", DefaultMaxDailyLossUSDT),
		dailyLossUsdt:    0,
		dailyDate:        time.Now().Format("2006-01-02"),
		logger:           logger,
	}
}

// SetEnabled は自動売買の ON/OFF を設定する。OFF にすると risk 停止状態も解除される。
func (r *RiskManager) SetEnabled(b bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enabled = b
	if b {
		r.stopped = false
	}
}

// Enabled は自動売買が ON かどうかを返す。
func (r *RiskManager) Enabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.enabled
}

// Stopped は日次損失リミットで停止中かどうかを返す。
func (r *RiskManager) Stopped() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopped
}

// TradeSizeUsdt は 1 回の BUY 想定額（数量計算用）を返す。
func (r *RiskManager) TradeSizeUsdt() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tradeSizeUsdt
}

// MaxPositionUsdt は最大ポジションサイズを返す。
func (r *RiskManager) MaxPositionUsdt() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.maxPositionUsdt
}

// AllowBuy は現在ポジション評価額に BUY の想定額を加えた合計が
// 最大ポジションサイズを超える場合に false を返す（拒否）。
// 従来の TradeSize 固定版。Quantity 指定がある場合は AllowBuyWithAmount を使う。
func (r *RiskManager) AllowBuy(currentPositionValueUsdt float64) (bool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if currentPositionValueUsdt < 0 {
		currentPositionValueUsdt = 0
	}
	total := currentPositionValueUsdt + r.tradeSizeUsdt
	if total > r.maxPositionUsdt {
		return false, "BUY rejected: position size limit exceeded"
	}
	return true, ""
}

// AllowBuyWithAmount は実発注額面での上限チェック（auto-loop の qty 確定値用）。
// Quantity 指定の ExecuteOrder から呼ばれ、TradeSize 固定との乖離を無くす。
func (r *RiskManager) AllowBuyWithAmount(currentPositionValueUsdt, orderAmountUsdt float64) (bool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if currentPositionValueUsdt < 0 {
		currentPositionValueUsdt = 0
	}
	if orderAmountUsdt < 0 {
		orderAmountUsdt = 0
	}
	if currentPositionValueUsdt+orderAmountUsdt > r.maxPositionUsdt {
		return false, "BUY rejected: position size limit exceeded"
	}
	return true, ""
}

// AllowShort は SELL（ショート）の想定額を加えた合計が
// 最大ポジションサイズを超える場合に false を返す（拒否）。
// v2 PreTradeCheck から AllowBuy と対称に呼ばれる。新規追加であり既存挙動は不変。
func (r *RiskManager) AllowShort(currentPositionValueUsdt float64) (bool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if currentPositionValueUsdt < 0 {
		currentPositionValueUsdt = 0
	}
	total := currentPositionValueUsdt + r.tradeSizeUsdt
	if total > r.maxPositionUsdt {
		return false, "SELL rejected: position size limit exceeded"
	}
	return true, ""
}

// RecordTrade は約定後の確定損益（USDT）を記録する。loss が日次リミットを
// 超えた場合、STOP_ON_LOSS なら自動売買を OFF にし、停止フラグを立てる。
func (r *RiskManager) RecordTrade(pnlUsdt float64) (stopped bool, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if pnlUsdt < 0 {
		r.resetDailyIfNewDayLocked()
		r.dailyLossUsdt += -pnlUsdt
		if r.dailyLossUsdt >= r.maxDailyLossUsdt {
			r.stopped = true
			if r.stopOnLoss {
				r.enabled = false
			}
			return true, "auto trade stopped: daily loss limit reached"
		}
	}
	return false, ""
}

// resetDailyIfNewDayLocked は日付が変わっていたら日次損失をリセットする。
// r.mu を保持している前提。
func (r *RiskManager) resetDailyIfNewDayLocked() {
	today := time.Now().Format("2006-01-02")
	if today != r.dailyDate {
		r.dailyDate = today
		r.dailyLossUsdt = 0
		r.stopped = false
	}
}

// SeedDailyLossFromLog は trade log CSV から今日分の損失を復元する。
// プロセス起動後、最初の RecordTrade 前に呼ばれる。
func (r *RiskManager) SeedDailyLossFromLog(log *TradeLog) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resetDailyIfNewDayLocked()
	today := r.dailyDate

	entries, err := log.Read(1000)
	if err != nil {
		r.logger.Warn("seed daily loss from trade log failed: %v", err)
		return
	}
	var loss float64
	for _, e := range entries {
		if e.Action != "SELL" || e.Pnl == nil || *e.Pnl >= 0 {
			continue
		}
		ts, err := time.Parse(time.RFC3339, e.Timestamp)
		if err != nil {
			continue
		}
		if ts.Format("2006-01-02") == today {
			loss += -*e.Pnl
		}
	}
	if loss > 0 {
		r.dailyLossUsdt = loss
		if r.dailyLossUsdt >= r.maxDailyLossUsdt {
			r.stopped = true
			if r.stopOnLoss {
				r.enabled = false
			}
			r.logger.Warn("daily loss limit already reached on startup (loss=%.2f USDT); auto trade stopped", loss)
		}
	}
}

// Status は現在のリスク管理状態を返す（UI 表示用）。
func (r *RiskManager) Status(positionValueUsdt float64) *RiskStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resetDailyIfNewDayLocked()
	return &RiskStatus{
		Enabled:             r.enabled,
		Stopped:             r.stopped,
		StopOnLoss:          r.stopOnLoss,
		MaxPositionSizeUsdt: r.maxPositionUsdt,
		MaxDailyLossUsdt:    r.maxDailyLossUsdt,
		TradeSizeUsdt:       r.tradeSizeUsdt,
		DailyLossUsdt:       round2(r.dailyLossUsdt),
		PositionValueUsdt:   round2(positionValueUsdt),
	}
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

func envBool(key string, fallback bool) bool {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return v
}

func envPositiveFloat(key string, fallback float64) float64 {
	if raw := os.Getenv(key); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil && v > 0 {
			return v
		}
	}
	return fallback
}
