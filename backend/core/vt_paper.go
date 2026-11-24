package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"os/signal"

	"github.com/cryptoEngine/core/backtest"
	"github.com/cryptoEngine/core/capture"
	"github.com/cryptoEngine/core/domain"
	"github.com/cryptoEngine/core/infrastructure/exchange"
	"github.com/cryptoEngine/core/usecase/trading"
	vt "github.com/cryptoEngine/core/usecase/trading/vt"
)

// vt_paper.go は V.Tエンジン（テクニカル分析型）の記録専用ループを提供する。
//
// 約定は一切行わない。board スナップショットを足に束ね、V.T判定の
// 入口・出口を JSONL に残す。銘柄選定と個数（sizing）は trading の
// 共通関数（ギア・余力上限）で計算し、live と同一ロジックで記録する。
// 出力は vt_signals_<SYM>_<bars>_<YYYYMMDD>.jsonl。
// --bars に複数足種（例 "60,300,3600"）を渡すと1プロセスで同時記録し、
// 17銘柄×3足種を17プロセスに収める。ファイル・退避建玉は足種別に分離する。

// vtSignalRecord は vt_signals の1行。paper-calib 互換欄（mid_price・
// regime・raw_score）を含み、将来の還流にも使える。
// 出口行は entry_time/entry_pattern で入口と対にできる。
type vtSignalRecord struct {
	Timestamp    int64   `json:"ts"`
	Symbol       string  `json:"symbol"`
	Type         string  `json:"type"`
	Price        float64 `json:"price"`
	Reason       string  `json:"reason"`
	Pattern      string  `json:"pattern"`
	Regime       string  `json:"regime"`
	Confidence   float64 `json:"confidence"`
	RSI          float64 `json:"rsi"`
	PercentB     float64 `json:"percent_b"`
	MACDHist     float64 `json:"macd_hist"`
	Qty          float64 `json:"qty"`
	MidPrice     float64 `json:"mid_price"`
	RawScore     float64 `json:"raw_score"`
	EntryTime    int64   `json:"entry_time,omitempty"`
	EntryPattern string  `json:"entry_pattern,omitempty"`
	// BarO/H/L/V は入口確定足の形状（事後検証用。live足と収集足の乖離切り分け）。
	// 出口行ではゼロ（約定時midのみ記録）。
	BarO float64 `json:"bar_o,omitempty"`
	BarH float64 `json:"bar_h,omitempty"`
	BarL float64 `json:"bar_l,omitempty"`
	BarV float64 `json:"bar_v,omitempty"`
	// MFE/MAE は建玉中の最大順行・最大逆行の比率（出口行のみ）。
	// ETH13:00型の「直後に来る本moves」逃しなど、出口タイミング検証用。
	// 約定判断には一切使わない（記録専用）。
	MFE float64 `json:"mfe,omitempty"`
	MAE float64 `json:"mae,omitempty"`
	// MMState は入口時点の板厚・テープ体制（accumulate/distribute/balanced）。
	// PFの体制別分離キー。出口行では入口値を引き継ぐ。
	MMState string `json:"mm_state,omitempty"`
	// BarsSec は入口足の秒数。1分・5分・1時間の記録を同一銘柄で並走させるため、
	// ファイル名・採点キーにも使う（vt_score は symbol+bars で対付けする）。
	BarsSec int64 `json:"bars_sec"`
}

func vtSignalsPath(dir, symbol string, barsSec int64, day string) string {
	return dir + "/vt_signals_" + symbol + "_" + strconv.FormatInt(barsSec, 10) + "_" + day + ".jsonl"
}

// gridSignalsPath はグリッド記録のパスを返す。trend とファイルを分け、
// PFを戦略別に精密分析できるようにする（週次採点の glob 対象外）。
func gridSignalsPath(dir, symbol string, barsSec int64, day string) string {
	return dir + "/grid_signals_" + symbol + "_" + strconv.FormatInt(barsSec, 10) + "_" + day + ".jsonl"
}

// vtTFState は1足種分の記録状態（エンジン・建玉・出力先）。
// peak/trough は建玉中の最大・最小mid（MFE/MAE記録用。約定判断には使わない）。
type vtTFState struct {
	barsSec  int64
	eng      *vt.Engine
	open     *vt.OpenPos
	sigFile  *os.File
	gridFile *os.File
	curDay   string
	entries  int
	exits    int
	peak     float64
	trough   float64
}

// parseBarsList は "60,300,3600" を検証済み足秒列に変換する。空は {300}。
// 単一値（"300"）も受け付けるため旧 cron と互換性がある。
func parseBarsList(raw string) []int64 {
	seen := map[int64]bool{}
	var out []int64
	if strings.TrimSpace(raw) == "" {
		raw = "300"
	}
	for _, p := range strings.Split(raw, ",") {
		var v int64
		if _, err := fmt.Sscanf(strings.TrimSpace(p), "%d", &v); err != nil {
			continue
		}
		if v != 60 && v != 300 && v != 900 && v != 3600 {
			continue
		}
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// rotate は日次ファイルを回す。paper と同じく1行単一 write で追記する
// （並行追記時の混線防止）。
func (st *vtTFState) rotate(dir, symbol, day string) error {
	if st.sigFile != nil {
		if err := st.sigFile.Close(); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(vtSignalsPath(dir, symbol, st.barsSec, day), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	st.sigFile = f
	if st.gridFile != nil {
		if err := st.gridFile.Close(); err != nil {
			return err
		}
	}
	gf, err := os.OpenFile(gridSignalsPath(dir, symbol, st.barsSec, day), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	st.gridFile = gf
	st.curDay = day
	return nil
}

// writeRec は1行追記する。
func (st *vtTFState) writeRec(rec vtSignalRecord) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = st.sigFile.Write(append(line, '\n'))
	return err
}

// writeGridRec はグリッド記録に1行追記する（trend とファイル分離・PF分離用）。
func (st *vtTFState) writeGridRec(rec vtSignalRecord) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = st.gridFile.Write(append(line, '\n'))
	return err
}

func (cli *CLI) vtPaper(args []string) error {
	kv := parseKeyValues(args)
	seconds := parseFloatKV(kv, "seconds", 0)
	symbol := kv["symbol"]
	if symbol == "" {
		symbol = "BTC"
	}
	// --bars は単一値互換のカンマ列（例 "60,300,3600"）。1プロセスで
	// 全足種を同時記録し、17銘柄×3足種を17プロセスに収める。
	barsList := parseBarsList(kv["bars"])
	if len(barsList) == 0 {
		return fmt.Errorf("vt-paper: --bars must be comma-separated subset of 60,300,900,3600")
	}
	trendTF := int64(parseFloatKV(kv, "trend-tf", 3600))
	balance := parseFloatKV(kv, "balance-jpy", 100000)
	riskFraction := parseFloatKV(kv, "risk-fraction", 0.2)

	dir := orderbookLogDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("vt-paper: mkdir: %w", err)
	}

	baseCfg := vt.DefaultConfig()
	baseCfg.TrendTFSec = trendTF
	baseCfg.MAFast = int(parseFloatKV(kv, "ma-fast", float64(baseCfg.MAFast)))
	baseCfg.MASlow = int(parseFloatKV(kv, "ma-slow", float64(baseCfg.MASlow)))
	baseCfg.BBPeriod = int(parseFloatKV(kv, "bb-period", float64(baseCfg.BBPeriod)))
	baseCfg.BBK = parseFloatKV(kv, "bb-k", baseCfg.BBK)
	baseCfg.RSIPeriod = int(parseFloatKV(kv, "rsi-period", float64(baseCfg.RSIPeriod)))
	baseCfg.MACDFast = int(parseFloatKV(kv, "macd-fast", float64(baseCfg.MACDFast)))
	baseCfg.MACDSlow = int(parseFloatKV(kv, "macd-slow", float64(baseCfg.MACDSlow)))
	baseCfg.MACDSig = int(parseFloatKV(kv, "macd-sig", float64(baseCfg.MACDSig)))
	baseCfg.TPRatio = parseFloatKV(kv, "tp-ratio", baseCfg.TPRatio)
	baseCfg.SLRatio = parseFloatKV(kv, "sl-ratio", baseCfg.SLRatio)
	baseCfg.MaxHoldSec = parseFloatKV(kv, "max-hold-sec", baseCfg.MaxHoldSec)
	baseCfg.FastTFSec = int64(parseFloatKV(kv, "fast-tf", 60))
	baseCfg.TrailActivateR = parseFloatKV(kv, "trail-activate", baseCfg.TrailActivateR)
	baseCfg.TrailBackR = parseFloatKV(kv, "trail-back", baseCfg.TrailBackR)
	baseCfg.TrailActivateATR = parseFloatKV(kv, "trail-activate-atr", baseCfg.TrailActivateATR)
	baseCfg.TrailBackATR = parseFloatKV(kv, "trail-back-atr", baseCfg.TrailBackATR)
	baseCfg.SLATR = parseFloatKV(kv, "sl-atr", baseCfg.SLATR)
	baseCfg.ImbMinHoldSec = parseFloatKV(kv, "imbalance-min-hold-sec", baseCfg.ImbMinHoldSec)
	baseCfg.ExitConfirmVotes = int(parseFloatKV(kv, "exit-confirm-votes", float64(baseCfg.ExitConfirmVotes)))
	minConf := parseFloatKV(kv, "min-confidence", 0)
	shortRSIFloor := parseFloatKV(kv, "short-rsi-floor", 0)
	// コスト・ゲート（k=0で無効）。live/replay と同一の gate を使う。
	costGateK := parseFloatKV(kv, "cost-gate-k", 0)
	costGateFeeMode := strings.TrimSpace(kv["cost-gate-fee-mode"])
	if costGateFeeMode == "" {
		costGateFeeMode = "taker"
	}
	gateEntryFee, gateExitFee, gateModeOK := feeModeRates(costGateFeeMode)
	if !gateModeOK {
		gateEntryFee, gateExitFee = 0.0005, 0.0005
	}
	entryCooldownSec := parseFloatKV(kv, "entry-cooldown-sec", 0)
	volTarget := parseFloatKV(kv, "vol-target", 0)
	gridCostRate := parseFloatKV(kv, "grid-cost-rate", 0)
	// ソフト出口の禁止（--exit-deny=rsi_weak,signal 等。空＝全許可）。
	var exitDeny []string
	for _, p := range strings.Split(kv["exit-deny"], ",") {
		if s := strings.TrimSpace(p); s != "" {
			exitDeny = append(exitDeny, s)
		}
	}
	// グリッド記録は既定で有効（記録のみで発注しないため安全）。
	// 無効化は --grid-enabled=false で行う。
	gridEnabled := true
	if raw, ok := kv["grid-enabled"]; ok {
		if s := strings.TrimSpace(raw); strings.EqualFold(s, "false") || s == "0" {
			gridEnabled = false
		}
	}
	gridRiskFraction := parseFloatKV(kv, "grid-risk-fraction", 0.05)
	if raw, ok := kv["short-patterns"]; ok {
		baseCfg.ShortAllow = vt.SplitPatterns(raw)
	}
	// live と同一の自動調整を検証用にも適用する（検証と執行の乖離防止）。
	if pf := strings.TrimSpace(kv["pattern-allow-file"]); pf != "" {
		if ld, sd, err := loadPatternDeny(pf); err != nil {
			cli.logger.Info("vt-paper: pattern file unreadable (%v); using defaults", err)
		} else {
			baseCfg.LongDeny, baseCfg.ShortDeny = ld, sd
		}
		if cells, err := loadAllowCells(pf); err == nil && len(cells) > 0 {
			baseCfg.AllowCells = cells
			cli.logger.Info("vt-paper: allow cells=%d from %s", len(cells), pf)
		}
	}
	baseCfg.GridCostRate = gridCostRate
	if len(exitDeny) > 0 {
		baseCfg.ExitDeny = exitDeny
	}
	baseCfg.SqueezeGate = true
	if raw, ok := kv["squeeze-gate"]; ok {
		if s := strings.TrimSpace(raw); strings.EqualFold(s, "false") || s == "0" {
			baseCfg.SqueezeGate = false
		}
	}
	if baseCfg.MASlow < 2 {
		baseCfg.MASlow = 2
	}
	if baseCfg.MAFast < 1 || baseCfg.MAFast >= baseCfg.MASlow {
		baseCfg.MAFast = baseCfg.MASlow - 1
		if baseCfg.MAFast < 1 {
			baseCfg.MAFast = 1
		}
	}
	nowMs := time.Now().UnixMilli()
	states := make([]*vtTFState, 0, len(barsList))
	for _, b := range barsList {
		c := baseCfg
		c.BarsSec = b
		st := &vtTFState{barsSec: b, eng: vt.NewEngine(c)}
		st.open = vtLoadOpen(dir, symbol, b, nowMs)
		if st.open != nil {
			cli.logger.Info("vt-paper: resumed open %s/%ds %s entry=%v", symbol, b, st.open.Side, st.open.EntryPrice)
		}
		states = append(states, st)
	}
	// 足種別の最終決済時刻（entry-cooldown 用）。
	lastExit := map[*vtTFState]int64{}
	defer func() {
		for _, st := range states {
			vtSaveOpen(dir, symbol, st.barsSec, st.open)
			if st.sigFile != nil {
				_ = st.sigFile.Close()
			}
			if st.gridFile != nil {
				_ = st.gridFile.Close()
			}
		}
	}()

	stop := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		close(stop)
	}()

	var deadline time.Time
	if seconds > 0 {
		deadline = time.Now().Add(time.Duration(seconds) * time.Second)
		go func() {
			<-time.After(time.Until(deadline))
			select {
			case <-stop:
			default:
				close(stop)
			}
		}()
	}

	// 約定テープ供給：15秒毎に公開テープを取得し、全足種エンジンの
	// CVD・吸収検知と trade_tape_<SYM>_<日>.csv への記録に使う（1取得多用途）。
	// 発注には一切使わない。
	tapeFetcher := vtTapeFetcher(captureSource(kv))
	tapeFile, tapeDay := vtOpenTapeFile(dir, symbol, time.Now().Format("20060102"))
	if tapeFile != nil {
		defer tapeFile.Close()
	}
	tapeSeen := map[string]bool{}
	tapeSeenOrder := make([]string, 0, 1024)
	pollTape := func() {
		if tapeFetcher == nil {
			return
		}
		execs, err := tapeFetcher.FetchExecutions(symbol, 100)
		if err != nil {
			return
		}
		day := time.Now().Format("20060102")
		if day != tapeDay && tapeFile != nil {
			tapeFile.Close()
			tapeFile, tapeDay = vtOpenTapeFile(dir, symbol, day)
		}
		for _, e := range execs {
			if e.Timestamp <= 0 || e.Price <= 0 || e.Qty <= 0 {
				continue
			}
			key := fmt.Sprintf("%d|%.8f|%.8f|%s", e.Timestamp, e.Price, e.Qty, e.Side)
			if tapeSeen[key] {
				continue
			}
			tapeSeen[key] = true
			tapeSeenOrder = append(tapeSeenOrder, key)
			if len(tapeSeenOrder) > 1024 {
				delete(tapeSeen, tapeSeenOrder[0])
				tapeSeenOrder = tapeSeenOrder[1:]
			}
			buy := e.Side == domain.TradeBuy
			for _, st := range states {
				st.eng.AddTape(symbol, e.Timestamp, e.Price, e.Qty, buy)
			}
			if tapeFile != nil {
				line := fmt.Sprintf("%d,%v,%v,%s\n", e.Timestamp, e.Price, e.Qty, e.Side)
				_, _ = tapeFile.WriteString(line)
			}
		}
	}
	go func() {
		pollTape()
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				pollTape()
			}
		}
	}()

	// ウォームアップ：当日＋前日の板 CSV を各足ビルダーに先読みする。
	// 無ければ live の蓄積を待つ（cron の 420 秒 bounded 実行では
	// 5分足31本に一生届かず 0 件のままになる欠陥の対策）。
	// VT_WARMUP=0 で無効化できる。
	if os.Getenv("VT_WARMUP") != "0" && !strings.EqualFold(os.Getenv("VT_WARMUP"), "false") {
		ticks := vtWarmupTicks(dir, symbol, barsList, trendTF)
		for _, st := range states {
			for _, t := range ticks {
				st.eng.Observe(symbol, t.ts, t.mid, t.bidVol, t.askVol)
			}
			cli.logger.Info("vt-paper: warmed %d ticks for %s/%ds", len(ticks), symbol, st.barsSec)
		}
	}

	streamOpts := capture.Options{
		Symbol:      symbol,
		Levels:      5,
		SampleEvery: time.Duration(parseFloatKV(kv, "interval", 5.0) * float64(time.Second)),
		Source:      captureSource(kv),
	}

	ch, err := capture.Stream(streamOpts, stop)
	if err != nil {
		return fmt.Errorf("vt-paper: stream: %w", err)
	}

	for snap := range ch {
		day := time.Now().Format("20060102")
		bidVol, askVol := 0.0, 0.0
		for _, o := range snap.Bids {
			bidVol += o.Volume
		}
		for _, o := range snap.Asks {
			askVol += o.Volume
		}
		spreadRatio := snapSpreadRatio(snap)
		wall, wallOK := snapWallImb(snap)
		wallBid3, wallAsk3, wallSizeOK := snapWallSizes(snap)
		for _, st := range states {
			if day != st.curDay {
				if err := st.rotate(dir, symbol, day); err != nil {
					return fmt.Errorf("vt-paper: rotate: %w", err)
				}
			}
			if wallOK {
				st.eng.AddWall(symbol, wall)
			}
			if wallSizeOK {
				st.eng.TrackWall(symbol, snap.Timestamp, wallBid3, wallAsk3, snap.MidPrice)
			}
			sig, bar := st.eng.Observe(symbol, snap.Timestamp, snap.MidPrice, bidVol, askVol)
			// 建玉あり → 出口は毎tick判定する（live と同一。SL・時間切れの
			// 足周期遅延をなくす。EvalExit 内の反転・トレンド反転が主経路で、
			// 下の反対シグナル分岐は後方互換の予備）。
			if st.open != nil {
				// MFE/MAE 追跡：建玉中の最大・最小midを更新する（記録専用）。
				if snap.MidPrice > st.peak {
					st.peak = snap.MidPrice
				}
				if st.trough == 0 || snap.MidPrice < st.trough {
					st.trough = snap.MidPrice
				}
				exited := false
				exitReason := ""
				exitSide := "sell"
				if st.open.Side == vt.Sell {
					exitSide = "buy"
				}
				// グリッド建玉は固有出口（利確・無効化）を先に判定する。
				if vt.IsGridPattern(st.open.EntryPattern) {
					if ok, reason := st.eng.GridExit(symbol, *st.open, snap.MidPrice, snap.Timestamp); ok {
						exitReason = reason
						exited = true
					}
				}
				if !exited {
					if ok, reason := st.eng.EvalExit(symbol, *st.open, snap.MidPrice, snap.Timestamp); ok {
						exitReason = reason
						exited = true
					} else if sig != nil && ((st.open.Side == vt.Buy && sig.Type == vt.Sell) ||
						(st.open.Side == vt.Sell && sig.Type == vt.Buy)) {
						exitReason = "opposite_" + sig.Pattern
						exited = true
					}
				}
				if exited {
					mfe, mae := paperExcursion(st.open, st.peak, st.trough)
					rec := vtSignalRecord{
						Timestamp: snap.Timestamp, Symbol: symbol, Type: exitSide,
						Price: snap.MidPrice, Reason: exitReason, Pattern: "exit",
						Regime: st.open.EntryRegime, Confidence: st.open.EntryConf,
						RSI: st.open.EntryRSI, PercentB: st.open.EntryPB, MACDHist: st.open.EntryHist,
						Qty: st.open.Volume, MidPrice: snap.MidPrice, RawScore: st.open.EntryConf,
						EntryTime: st.open.EntryTime, EntryPattern: st.open.EntryPattern,
						MFE: mfe, MAE: mae,
						BarsSec: st.barsSec,
					}
					// グリッド建玉の出口はグリッド記録へ（PF分離・二重記録防止）。
					var writeErr error
					if vt.IsGridPattern(st.open.EntryPattern) {
						writeErr = st.writeGridRec(rec)
					} else {
						writeErr = st.writeRec(rec)
					}
					if writeErr != nil {
						return fmt.Errorf("vt-paper: write exit: %w", writeErr)
					}
					st.exits++
					st.open = nil
					lastExit[st] = snap.Timestamp
				}
				continue
			}
			// 建玉なし → 入口は確定足でのみ判定する（sig=nil の中間tickは
			// 何もしない。nil 参照防止）。
			if bar == nil || sig == nil {
				continue
			}
			// 建玉なし → 入口判定。hold は記録しない（量が多すぎるため）。
			// trend が Hold のときだけグリッド（二刀流のレンジ担当）を評価する。
			if sig.Type != vt.Buy && sig.Type != vt.Sell {
				if gridEnabled {
					gsig := st.eng.GridEntry(symbol, snap.Timestamp, spreadRatio)
					if gsig.Type == vt.Buy {
						okGate, _ := entryAllowed(gsig, minConf, shortRSIFloor)
						okCost, _ := costGateAllowed(st.eng.ATRRatio(symbol), spreadRatio, gateEntryFee, gateExitFee, costGateK)
						if okGate && okCost && !tobiitaBlocked(spreadRatio) && !cooldownBlocked(snap.Timestamp, lastExit[st], entryCooldownSec) {
							gqty := vtPaperQty(symbol, snap.MidPrice, gsig.Confidence, balance, gridRiskFraction, spreadRatio)
							if gqty > 0 {
								if volTarget > 0 {
									gqty *= volScale(st.eng.ATRRatio(symbol), volTarget)
								}
								if err := st.writeGridRec(vtSignalRecord{
									Timestamp: gsig.Timestamp, Symbol: symbol, Type: string(gsig.Type),
									Price: gsig.Price, Reason: gsig.Reason, Pattern: gsig.Pattern,
									Regime: gsig.Regime, Confidence: gsig.Confidence,
									RSI: gsig.RSI, PercentB: gsig.PercentB, MACDHist: gsig.MACDHist,
									Qty: gqty, MidPrice: gsig.Price, RawScore: gsig.Confidence,
									MMState: gsig.MMState,
									BarO:    gsig.BarO, BarH: gsig.BarH, BarL: gsig.BarL, BarV: gsig.BarV,
									BarsSec: st.barsSec,
								}); err != nil {
									return fmt.Errorf("vt-paper: write grid entry: %w", err)
								}
								st.entries++
								st.open = &vt.OpenPos{Side: gsig.Type, EntryPrice: gsig.Price, EntryTime: gsig.Timestamp, Volume: gqty,
									EntryRegime: gsig.Regime, EntryPattern: gsig.Pattern,
									EntryRSI: gsig.RSI, EntryPB: gsig.PercentB, EntryHist: gsig.MACDHist, EntryConf: gsig.Confidence}
								st.peak, st.trough = gsig.Price, gsig.Price
							}
						}
					}
				}
				continue
			}
			// 入口ゲート（live と同一。検証と執行の乖離防止）。
			if ok, _ := entryAllowed(sig, minConf, shortRSIFloor); !ok {
				continue
			}
			// 飛び板注意：live と対称にスプレッド異常拡大では見送る。
			if tobiitaBlocked(spreadRatio) {
				continue
			}
			// コスト・ゲート／回転抑制（live/replay と同一）。
			if ok, _ := costGateAllowed(st.eng.ATRRatio(symbol), spreadRatio, gateEntryFee, gateExitFee, costGateK); !ok {
				continue
			}
			if cooldownBlocked(snap.Timestamp, lastExit[st], entryCooldownSec) {
				continue
			}
			qty := vtPaperQty(symbol, snap.MidPrice, sig.Confidence, balance, riskFraction, spreadRatio)
			if volTarget > 0 && qty > 0 {
				qty *= volScale(st.eng.ATRRatio(symbol), volTarget)
			}
			if err := st.writeRec(vtSignalRecord{
				Timestamp: sig.Timestamp, Symbol: symbol, Type: string(sig.Type),
				Price: sig.Price, Reason: sig.Reason, Pattern: sig.Pattern,
				Regime: sig.Regime, Confidence: sig.Confidence,
				RSI: sig.RSI, PercentB: sig.PercentB, MACDHist: sig.MACDHist,
				Qty: qty, MidPrice: sig.Price, RawScore: sig.Confidence,
				MMState: sig.MMState,
				BarO:    sig.BarO, BarH: sig.BarH, BarL: sig.BarL, BarV: sig.BarV,
				BarsSec: st.barsSec,
			}); err != nil {
				return fmt.Errorf("vt-paper: write entry: %w", err)
			}
			st.entries++
			side := sig.Type
			st.open = &vt.OpenPos{Side: side, EntryPrice: sig.Price, EntryTime: sig.Timestamp, Volume: qty,
				EntryRegime: sig.Regime, EntryPattern: sig.Pattern,
				EntryRSI: sig.RSI, EntryPB: sig.PercentB, EntryHist: sig.MACDHist, EntryConf: sig.Confidence}
			st.peak, st.trough = sig.Price, sig.Price
		}
	}

	entries, exits := 0, 0
	for _, st := range states {
		entries += st.entries
		exits += st.exits
	}
	return printJSON(map[string]any{
		"status": "ok", "engine": "vt", "symbol": symbol,
		"entries": entries, "exits": exits, "dir": dir,
	})
}

// vtTapeFetcher はテープ取得クライアントを作る。失敗時は nil（テープなし運転）。
func vtTapeFetcher(source string) exchange.TradeTapeFetcher {
	if exchange.ExchangeID(strings.ToLower(source)) == exchange.ExchangeGMO ||
		strings.EqualFold(strings.TrimSpace(source), "gmo") {
		return exchange.NewGMOClient(defaultTimeout)
	}
	if strings.TrimSpace(source) == "" || strings.EqualFold(strings.TrimSpace(source), "binance") {
		base := os.Getenv("EXCHANGE_BASE_URL")
		if base == "" {
			base = defaultExchangeURL
		}
		return exchange.NewClient(base, defaultTimeout)
	}
	return nil
}

// vtOpenTapeFile は trade_tape_<SYM>_<日>.csv を追記開く。失敗時は nil。
func vtOpenTapeFile(dir, symbol, day string) (*os.File, string) {
	path := dir + "/trade_tape_" + symbol + "_" + day + ".csv"
	needHeader := false
	if fi, err := os.Stat(path); err != nil || fi.Size() == 0 {
		needHeader = true
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, day
	}
	if needHeader {
		if _, err := f.WriteString("timestamp,price,qty,side\n"); err != nil {
			f.Close()
			return nil, day
		}
	}
	return f, day
}

// vtWarmTick はウォームアップ用の1tick（板出来高＋板厚付き）。
type vtWarmTick struct {
	ts             int64
	mid            float64
	bidVol, askVol float64
	wallImb        float64
	wallOK         bool
	wallBid3       float64
	wallAsk3       float64
	wallSizeOK     bool
}

// warmupLookbackDays は板履歴の遡及上限（日）。prune の .gz 90日保持に対応し、
// 不足分は .csv.gz から透過的に読む（openSeriesEither）。
const warmupLookbackDays = 90

// openSeriesEither は orderbook_<SYM>_<日> を .csv→.csv.gz の順で開く。
// どちらも無ければ error（backtest.OpenSeries は .gz suffix で透過展開する）。
func openSeriesEither(dir, symbol, day string) (io.ReadCloser, error) {
	base := dir + "/orderbook_" + symbol + "_" + day
	f, err := backtest.OpenSeries(base + ".csv")
	if err == nil {
		return f, nil
	}
	return backtest.OpenSeries(base + ".csv.gz")
}

// vtWarmupTicks は新しい順に板 CSV を読み、必要十分な tick 列を返す。
// 全量パースは VPS を詰まらせるため、 demanded span（最大足×60本＋余裕）か
// tick 上限のいずれかに達したら打ち切る。通常は直近2〜3日で足りる。
func vtWarmupTicks(dir, symbol string, barsList []int64, trendTF int64) []vtWarmTick {
	maxTF := trendTF
	for _, b := range barsList {
		if b > maxTF {
			maxTF = b
		}
	}
	if maxTF <= 0 {
		maxTF = 3600
	}
	spanSec := maxTF * 70
	cutoff := time.Now().UnixMilli() - spanSec*1000
	const tickCap = 120000
	var out []vtWarmTick
	for d := 0; d < warmupLookbackDays; d++ {
		day := time.Now().Add(-time.Duration(d) * 24 * time.Hour).Format("20060102")
		f, err := openSeriesEither(dir, symbol, day)
		if err != nil {
			continue
		}
		var rows []vtWarmTick
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			ts, mid, bidVol, askVol, wall, wallOK, bid3, ask3, sizeOK := parseOrderbookRow(line)
			if ts <= 0 || mid <= 0 {
				continue // ヘッダ行・壊れ行
			}
			rows = append(rows, vtWarmTick{ts: ts, mid: mid, bidVol: bidVol, askVol: askVol, wallImb: wall, wallOK: wallOK, wallBid3: bid3, wallAsk3: ask3, wallSizeOK: sizeOK})
		}
		f.Close()
		// 新しい順に積むため先頭に挿入する。
		out = append(rows, out...)
		if len(out) >= tickCap {
			out = out[len(out)-tickCap:]
			break
		}
		if len(rows) > 0 && rows[0].ts <= cutoff {
			break
		}
	}
	return out
}

// parseOrderbookRow は capture CSV 行を解釈する。
// 形式: timestamp,midPrice,spread,bids(JSON),asks(JSON)。
// bids/asks 内のカンマを避けるため先頭3列だけ SplitN し、残りは JSON 配列
// 2個として切り分ける。取れない部分はゼロ扱い（足欠落よりマシ）。
// wall/wallOK は上位3段の板厚imb（取れなければ ok=false）。
// bid3/ask3/sizeOK は上位3段の買い・売り数量合計（壁ライフサイクル用）。
func parseOrderbookRow(line string) (ts int64, mid, bidVol, askVol, wall float64, wallOK bool, bid3, ask3 float64, sizeOK bool) {
	parts := strings.SplitN(line, ",", 4)
	if len(parts) < 3 {
		return 0, 0, 0, 0, 0, false, 0, 0, false
	}
	if _, err := fmt.Sscanf(strings.TrimSpace(parts[0]), "%d", &ts); err != nil || ts <= 0 {
		return 0, 0, 0, 0, 0, false, 0, 0, false
	}
	if _, err := fmt.Sscanf(strings.TrimSpace(parts[1]), "%f", &mid); err != nil || mid <= 0 {
		return 0, 0, 0, 0, 0, false, 0, 0, false
	}
	if len(parts) < 4 {
		return ts, mid, 0, 0, 0, false, 0, 0, false
	}
	rest := parts[3]
	sep := strings.Index(rest, "],[")
	var bidsJSON, asksJSON string
	if sep < 0 {
		// 空配列のみ等の変形。全体を bids として解釈を試みる。
		bidsJSON, asksJSON = rest, "[]"
	} else {
		bidsJSON, asksJSON = rest[:sep+1], rest[sep+2:]
	}
	bidVol = sumVolumes(bidsJSON)
	askVol = sumVolumes(asksJSON)
	wall, wallOK = topWallJSON(bidsJSON, asksJSON)
	bid3, ask3, sizeOK = topWallSizesJSON(bidsJSON, asksJSON)
	return ts, mid, bidVol, askVol, wall, wallOK, bid3, ask3, sizeOK
}

// priceVols は板JSONから (price, volume) 列を返す。壊れていれば空。
// [{"price":..,"volume":..}] または [[price,qty]] の両形式に対応する。
func priceVols(raw string) (px, vol []float64) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil, nil
	}
	var objs []struct {
		Price  float64 `json:"price"`
		Volume float64 `json:"volume"`
		Qty    float64 `json:"qty"`
	}
	if err := json.Unmarshal([]byte(raw), &objs); err == nil {
		for _, o := range objs {
			if o.Price <= 0 {
				continue
			}
			v := o.Volume + o.Qty
			if v < 0 {
				continue
			}
			px = append(px, o.Price)
			vol = append(vol, v)
		}
		return px, vol
	}
	var arrs [][]float64
	if err := json.Unmarshal([]byte(raw), &arrs); err == nil {
		for _, a := range arrs {
			if len(a) >= 2 && a[0] > 0 && a[1] >= 0 {
				px = append(px, a[0])
				vol = append(vol, a[1])
			}
		}
		return px, vol
	}
	return nil, nil
}

// topWallJSON は bids/asks JSON から上位3段imbを返す。取れなければ ok=false。
func topWallJSON(bidsJSON, asksJSON string) (float64, bool) {
	bidPx, bidVol := priceVols(bidsJSON)
	askPx, askVol := priceVols(asksJSON)
	return topWallImb(bidPx, bidVol, askPx, askVol)
}

// topWallSizesJSON は bids/asks JSON から上位3段の買い・売り数量合計を返す。
// 壁ライフサイクル追跡用。取れなければ ok=false。
func topWallSizesJSON(bidsJSON, asksJSON string) (bid3, ask3 float64, ok bool) {
	bidPx, bidVol := priceVols(bidsJSON)
	askPx, askVol := priceVols(asksJSON)
	return topWallSizes(bidPx, bidVol, askPx, askVol)
}

// sumVolumes は [{"price":..,"volume":..}] または [[price,qty]] の合計を返す。
// 壊れていれば 0。
func sumVolumes(raw string) float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return 0
	}
	var objs []struct {
		Volume float64 `json:"volume"`
		Qty    float64 `json:"qty"`
	}
	if err := json.Unmarshal([]byte(raw), &objs); err == nil {
		total := 0.0
		for _, o := range objs {
			total += o.Volume + o.Qty
		}
		return total
	}
	var arrs [][]float64
	if err := json.Unmarshal([]byte(raw), &arrs); err == nil {
		total := 0.0
		for _, a := range arrs {
			if len(a) >= 2 {
				total += a[1]
			}
		}
		return total
	}
	return 0
}

// vtWarmup は板 CSV をエンジンに先読みし、読んだ tick 行数を返す。
// 失敗・不在時は 0（live 蓄積にフォールバックする）。
func vtWarmup(eng *vt.Engine, dir, symbol string, barsList []int64, trendTF int64) int {
	ticks := vtWarmupTicks(dir, symbol, barsList, trendTF)
	for _, t := range ticks {
		if t.wallOK {
			eng.AddWall(symbol, t.wallImb)
		}
		if t.wallSizeOK {
			eng.TrackWall(symbol, t.ts, t.wallBid3, t.wallAsk3, t.mid)
		}
		eng.Observe(symbol, t.ts, t.mid, t.bidVol, t.askVol)
	}
	return len(ticks)
}

// paperExcursion は建玉の MFE/MAE 比率を返す（記録専用・テスト対象）。
// peak/trough は建玉中の最大・最小mid（決済tick含む）。
// 買い玉：mfe=(peak-entry)/entry（>=0）、mae=(trough-entry)/entry（<=0）。
// 売り玉：対称。entry<=0 のときは共に 0。約定判断には使わない。
func paperExcursion(open *vt.OpenPos, peak, trough float64) (mfe, mae float64) {
	if open == nil || open.EntryPrice <= 0 {
		return 0, 0
	}
	if open.Side == vt.Sell {
		return (open.EntryPrice - trough) / open.EntryPrice, (open.EntryPrice - peak) / open.EntryPrice
	}
	return (peak - open.EntryPrice) / open.EntryPrice, (trough - open.EntryPrice) / open.EntryPrice
}

// snapSpreadRatio は板スナップショットのスプレッド率を返す（取れなければ 0）。
func snapSpreadRatio(snap capture.Snapshot) float64 {
	if snap.MidPrice <= 0 || len(snap.Bids) == 0 || len(snap.Asks) == 0 {
		return 0
	}
	spread := snap.Asks[0].Price - snap.Bids[0].Price
	if spread < 0 {
		spread = 0
	}
	return spread / snap.MidPrice
}

// snapWallImb はスナップショットの上位3段の板厚imbを返す。
// 取れなければ ok=false（呼び出し側は AddWall を見送る）。
// Bids/Asks は最良先頭を想定するが、価格順に選び直すため順序不正にも強い。
func snapWallImb(snap capture.Snapshot) (float64, bool) {
	bidPx, bidVol, askPx, askVol := snapWallPxVol(snap)
	return topWallImb(bidPx, bidVol, askPx, askVol)
}

// snapWallSizes はスナップショットの上位3段の買い・売り数量合計を返す。
// 壁ライフサイクル（出現・消失・食い尽くし）追跡用。取れなければ ok=false。
func snapWallSizes(snap capture.Snapshot) (bid3, ask3 float64, ok bool) {
	bidPx, bidVol, askPx, askVol := snapWallPxVol(snap)
	return topWallSizes(bidPx, bidVol, askPx, askVol)
}

// snapWallPxVol は板スナップショットから有効な価格・数量列を抜き出す共用部。
func snapWallPxVol(snap capture.Snapshot) (bidPx, bidVol, askPx, askVol []float64) {
	for _, o := range snap.Bids {
		if o.Price <= 0 || o.Volume < 0 {
			continue
		}
		bidPx = append(bidPx, o.Price)
		bidVol = append(bidVol, o.Volume)
	}
	for _, o := range snap.Asks {
		if o.Price <= 0 || o.Volume < 0 {
			continue
		}
		askPx = append(askPx, o.Price)
		askVol = append(askVol, o.Volume)
	}
	return bidPx, bidVol, askPx, askVol
}

// vtPaperQty は paper 用の発注数量を live と同一ロジックで計算する。
// 基準額面＝余力×リスク割合とし、信頼度・スプレッド・上限をギアで調整する。
//
// 注意: SizeForSignal は (0.5+conf) 倍スケール後に上限判定するため、
// 基準額面を Balance*RiskFraction でもう一度縛ると conf>0.5 で必ず棄却され
// qty=0 になる（2026-09-22 実測で全銘柄 qty:0 を確認）。上限はフルスケール
// (1.5倍) を見込んだ MaxPos でのみ掛け、Balance 側は使わない。
func vtPaperQty(symbol string, price, conf, balance, riskFraction, spreadRatio float64) float64 {
	ruleMin, ruleStep, ok := trading.GMOSymbolRule(symbol)
	if !ok || price <= 0 {
		return 0
	}
	base := balance * riskFraction
	if base <= 0 {
		return 0
	}
	if spreadRatio < 0 {
		spreadRatio = 0
	}
	qty, ok := trading.SizeForSignal(base, price, ruleMin, ruleStep, conf, spreadRatio, trading.GearParams{
		TradeSize: base, MaxPos: base * 1.5,
	})
	if !ok {
		return 0
	}
	return qty
}

// vtOpenStatePath は bounded 実行をまたぐ建玉退避ファイルのパスを返す。
// 足種ごとに分ける（1分・5分・1時間を同一銘柄で並走させるため）。
func vtOpenStatePath(dir, symbol string, barsSec int64) string {
	return dir + "/vt_open_" + symbol + "_" + strconv.FormatInt(barsSec, 10) + ".json"
}

// vtResumeMaxAgeSec は退避建玉の再開上限（秒）。ゾンビ建玉の再開を防ぐ。
const vtResumeMaxAgeSec = int64(7 * 24 * 3600)

// vtLoadOpen は退避建玉を読み込む。無効・古い場合は nil。
func vtLoadOpen(dir, symbol string, barsSec int64, nowMs int64) *vt.OpenPos {
	data, err := os.ReadFile(vtOpenStatePath(dir, symbol, barsSec))
	if err != nil {
		return nil
	}
	var open vt.OpenPos
	if err := json.Unmarshal(data, &open); err != nil {
		return nil
	}
	if open.EntryPrice <= 0 || open.Volume <= 0 {
		return nil
	}
	if open.Side != vt.Buy && open.Side != vt.Sell {
		return nil
	}
	if nowMs-open.EntryTime > vtResumeMaxAgeSec*1000 {
		return nil
	}
	return &open
}

// vtSaveOpen は建玉を退避する。open==nil のときは退避ファイルを消す。
func vtSaveOpen(dir, symbol string, barsSec int64, open *vt.OpenPos) {
	path := vtOpenStatePath(dir, symbol, barsSec)
	if open == nil {
		_ = os.Remove(path)
		return
	}
	data, err := json.Marshal(open)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, append(data, '\n'), 0o644)
}
