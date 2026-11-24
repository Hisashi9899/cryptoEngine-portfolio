package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cryptoEngine/core/capture"
	"github.com/cryptoEngine/core/domain"
	"github.com/cryptoEngine/core/infrastructure/exchange"
	"github.com/cryptoEngine/core/infrastructure/logging"
	"github.com/cryptoEngine/core/infrastructure/repository"
	"github.com/cryptoEngine/core/usecase/market"
	"github.com/cryptoEngine/core/usecase/trading"
	vt "github.com/cryptoEngine/core/usecase/trading/vt"
)

const (
	// defaultExchangeURL は Binance 公開 REST のデフォルト。
	// EXCHANGE_BASE_URL 環境変数で上書きできる（VPS 等で地域制限がある場合は
	// https://data-api.binance.vision を使用）。
	defaultExchangeURL = "https://api.binance.com"
	defaultSymbol      = "BTCUSDT"
	defaultLimit       = 50
	defaultTimeout     = 10 * time.Second
)

type CLI struct {
	logger        *logging.Logger
	marketUC      *market.UseCase
	positionStore trading.PositionStore
	tradeLog      *trading.TradeLog
	signalRepo    repository.SignalRepository
	// vtEngines/vtOpen は V.Tシャドー評価用（執行なし）。
	// 人間が添削するための判定記録。テープは cron 側 vt-paper が担う。
	vtEngines map[string]*vt.Engine
	vtOpen    map[string]vt.OpenPos
}

func main() {
	logger := logging.NewLogger()
	exchangeURL := os.Getenv("EXCHANGE_BASE_URL")
	if exchangeURL == "" {
		exchangeURL = defaultExchangeURL
	}
	logger.Info("exchange client using base URL: %s", exchangeURL)
	var mdProvider exchange.MarketDataProvider
	if exchange.ExchangeID(strings.ToLower(os.Getenv("EXCHANGE_ID"))) == exchange.ExchangeGMO {
		mdProvider = exchange.NewGMOClient(defaultTimeout)
		logger.Info("market data provider: gmo")
	} else {
		mdProvider = exchange.NewClient(exchangeURL, defaultTimeout)
		logger.Info("market data provider: binance")
	}
	marketUC := market.NewUseCase(mdProvider, logger)

	// DB は必要なコマンド（execute-order）でのみ接続する。
	// gmo-assets / fetch-* / capture / vt-paper / vt-score 等の DB 不要コマンドは
	// DB 切断時も動作する（execute-order は positionStore nil 時に専用エラーを返す）。
	command := ""
	if len(os.Args) >= 2 {
		command = os.Args[1]
	}
	needsDB := command == "execute-order"
	var signalRepo repository.SignalRepository
	var positionStore trading.PositionStore
	if needsDB {
		if dbURL := os.Getenv("DB_URL"); dbURL != "" {
			repo, err := repository.New(context.Background(), dbURL)
			if err != nil {
				logger.Error("failed to connect to database: %v", err)
				os.Exit(1)
			}
			defer repo.Close()
			signalRepo = repo.Signals
			positionStore = repo.Positions
			logger.Info("database connection established")
		}
	}

	cli := &CLI{
		logger:        logger,
		marketUC:      marketUC,
		positionStore: positionStore,
		tradeLog:      trading.NewTradeLog(),
		signalRepo:    signalRepo,
	}

	if err := cli.run(os.Args); err != nil {
		logger.Error("%v", err)
		os.Exit(1)
	}
}

func (cli *CLI) run(args []string) error {
	if len(args) < 2 {
		return printUsage()
	}

	command := args[1]

	switch command {
	case "fetch-orderbook":
		return cli.fetchOrderBook(args[2:])
	case "fetch-ticker":
		return cli.fetchTicker(args[2:])
	case "capture-local":
		return cli.captureLocal(args[2:])
	case "capture-daily":
		return cli.captureDaily(args[2:])
	case "capture-tape":
		return cli.captureTape(args[2:])
	case "vt-paper":
		return cli.vtPaper(args[2:])
	case "vt-score":
		return cli.vtScore(args[2:])
	case "vt-pattern-allow":
		return cli.vtPatternAllow(args[2:])
	case "vt-pick":
		return cli.vtPick(args[2:])
	case "vt-live":
		return cli.vtLive(args[2:])
	case "vt-portfolio":
		return cli.vtPortfolio(args[2:])
	case "vt-replay":
		return cli.vtReplay(args[2:])
	case "vt-why":
		return cli.vtWhy(args[2:])
	case "vt-acceptance":
		return cli.vtAcceptance(args[2:])
	case "vt-tune":
		return cli.vtTune(args[2:])
	case "vt-weekly-report":
		return cli.vtWeeklyReport(args[2:])
	case "execute-order":
		return cli.executeOrder(args[2:])
	case "gmo-assets":
		return cli.gmoAssets(args[2:])
	case "gmo-active-orders":
		return cli.gmoActiveOrders(args[2:])
	case "gmo-affordability":
		return cli.gmoAffordability(args[2:])
	case "gmo-margin":
		return cli.gmoMargin(args[2:])
	case "gmo-positions":
		return cli.gmoPositions(args[2:])
	case "close-margin":
		return cli.closeMargin(args[2:])
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
}

func printUsage() error {
	usage := `Usage: core <command> [options]

Commands:
  fetch-orderbook [symbol] [limit]   Fetch order book data
  fetch-ticker    [symbol]           Fetch ticker data
  capture-local   <out.csv> [opts]   Capture Binance WS orderbook for backtest
  capture-daily   [opts]             Append-capture to ORDERBOOK_LOG_DIR/orderbook_<SYM>_YYYYMMDD.csv
  capture-tape    [opts]             Append-capture trade tape to trade_tape_<SYM>_YYYYMMDD.csv
  vt-paper        [opts]             V.T engine recording (vt_signals jsonl, no orders)
  vt-score        [opts]             Score vt_signals/vt_shadow jsonl into win-rate/PnL summary
  vt-portfolio    [opts]             Replay paper records to compare max concurrent positions
  vt-replay         [opts]             Replay saved board CSVs through the V.T engine (no orders)
  execute-order   <symbol> <side>    Execute a market order (auto trade)
  gmo-assets                         Fetch GMO balances (read-only key check, no orders)
  gmo-margin                         Fetch GMO margin status (read-only leverage check, no orders)
  gmo-positions [--symbol=BTC_JPY]    Fetch GMO open margin positions (read-only, no orders)
  close-margin <symbol> [--quantity] Close GMO margin positions (ops recovery, places orders)
  gmo-active-orders [--symbol=X]      Fetch GMO open orders (read-only trade-permission check)
  gmo-affordability                  Show per-symbol tradability vs JPY balance (no orders)

  (v1/v2/v3 commands retired and removed 2026-09-26. V.T only.)

Options:
  symbol    Trading pair symbol (default: BTCUSDT)
  limit     Order book depth (default: 50)

Capture-local options:
  --seconds=1800             Capture duration in seconds (default: LOCAL_CAPTURE_MINUTES*60, clamped 10..60 min)
  --symbol=btcusdt           Binance stream symbol (lowercase)
  --levels=5                 Depth stream levels
  --interval=1.0             Sample interval in seconds

Capture-daily options:
  ORDERBOOK_LOG_DIR          Output dir for orderbook_<SYM>_YYYYMMDD.csv (default: trade_logs)
  --seconds=N                Duration in seconds (0 = run until SIGINT/SIGTERM)

  vt-paper options (V.T engine recording only, no orders):
  --symbol=BTC               GMO spot symbol (default BTC)
  --bars=300                 Entry bars seconds: 60, 300, 900 or 3600 (default 300)
  --trend-tf=3600            Trend timeframe seconds for direction gate (default 3600;
                             0 or <=bars disables = entry bars judge themselves)
  --ma-fast=5 --ma-slow=20   MA periods for trend gate and regime
  --bb-period=20 --bb-k=2.0  Bollinger period and sigma
  --rsi-period=14            RSI period
  --macd-fast=12 --macd-slow=26 --macd-sig=9  MACD parameters
  --tp-ratio=0 --sl-ratio=0  Exit ratios vs entry (0 = off, opposite-signal exit only)
  --max-hold-sec=0           Max hold seconds (0 = off)
  --fast-tf=60               Lower timeframe seconds for entry confirmation (0 = off)
  --trail-activate=0.015     Trailing activation profit ratio (0 = off)
  --trail-back=0.008         Trailing pullback ratio from peak (0 = off)
  --short-patterns=...       Comma list to restrict short entries (empty = all; default: 7 winners)
  --min-confidence=0         Entry confidence floor (0 = off)
  --short-rsi-floor=0        Skip short entries below this RSI (0 = off)
  --squeeze-gate=true        Halt non-breakout entries while coiled (false/0 disables)
  --interval=5               Sample interval seconds (default 5)
  --balance-jpy=100000       Reference balance for sizing (default 100000)
  --risk-fraction=0.2        Risk fraction for sizing (default 0.2)
  --seconds=N                Duration in seconds (0 = run until SIGINT/SIGTERM)

  vt-score options (V.T recording scorer, read-only):
  --dir=trade_logs           vt_signals_*/vt_shadow_* search dir (default ORDERBOOK_LOG_DIR)
  --days=7                   Look back N days (default 7, 0 = all)
  --out=                     Write JSON summary to path (default: stdout only)

  vt-pattern-allow options (entry-pattern auto-tuner, read-only):
  --dir=trade_logs           vt_signals search dir (default ORDERBOOK_LOG_DIR)
  --days=14                  Look back N days (default 14, 0 = all)
  --bars=300                 Entry bars filter (default 300, 0 = all)
  --min-pairs=5              Minimum pairs to block a losing pattern (default 5)
  --out=                     Write deny JSON to path (default: stdout only)

  vt-pick options (multi-symbol priority ranker, read-only):
  --dir=trade_logs           vt_signals search dir (default ORDERBOOK_LOG_DIR)
  --bars=300                 Entry bars filter (default 300, 0 = all)
  --within-min=15            Signal freshness window minutes (default 15)
  --symbols=BTC,XRP          Limit symbols (default: all)
  --balance-jpy=100000       Reference balance for sizing (default 100000)
  --risk-fraction=0.2        Risk fraction for sizing (default 0.2)
  --fee-bps=5                One-way fee bps for efficiency (default 5)

  vt-live options (V.T live execution, GMO spot market):  --symbols=BTC,XRP          Symbols (default: BTC,XRP,XLM,BCH,LTC)
  --bars=300                 Single entry bars (default 300)
  --trend-tf=3600            Trend timeframe seconds (default 3600)
  --balance-jpy=100000       Reference balance for sizing (default 100000)
  --risk-fraction=0.2        Risk fraction for sizing (default 0.2)
  --max-positions=1          Max concurrent opens (default 1)
  --max-daily-loss-jpy=2000  New-entry stop on daily loss (default 2000)
  --pick-window-sec=20       Candidate gather window seconds (default 20)
  --fee-bps=5                Fee bps for efficiency rank (default 5)
  --fast-tf=60               Lower timeframe seconds for entry confirmation (0 = off)
  --trail-activate=0.015     Trailing activation profit ratio (0 = off)
  --trail-back=0.008         Trailing pullback ratio from peak (0 = off)
  --tp-ratio=0               Take-profit ratio (0 = off)
  --sl-ratio=0.02            Stop-loss ratio (0 = off)
  --max-hold-sec=43200       Time-stop seconds, exit stale opens (0 = off, default 12h)
  --resume-max-age-sec=86400 Resume opens newer than N seconds (default 24h)
  --maintenance-exit-min=30  Flatten opens + halt entries N min before Sat 09:00 JST maintenance (0 = off)
  --pattern-allow-file=      Weekly deny JSON from vt-pattern-allow (default: none = defaults)
  --squeeze-gate=true        Halt non-breakout entries while coiled (false/0 disables)
  --short-patterns=...       Comma list to restrict short entries (empty = all; default: 7 winners)
  --trail-activate-atr=0     Trail activation ATR multiple (0 = off)
  --trail-back-atr=0         Trail pullback ATR multiple (0 = off)
  --sl-atr=0                 Stop-loss ATR multiple (0 = off)
  --imbalance-min-hold-sec=0 Board-exit minimum hold seconds (0 = off)
  --exit-confirm-votes=0   Confirm votes for weak exits rsi_weak/imbalance/ma_break (0 = off)
  --margin-fallback-mult=2.0 Margin fallback cap multiple of base (<=0 disables fallback)
  --min-confidence=0         Entry confidence floor (0 = off)
  --short-rsi-floor=0        Skip short entries below this RSI (0 = off)
  --slip-alert-bps=50        Warn when fill slips this many bps vs signal (log only)
  --block-symbols=SUI,WILD   Live-excluded symbols, comma-separated (default: none)
  --block-days=7             Paper lookback days for loser exclusion (default 7)
  --block-min-pairs=5        Minimum paper pairs to block a losing symbol (default 5)
  --allow-symbols=LTC        Allowlist that beats auto loser-block (default: none)
  --max-daily-loss-ratio=0   Daily loss limit as fraction of balance (0 = use fixed JPY)
  --balance-refresh-sec=300  Re-read balance every N seconds in live (0 = off, min 60)
  --exit-sweep-sec=60        Re-evaluate open exits via REST when streams stall (0 = off)
  --tick-stale-sec=120       Tick staleness threshold seconds for the sweep
  --margin                   Enable margin Sell entries (1x via XXX_JPY); Buy stays spot

  vt-portfolio options (replay paper records to compare max positions):
  --dir=trade_logs           vt_signals search dir (default ORDERBOOK_LOG_DIR)
  --days=14                  Look back N days (default 14, 0 = all)
  --bars=300                 Entry bars filter (default 300)
  --max-daily=500            Daily loss halt in replay (default 500)
  --fee-rate=0.0005          One-way fee rate deducted per round trip (default 0.0005)
  --max-list=1,2,3,4,5,6     Max positions to compare (default 1,2,3,4,5,6)
  --exclude=SUI,WILD         Symbols to exclude from replay (default: none)

  vt-replay options (historical replay, read-only):
  --symbol=BTC               GMO spot symbol (default BTC)
  --bars=300                 Single entry bars: 60, 300, 900 or 3600 (default 300)
  --from=20260901 --to=20260923  Date range YYYYMMDD (default: last 30 days)
  --trend-tf=3600            Trend timeframe seconds (default 3600)
  --fast-tf=60               Lower timeframe seconds (default 60, 0 = off)
  --trail-activate=0.015     Trailing activation profit ratio (default 0.015)
  --trail-back=0.008         Trailing pullback ratio (default 0.008)
  --min-confidence=0         Entry confidence floor (0 = off, for A/B validation)
  --short-rsi-floor=0        Skip short entries below this RSI (0 = off)
  --balance-jpy=100000       Reference balance for sizing (default 100000)
  --risk-fraction=0.2        Risk fraction for sizing (default 0.2)
  --out=                     Write JSONL records to path (default: summary only)
  --interval=5               Board poll seconds (default 5)
  --seconds=0                Duration (0 = daemon until signal)
  --live                     Place real orders (absent = dry-run intent log only)

Execute-order options:
  --price=12345.67           Reference price (signal mid price) for qty calc
  --reason=signal            Close reason: signal | tp | sl | risk
  --auto-trade=true          Override AUTO_TRADE_ENABLED for this run (API がランタイム状態を渡す)
  --order-type=limit         market (default) or limit (GMO only for now)
  --limit-price=12345.67     Limit price (required for limit)
  --time-in-force=FAK        FAK (default) | FAS | FOK (limit only)

Examples:
  core fetch-orderbook BTCUSDT 10
  core fetch-ticker ETHUSDT
  core capture-local tmp_local_capture.csv --seconds=600
  core vt-paper --symbol=BTC --bars=300 --seconds=420
  core vt-score --days=7
  core vt-pick --bars=300 --within-min=15
  core vt-live --symbols=BTC,XRP --bars=300 --seconds=600
  core vt-why --symbols=BTC,XRP --bars=60,300,3600
  core execute-order BTCUSDT buy --price=50000 --reason=signal --auto-trade=true
`
	fmt.Fprint(os.Stderr, usage)
	return nil
}

func (cli *CLI) fetchOrderBook(args []string) error {
	symbol := defaultSymbol
	limit := defaultLimit

	if len(args) >= 1 && args[0] != "" {
		symbol = args[0]
	}
	if len(args) >= 2 {
		fmt.Sscanf(args[1], "%d", &limit)
	}

	result, err := cli.marketUC.FetchOrderBook(market.FetchOrderBookInput{
		Symbol: symbol,
		Limit:  limit,
	})
	if err != nil {
		return fmt.Errorf("fetch-orderbook: %w", err)
	}

	return printJSON(result)
}

func (cli *CLI) fetchTicker(args []string) error {
	symbol := defaultSymbol
	if len(args) >= 1 && args[0] != "" {
		symbol = args[0]
	}

	result, err := cli.marketUC.FetchTicker(market.FetchTickerInput{Symbol: symbol})
	if err != nil {
		return fmt.Errorf("fetch-ticker: %w", err)
	}

	return printJSON(result)
}

func printJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("json marshal: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

// captureLocal は Binance WS から短期間オーダーブックをキャプチャし、バックテスト用
// CSV を出力する。例: core capture-local tmp_local_capture.csv --seconds=600
func (cli *CLI) captureLocal(args []string) error {
	if len(args) < 1 || args[0] == "" {
		return fmt.Errorf("capture-local requires an output CSV path")
	}
	kv := parseKeyValues(args[1:])
	seconds := parseFloatKV(kv, "seconds", 0)
	if seconds <= 0 {
		seconds = envCaptureSeconds()
	}
	opts := capture.Options{
		Symbol:      kv["symbol"],
		Levels:      int(parseFloatKV(kv, "levels", capture.DefaultLevels)),
		Duration:    time.Duration(seconds) * time.Second,
		SampleEvery: time.Duration(parseFloatKV(kv, "interval", 1.0) * float64(time.Second)),
		OutputPath:  args[0],
		Source:      captureSource(kv),
	}
	rows, err := capture.Run(opts)
	if err != nil {
		return fmt.Errorf("capture-local: %w", err)
	}
	return printJSON(map[string]any{
		"status":  "ok",
		"rows":    rows,
		"output":  args[0],
		"seconds": int(seconds),
	})
}

// captureSource は --exchange フラグまたは EXCHANGE_ID 環境変数から
// capture ソース（""=binance 既定 / "gmo"）を解決する。
func captureSource(kv map[string]string) string {
	if v, ok := kv["exchange"]; ok && v != "" {
		return strings.ToLower(v)
	}
	if v := os.Getenv("EXCHANGE_ID"); v != "" {
		return strings.ToLower(v)
	}
	return ""
}

// orderbookLogDir は ORDERBOOK_LOG_DIR（既定 "trade_logs"）を返す。
// capture-daily と paper の出力先（orderbook_YYYYMMDD.csv / signals_v2_*.jsonl /
// calibration_*.json）を一元化する。
func orderbookLogDir() string {
	if d := os.Getenv("ORDERBOOK_LOG_DIR"); d != "" {
		return d
	}
	return "trade_logs"
}

// captureDaily は ORDERBOOK_LOG_DIR/orderbook_YYYYMMDD.csv へ追記キャプチャする。
// 例: ORDERBOOK_LOG_DIR=/app/backend/core/trade_logs core capture-daily --seconds=600
//
// capture-local（truncate）と異なり追記モードのため、cron 等の短間隔実行で
// 日次ファイルを育てる用途に向く。既存ファイルが空でなければヘッダは書かない。
func (cli *CLI) captureDaily(args []string) error {
	dir := orderbookLogDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("capture-daily: mkdir: %w", err)
	}
	kv := parseKeyValues(args)
	// 銘柄別ファイル（複数銘柄の並行収集用）。symbol 未指定時は従来名。
	fname := "orderbook_" + time.Now().Format("20060102") + ".csv"
	if sym := strings.ToUpper(strings.TrimSpace(kv["symbol"])); sym != "" {
		fname = "orderbook_" + sym + "_" + time.Now().Format("20060102") + ".csv"
	}
	path := filepath.Join(dir, fname)
	seconds := parseFloatKV(kv, "seconds", 0)
	if seconds <= 0 {
		seconds = envCaptureSeconds()
	}
	opts := capture.Options{
		Symbol:      kv["symbol"],
		Levels:      int(parseFloatKV(kv, "levels", capture.DefaultLevels)),
		Duration:    time.Duration(seconds) * time.Second,
		SampleEvery: time.Duration(parseFloatKV(kv, "interval", 1.0) * float64(time.Second)),
		OutputPath:  path,
		Append:      true,
		Source:      captureSource(kv),
	}
	rows, err := capture.Run(opts)
	if err != nil {
		return fmt.Errorf("capture-daily: %w", err)
	}
	return printJSON(map[string]any{
		"status":  "ok",
		"rows":    rows,
		"output":  path,
		"seconds": int(seconds),
	})
}

// captureTape は約定テープを trade_tape_<SYM>_YYYYMMDD.csv へ追記収集する。
// 例: core capture-tape --symbol=BTC --seconds=600
// vt-paper が同時記録するため通常は不要。単独収集・検証用。
func (cli *CLI) captureTape(args []string) error {
	dir := orderbookLogDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("capture-tape: mkdir: %w", err)
	}
	kv := parseKeyValues(args)
	sym := strings.ToUpper(strings.TrimSpace(kv["symbol"]))
	if sym == "" {
		return fmt.Errorf("capture-tape requires --symbol=")
	}
	fname := "trade_tape_" + sym + "_" + time.Now().Format("20060102") + ".csv"
	path := filepath.Join(dir, fname)
	seconds := parseFloatKV(kv, "seconds", 0)
	if seconds <= 0 {
		seconds = envCaptureSeconds()
	}
	var fetcher exchange.TradeTapeFetcher
	if exchange.ExchangeID(strings.ToLower(captureSource(kv))) == exchange.ExchangeGMO ||
		strings.EqualFold(strings.TrimSpace(captureSource(kv)), "gmo") {
		fetcher = exchange.NewGMOClient(defaultTimeout)
	} else {
		base := os.Getenv("EXCHANGE_BASE_URL")
		if base == "" {
			base = defaultExchangeURL
		}
		fetcher = exchange.NewClient(base, defaultTimeout)
	}
	rows, err := capture.RunTape(capture.TapeOptions{
		Symbol:     sym,
		Source:     captureSource(kv),
		PollEvery:  time.Duration(parseFloatKV(kv, "interval", 15.0) * float64(time.Second)),
		Duration:   time.Duration(seconds) * time.Second,
		Limit:      int(parseFloatKV(kv, "limit", 100)),
		OutputPath: path,
		Append:     true,
	}, fetcher)
	if err != nil {
		return fmt.Errorf("capture-tape: %w", err)
	}
	return printJSON(map[string]any{
		"status": "ok", "rows": rows, "output": path, "seconds": int(seconds),
	})
}

// --- v1/v2/v3 系コマンドは破棄・削除済み（2026-09-26）。
// 検証は V.T の vt-paper / vt-score を使う。 ---

// executeOrder は自動売買の成行注文を実行する（シグナル → 注文）。
// 例: core execute-order BTCUSDT buy --price=50000 --reason=signal --auto-trade=true
//
// 自動売買の ON/OFF は --auto-trade フラグ（API がランタイム状態を渡す）で
// 上書きできる。フラグが無い場合は環境変数 AUTO_TRADE_ENABLED をデフォルトとして使う。
// ポジション管理に DB が必要なため、DB 未接続時はエラーを返す。
func (cli *CLI) executeOrder(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("execute-order requires <symbol> <side>")
	}
	if cli.positionStore == nil {
		return fmt.Errorf("execute-order requires a database connection (DB_URL)")
	}
	symbol := args[0]
	side := domain.TradeSide(strings.ToLower(args[1]))
	kv := parseKeyValues(args[2:])

	price := parseFloatKV(kv, "price", 0)
	reason := kv["reason"]
	if reason == "" {
		reason = "signal"
	}
	orderType := strings.ToLower(kv["order-type"])
	limitPrice := parseFloatKV(kv, "limit-price", 0)
	timeInForce := strings.ToUpper(kv["time-in-force"])

	risk := trading.NewRiskManager(cli.logger)
	if raw, ok := kv["auto-trade"]; ok && raw != "" {
		if v, err := strconv.ParseBool(raw); err == nil {
			risk.SetEnabled(v)
		}
	}

	var executor trading.OrderExecutor
	if exchange.ExchangeID(strings.ToLower(os.Getenv("EXCHANGE_ID"))) == exchange.ExchangeGMO {
		executor = trading.NewGMOOrderExecutor(cli.logger)
		cli.logger.Info("order executor: gmo")
	} else {
		executor = trading.NewBinanceOrderExecutor(cli.logger)
		cli.logger.Info("order executor: binance")
	}
	atUC := trading.NewAutoTradeUseCase(executor, cli.positionStore, risk, cli.tradeLog, cli.logger)

	out, err := atUC.ExecuteOrder(context.Background(), trading.ExecuteOrderInput{
		Symbol:      symbol,
		Side:        side,
		Price:       price,
		Reason:      reason,
		OrderType:   orderType,
		LimitPrice:  limitPrice,
		TimeInForce: timeInForce,
	})
	if err != nil {
		return fmt.Errorf("execute-order: %w", err)
	}
	return printJSON(out)
}

// gmoAssets は GMO 残高を読み取り専用で取得する。発注しないため、API キーの
// 疎通確認に使う。例: EXCHANGE_ID=gmo core gmo-assets
func (cli *CLI) gmoAssets(args []string) error {
	_ = args
	executor := trading.NewGMOOrderExecutor(cli.logger)
	assets, err := executor.FetchAssets(context.Background())
	if err != nil {
		return fmt.Errorf("gmo-assets: %w", err)
	}
	return printJSON(map[string]any{"status": "ok", "assets": assets})
}

// gmoMargin は証拠金状況を読み取り専用で取得する。発注しないため、
// レバレッジ口座の疎通確認に使う。例: core gmo-margin
func (cli *CLI) gmoMargin(args []string) error {
	_ = args
	executor := trading.NewGMOOrderExecutor(cli.logger)
	st, err := executor.FetchMargin(context.Background())
	if err != nil {
		return fmt.Errorf("gmo-margin: %w", err)
	}
	return printJSON(map[string]any{"status": "ok", "margin": st})
}

// pickCloseSide は建玉一覧から決済対象 side・数量・建値VWAPを決める
// （純粋関数・テスト対象）。side 空文字は自動判定（両建て時は明示を要求）。
// quantity<=0 はその side の全量。建玉なしはエラー。
func pickCloseSide(positions []trading.GMOOpenPosition, want string, qty float64) (domain.TradeSide, float64, float64, error) {
	type agg struct {
		qty float64
		val float64
	}
	sums := map[string]*agg{"BUY": {}, "SELL": {}}
	for _, p := range positions {
		side := strings.ToUpper(strings.TrimSpace(p.Side))
		a := sums[side]
		if a == nil {
			continue
		}
		var sz, px float64
		fmt.Sscanf(strings.TrimSpace(p.Size), "%f", &sz)
		fmt.Sscanf(strings.TrimSpace(p.Price), "%f", &px)
		if sz <= 0 || px <= 0 {
			continue
		}
		a.qty += sz
		a.val += sz * px
	}
	side := strings.ToUpper(strings.TrimSpace(want))
	if side != "BUY" && side != "SELL" {
		if sums["SELL"].qty > 0 && sums["BUY"].qty > 0 {
			return "", 0, 0, fmt.Errorf("both sides open; specify --side=buy|sell")
		}
		if sums["SELL"].qty > 0 {
			side = "SELL"
		} else if sums["BUY"].qty > 0 {
			side = "BUY"
		} else {
			return "", 0, 0, fmt.Errorf("no open margin position to close")
		}
	}
	a := sums[side]
	if a.qty <= 0 {
		return "", 0, 0, fmt.Errorf("no open %s position to close", side)
	}
	if qty <= 0 {
		qty = a.qty
	}
	if qty > a.qty {
		qty = a.qty
	}
	return domain.TradeSide(strings.ToLower(side)), qty, a.val / a.qty, nil
}

// closeMargin は証拠金建玉の決済（ops 用の手動 close）。
// 例: core close-margin BCH --quantity=0.6 --reason=orphan-cleanup
// 数量省略時はその side の全量。BUY/SELL は建玉から自動判定する
// （--side で明示も可）。約定は trade_log.csv に残す。
func (cli *CLI) closeMargin(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("close-margin requires <symbol>")
	}
	symbol := args[0]
	kv := parseKeyValues(args[1:])
	qty := parseFloatKV(kv, "quantity", 0)
	reason := kv["reason"]
	if reason == "" {
		reason = "manual-close"
	}
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	if strings.HasSuffix(sym, "USDT") {
		return fmt.Errorf("close-margin requires spot symbol (e.g. BCH, not BCHUSDT), got %q", symbol)
	}
	if !strings.HasSuffix(sym, "_JPY") {
		sym += "_JPY"
	}
	executor := trading.NewGMOOrderExecutor(cli.logger)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	positions, err := executor.FetchOpenPositions(ctx, sym)
	if err != nil {
		return fmt.Errorf("close-margin: %w", err)
	}
	side, qty, entryVWAP, err := pickCloseSide(positions, kv["side"], qty)
	if err != nil {
		return fmt.Errorf("close-margin: %w", err)
	}
	res, err := executor.CloseMarginPosition(ctx, symbol, side, qty)
	if err != nil {
		return fmt.Errorf("close-margin: %w", err)
	}
	fill := res.Price
	if fill <= 0 {
		fill = entryVWAP
	}
	pnl := (entryVWAP - fill) * res.Quantity
	action := "BUY"
	if side == domain.TradeBuy {
		pnl = (fill - entryVWAP) * res.Quantity
		action = "SELL"
	}
	pnlCopy := pnl
	_ = cli.tradeLog.Append(trading.TradeLogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Action:    action, Symbol: strings.ToUpper(strings.TrimSpace(symbol)),
		Price: fill, Amount: res.Quantity, Pnl: &pnlCopy,
		Reason: reason, OrderID: res.OrderID, Status: "filled",
	})
	return printJSON(map[string]any{
		"status": "ok", "symbol": symbol, "side": string(side),
		"closed_qty": res.Quantity, "fill_price": fill, "pnl": pnlCopy,
		"order_id": res.OrderID, "reason": reason,
	})
}

// gmoPositions はレバレッジ建玉一覧を読み取り専用で取得する。
// 例: core gmo-positions --symbol=BTC_JPY
func (cli *CLI) gmoPositions(args []string) error {
	kv := parseKeyValues(args)
	symbol := kv["symbol"]
	if symbol == "" {
		symbol = "BTC_JPY"
	}
	executor := trading.NewGMOOrderExecutor(cli.logger)
	positions, err := executor.FetchOpenPositions(context.Background(), symbol)
	if err != nil {
		return fmt.Errorf("gmo-positions: %w", err)
	}
	return printJSON(map[string]any{"status": "ok", "symbol": symbol, "positions": positions})
}

// gmoActiveOrders は GMO 有効注文一覧を読み取り専用で取得する。
// トレード系権限の有効判定に使う。例: core gmo-active-orders --symbol=XRP
func (cli *CLI) gmoActiveOrders(args []string) error {
	kv := parseKeyValues(args)
	symbol := kv["symbol"]
	if symbol == "" {
		symbol = "BTC"
	}
	executor := trading.NewGMOOrderExecutor(cli.logger)
	orders, err := executor.FetchActiveOrders(context.Background(), symbol)
	if err != nil {
		return fmt.Errorf("gmo-active-orders: %w", err)
	}
	return printJSON(map[string]any{"status": "ok", "orders": orders})
}

// gmoAffordability は口座余力に対する銘柄別の取引可否を出力する。
// 例: core gmo-affordability
// 残高不足で発注できない場合に「いくら必要か・どれなら買えるか」を示す。
func (cli *CLI) gmoAffordability(args []string) error {
	_ = args
	ctx := context.Background()
	executor := trading.NewGMOOrderExecutor(cli.logger)
	assets, err := executor.FetchAssets(ctx)
	if err != nil {
		return fmt.Errorf("gmo-affordability: %w", err)
	}
	md := exchange.NewGMOClient(defaultTimeout)
	report, err := trading.CheckAffordability(assets, func(_ context.Context, symbol string) (*domain.Ticker, error) {
		return md.FetchTicker(symbol)
	})
	if err != nil {
		return fmt.Errorf("gmo-affordability: %w", err)
	}
	return printJSON(map[string]any{
		"status":             "ok",
		"jpy_available":      report.JPYAvailable,
		"fee_bps":            report.FeeBps,
		"symbols":            report.Symbols,
		"affordable_symbols": report.AffordableSymbols(),
	})
}

// parseKeyValues は "--key=value" 形式の引数を map に変換する。
func parseKeyValues(args []string) map[string]string {
	kv := map[string]string{}
	for _, arg := range args {
		key, value, ok := strings.Cut(arg, "=")
		if !ok {
			continue
		}
		kv[strings.TrimPrefix(key, "--")] = value
	}
	return kv
}

func parseFloatKV(kv map[string]string, key string, fallback float64) float64 {
	if v, ok := kv[key]; ok && v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			return f
		}
	}
	return fallback
}

// envCaptureSeconds は LOCAL_CAPTURE_MINUTES（10〜60 にクランプ）を秒数へ変換する。
// 未設定時は 30 分。
func envCaptureSeconds() float64 {
	minutes := 30.0
	if raw := os.Getenv("LOCAL_CAPTURE_MINUTES"); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil {
			minutes = math.Min(math.Max(v, 10), 60)
		}
	}
	return minutes * 60
}
