package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cryptoEngine/core/backtest"
	vt "github.com/cryptoEngine/core/usecase/trading/vt"
)

// vt_replay.go は保存板 CSV の歴史リプレイ（発注なし・paper 互換記録）。
//
// 例: core vt-replay --symbol=BTC --bars=300 --from=20260901 --to=20260923
//     --trend-tf=3600 --fast-tf=60 --balance-jpy=100000 --risk-fraction=0.2
//     --trail-activate=0.015 --trail-back=0.008 --out=/tmp/replay.jsonl
//
// 板＋テープを時系列でエンジンに流し、1建玉ずつ入口・出口を記録する。
// live と同一の判定・数量ロジックを使い、Sell 入口も記録する点だけ
// paper 側に合わせる（現物 Buy 専用フィルタは live 側の責務）。
// 最後に vt-score と同じ対付け集計を表示する。

// replayTick はリプレイ用の1tick（板出来高＋スプレッド＋板厚付き）。
type replayTick struct {
	ts          int64
	mid         float64
	bidVol      float64
	askVol      float64
	spreadRatio float64
	wallImb     float64
	wallOK      bool
	wallBid3    float64
	wallAsk3    float64
	wallSizeOK  bool
}

// replayTape はリプレイ用の約定1件。
type replayTape struct {
	ts    int64
	price float64
	qty   float64
	buy   bool
}

// parseSpreadRatio は capture CSV 行のスプレッド率を返す（取れなければ 0）。
// 形式: timestamp,midPrice,spread,bids(JSON),asks(JSON)。
func parseSpreadRatio(line string, mid float64) float64 {
	if mid <= 0 {
		return 0
	}
	parts := strings.SplitN(line, ",", 4)
	if len(parts) < 3 {
		return 0
	}
	spread, err := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
	if err != nil || spread < 0 {
		return 0
	}
	return spread / mid
}

// readReplayDay は1日分の板・テープ列を読む（.csv→.csv.gz の順）。
func readReplayDay(dir, symbol, day string) ([]replayTick, []replayTape, bool) {
	base := dir + "/orderbook_" + symbol + "_" + day
	bf, err := backtest.OpenSeries(base + ".csv")
	if err != nil {
		bf, err = backtest.OpenSeries(base + ".csv.gz")
		if err != nil {
			return nil, nil, false
		}
	}
	var ticks []replayTick
	sc := bufio.NewScanner(bf)
	sc.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		ts, mid, bidVol, askVol, wall, wallOK, bid3, ask3, sizeOK := parseOrderbookRow(line)
		if ts <= 0 || mid <= 0 {
			continue
		}
		ticks = append(ticks, replayTick{ts: ts, mid: mid,
			bidVol: bidVol, askVol: askVol, spreadRatio: parseSpreadRatio(line, mid),
			wallImb: wall, wallOK: wallOK, wallBid3: bid3, wallAsk3: ask3, wallSizeOK: sizeOK})
	}
	bf.Close()
	if len(ticks) == 0 {
		return nil, nil, false
	}
	var tapes []replayTape
	for _, ext := range []string{".csv", ".csv.gz"} {
		tf, err := backtest.OpenSeries(dir + "/trade_tape_" + symbol + "_" + day + ext)
		if err != nil {
			continue
		}
		tsc := bufio.NewScanner(tf)
		tsc.Buffer(make([]byte, 1024*1024), 1024*1024)
		for tsc.Scan() {
			parts := strings.Split(strings.TrimSpace(tsc.Text()), ",")
			if len(parts) < 4 {
				continue
			}
			ts, err1 := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
			price, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
			qty, err3 := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
			if err1 != nil || err2 != nil || err3 != nil || ts <= 0 || price <= 0 || qty <= 0 {
				continue // ヘッダ行・壊れ行
			}
			tapes = append(tapes, replayTape{ts: ts, price: price, qty: qty,
				buy: strings.EqualFold(strings.TrimSpace(parts[3]), "buy")})
		}
		tf.Close()
		break
	}
	return ticks, tapes, true
}

// replaySymbol は期間の板・テープをエンジンに流して記録列を返す。
func replaySymbol(dir, symbol, fromDay, toDay string, barsSec, trendTF, fastTF int64, trailA, trailB, balance, riskFraction float64, shortAllow []string, allowCells []vt.AllowCell, minConf, shortRSIFloor, gridRiskFraction float64, gridEnabled bool, maker bool, costGateK, gateEntryFee, gateExitFee, entryCooldownSec, volTarget, gridCostRate float64, makerRequireAbsorb bool, trailActivateATR, trailBackATR, slATR, makerCrossBps float64, exitConfirmVotes int, longDeny, shortDeny, exitDeny []string, tpRatio float64, exitMaker bool, exitMakerTimeoutMs float64) ([]vtSignalRecord, int, error) {
	cfg := vt.DefaultConfig()
	cfg.BarsSec = barsSec
	cfg.TrendTFSec = trendTF
	cfg.FastTFSec = fastTF
	cfg.TrailActivateR = trailA
	cfg.TrailBackR = trailB
	cfg.TPRatio = tpRatio
	cfg.GridCostRate = gridCostRate
	cfg.TrailActivateATR = trailActivateATR
	cfg.TrailBackATR = trailBackATR
	cfg.SLATR = slATR
	if shortAllow != nil {
		cfg.ShortAllow = shortAllow
	}
	if len(allowCells) > 0 {
		cfg.AllowCells = allowCells
	}
	cfg.ExitConfirmVotes = exitConfirmVotes
	if longDeny != nil {
		cfg.LongDeny = longDeny
	}
	if shortDeny != nil {
		cfg.ShortDeny = shortDeny
	}
	if len(exitDeny) > 0 {
		cfg.ExitDeny = exitDeny
	}
	eng := vt.NewEngine(cfg)
	var recs []vtSignalRecord
	var open *vt.OpenPos
	var peak, trough float64
	// makerPending は指値待機中の入口（Maker検証用）。約定も取消もなく
	// タイムアウトしたら消える（miss）。約定はタッチで全量とみなす
	// （上振れ評価。キュー・部分約定は無視する旨を検証報告に明記）。
	type makerPending struct {
		side  vt.Side
		limit float64
		qty   float64
		sig   *vt.Signal
		until int64
	}
	var pending *makerPending
	// makerExitPending は利確の指値待機（Maker出口検証用）。約定は指値到達で
	// 成立し、他出口・期限では成行（taker）へ退避する。
	type makerExitPending struct {
		side   string
		limit  float64
		until  int64
		reason string
	}
	var exitPending *makerExitPending
	// lastExitMs は最終決済時刻（entry-cooldown 用・単一銘柄）。
	var lastExitMs int64
	const makerTimeoutMs = int64(15 * 60 * 1000)
	files := 0
	from, err := time.Parse("20060102", fromDay)
	if err != nil {
		return nil, 0, fmt.Errorf("vt-replay: bad --from (want YYYYMMDD): %w", err)
	}
	to, err := time.Parse("20060102", toDay)
	if err != nil {
		return nil, 0, fmt.Errorf("vt-replay: bad --to (want YYYYMMDD): %w", err)
	}
	if to.Before(from) {
		return nil, 0, fmt.Errorf("vt-replay: --to is before --from")
	}
	if to.Sub(from).Hours()/24 > 183 {
		return nil, 0, fmt.Errorf("vt-replay: range over 183 days")
	}
	for d := from; !d.After(to); d = d.Add(24 * time.Hour) {
		day := d.Format("20060102")
		ticks, tapes, ok := readReplayDay(dir, symbol, day)
		if !ok {
			continue
		}
		files++
		ti := 0
		for _, t := range ticks {
			for ti < len(tapes) && tapes[ti].ts <= t.ts {
				tp := tapes[ti]
				eng.AddTape(symbol, tp.ts, tp.price, tp.qty, tp.buy)
				ti++
			}
			if t.wallOK {
				eng.AddWall(symbol, t.wallImb)
			}
			if t.wallSizeOK {
				eng.TrackWall(symbol, t.ts, t.wallBid3, t.wallAsk3, t.mid)
			}
			// Maker待機の約定判定：指値タッチ（または crossBps 貫通）で全量約定、
			// 期限切れで取消。crossBps>0 は貫通要求で逆選択を織り込む。
			if maker && pending != nil {
				filled := makerCrossed(string(pending.side), t.mid, pending.limit, makerCrossBps)
				if filled {
					psig := pending.sig
					recs = append(recs, vtSignalRecord{
						Timestamp: t.ts, Symbol: symbol, Type: string(pending.side),
						Price: pending.limit, Reason: psig.Reason + "/maker", Pattern: psig.Pattern,
						Regime: psig.Regime, Confidence: psig.Confidence,
						RSI: psig.RSI, PercentB: psig.PercentB, MACDHist: psig.MACDHist,
						Qty: pending.qty, MidPrice: t.mid, RawScore: psig.Confidence,
						MMState: psig.MMState,
						BarO:    psig.BarO, BarH: psig.BarH, BarL: psig.BarL, BarV: psig.BarV,
						BarsSec: barsSec,
					})
					open = &vt.OpenPos{Side: pending.side, EntryPrice: pending.limit, EntryTime: t.ts, Volume: pending.qty,
						EntryRegime: psig.Regime, EntryPattern: psig.Pattern,
						EntryRSI: psig.RSI, EntryPB: psig.PercentB, EntryHist: psig.MACDHist, EntryConf: psig.Confidence}
					peak, trough = pending.limit, pending.limit
					pending = nil
				} else if t.ts > pending.until {
					pending = nil
				}
			}
			sig, bar := eng.Observe(symbol, t.ts, t.mid, t.bidVol, t.askVol)
			// Maker出口（利確）待機：毎tickで約定/強制/期限を判定する。
			if open != nil && exitPending != nil {
				exited := false
				reason := ""
				price := t.mid
				if makerCrossed(exitPending.side, t.mid, exitPending.limit, makerCrossBps) {
					exited, reason, price = true, exitPending.reason+"/maker", exitPending.limit
				} else {
					forced := ""
					if bar != nil && sig != nil {
						if ok, r := eng.EvalExit(symbol, *open, t.mid, t.ts); ok && r != "take_profit" {
							forced = r
						} else if (open.Side == vt.Buy && sig.Type == vt.Sell) ||
							(open.Side == vt.Sell && sig.Type == vt.Buy) {
							forced = "opposite_" + sig.Pattern
						}
					}
					if forced != "" {
						exited, reason, price = true, forced, t.mid
					} else if t.ts > exitPending.until {
						exited, reason, price = true, "take_profit/timeout", t.mid
					}
				}
				if exited {
					mfe, mae := paperExcursion(open, peak, trough)
					recs = append(recs, vtSignalRecord{
						Timestamp: t.ts, Symbol: symbol, Type: exitPending.side,
						Price: price, Reason: reason, Pattern: "exit",
						Regime: open.EntryRegime, Confidence: open.EntryConf,
						RSI: open.EntryRSI, PercentB: open.EntryPB, MACDHist: open.EntryHist,
						Qty: open.Volume, MidPrice: t.mid, RawScore: open.EntryConf,
						EntryTime: open.EntryTime, EntryPattern: open.EntryPattern,
						MFE: mfe, MAE: mae, BarsSec: barsSec,
					})
					lastExitMs = t.ts
					open = nil
					exitPending = nil
				}
				continue
			}
			if bar == nil || sig == nil {
				continue
			}
			if open != nil {
				if t.mid > peak {
					peak = t.mid
				}
				if trough == 0 || t.mid < trough {
					trough = t.mid
				}
				exited := false
				exitReason := ""
				exitSide := "sell"
				if open.Side == vt.Sell {
					exitSide = "buy"
				}
				if vt.IsGridPattern(open.EntryPattern) {
					if ok, reason := eng.GridExit(symbol, *open, t.mid, t.ts); ok {
						exitReason = reason
						exited = true
					}
				}
				if !exited {
					if ok, reason := eng.EvalExit(symbol, *open, t.mid, t.ts); ok {
						exitReason = reason
						exited = true
					} else if (open.Side == vt.Buy && sig.Type == vt.Sell) ||
						(open.Side == vt.Sell && sig.Type == vt.Buy) {
						exitReason = "opposite_" + sig.Pattern
						exited = true
					}
				}
				// Maker出口（利確）：指値待機に切り替える（非グリッドのみ）。
				if exited && exitReason == "take_profit" && exitMaker && !vt.IsGridPattern(open.EntryPattern) {
					lim := open.EntryPrice * (1 + tpRatio)
					side := "sell"
					if open.Side == vt.Sell {
						lim = open.EntryPrice * (1 - tpRatio)
						side = "buy"
					}
					if lim > 0 {
						exitPending = &makerExitPending{side: side, limit: lim,
							until: t.ts + int64(exitMakerTimeoutMs), reason: "take_profit"}
						exited = false
					}
				}
				if exited {
					mfe, mae := paperExcursion(open, peak, trough)
					recs = append(recs, vtSignalRecord{
						Timestamp: t.ts, Symbol: symbol, Type: exitSide,
						Price: t.mid, Reason: exitReason, Pattern: "exit",
						Regime: open.EntryRegime, Confidence: open.EntryConf,
						RSI: open.EntryRSI, PercentB: open.EntryPB, MACDHist: open.EntryHist,
						Qty: open.Volume, MidPrice: t.mid, RawScore: open.EntryConf,
						EntryTime: open.EntryTime, EntryPattern: open.EntryPattern,
						MFE: mfe, MAE: mae,
						BarsSec: barsSec,
					})
					lastExitMs = t.ts
					open = nil
				}
				continue
			}
			if sig.Type != vt.Buy && sig.Type != vt.Sell {
				// trend が Hold のときだけグリッドを評価する（paper と同一）。
				if gridEnabled {
					gsig := eng.GridEntry(symbol, t.ts, t.spreadRatio)
					if gsig.Type == vt.Buy {
						okGate, _ := entryAllowed(gsig, minConf, shortRSIFloor)
						okCost, _ := costGateAllowed(eng.ATRRatio(symbol), t.spreadRatio, gateEntryFee, gateExitFee, costGateK)
						if okGate && okCost && !tobiitaBlocked(t.spreadRatio) && !cooldownBlocked(t.ts, lastExitMs, entryCooldownSec) {
							gqty := vtPaperQty(symbol, t.mid, gsig.Confidence, balance, gridRiskFraction, t.spreadRatio)
							if gqty > 0 {
								if volTarget > 0 {
									gqty *= volScale(eng.ATRRatio(symbol), volTarget)
								}
								recs = append(recs, vtSignalRecord{
									Timestamp: gsig.Timestamp, Symbol: symbol, Type: string(gsig.Type),
									Price: gsig.Price, Reason: gsig.Reason, Pattern: gsig.Pattern,
									Regime: gsig.Regime, Confidence: gsig.Confidence,
									RSI: gsig.RSI, PercentB: gsig.PercentB, MACDHist: gsig.MACDHist,
									Qty: gqty, MidPrice: gsig.Price, RawScore: gsig.Confidence,
									MMState: gsig.MMState,
									BarO:    gsig.BarO, BarH: gsig.BarH, BarL: gsig.BarL, BarV: gsig.BarV,
									BarsSec: barsSec,
								})
								open = &vt.OpenPos{Side: gsig.Type, EntryPrice: gsig.Price, EntryTime: gsig.Timestamp, Volume: gqty,
									EntryRegime: gsig.Regime, EntryPattern: gsig.Pattern,
									EntryRSI: gsig.RSI, EntryPB: gsig.PercentB, EntryHist: gsig.MACDHist, EntryConf: gsig.Confidence}
								peak, trough = gsig.Price, gsig.Price
							}
						}
					}
				}
				continue
			}
			// 入口ゲート（live/paper と同一。A/B 検証用にフラグ化）。
			if ok, _ := entryAllowed(sig, minConf, shortRSIFloor); !ok {
				continue
			}
			// 飛び板注意：live/paper と対称に見送る。
			if tobiitaBlocked(t.spreadRatio) {
				continue
			}
			// コスト・ゲート／回転抑制（live/paper と同一）。
			if ok, _ := costGateAllowed(eng.ATRRatio(symbol), t.spreadRatio, gateEntryFee, gateExitFee, costGateK); !ok {
				continue
			}
			if cooldownBlocked(t.ts, lastExitMs, entryCooldownSec) {
				continue
			}
			qty := vtPaperQty(symbol, t.mid, sig.Confidence, balance, riskFraction, t.spreadRatio)
			if volTarget > 0 && qty > 0 {
				qty *= volScale(eng.ATRRatio(symbol), volTarget)
			}
			// Maker検証：指値を置いて待つ（買いはbidタッチ、売りはaskタッチ）。
			// スプレッド不明時はmidを使う。最新シグナルで上書きする。
			// 逆選択抑制：吸収を確認できた押し目/戻りだけを指値待機にし、
			// 未確認は成行記録（従来）へ退化する。
			makerSide := 1
			if sig.Type == vt.Sell {
				makerSide = -1
			}
			if maker && (!makerRequireAbsorb || eng.AbsorbConfirmed(symbol, makerSide)) {
				lim := sig.Price
				if t.spreadRatio > 0 {
					if sig.Type == vt.Buy {
						lim = t.mid * (1 - t.spreadRatio/2)
					} else {
						lim = t.mid * (1 + t.spreadRatio/2)
					}
				}
				cp := *sig
				pending = &makerPending{side: sig.Type, limit: lim, qty: qty, sig: &cp, until: t.ts + makerTimeoutMs}
				continue
			}
			recs = append(recs, vtSignalRecord{
				Timestamp: sig.Timestamp, Symbol: symbol, Type: string(sig.Type),
				Price: sig.Price, Reason: sig.Reason, Pattern: sig.Pattern,
				Regime: sig.Regime, Confidence: sig.Confidence,
				RSI: sig.RSI, PercentB: sig.PercentB, MACDHist: sig.MACDHist,
				Qty: qty, MidPrice: sig.Price, RawScore: sig.Confidence,
				MMState: sig.MMState,
				BarO:    sig.BarO, BarH: sig.BarH, BarL: sig.BarL, BarV: sig.BarV,
				BarsSec: barsSec,
			})
			open = &vt.OpenPos{Side: sig.Type, EntryPrice: sig.Price, EntryTime: sig.Timestamp, Volume: qty,
				EntryRegime: sig.Regime, EntryPattern: sig.Pattern,
				EntryRSI: sig.RSI, EntryPB: sig.PercentB, EntryHist: sig.MACDHist, EntryConf: sig.Confidence}
			peak, trough = sig.Price, sig.Price
		}
	}
	return recs, files, nil
}

func (cli *CLI) vtReplay(args []string) error {
	kv := parseKeyValues(args)
	symbol := strings.ToUpper(strings.TrimSpace(kv["symbol"]))
	if symbol == "" {
		symbol = "BTC"
	}
	barsList := parseBarsList(kv["bars"])
	if len(barsList) != 1 {
		return fmt.Errorf("vt-replay: --bars must be a single value (60, 300, 900 or 3600)")
	}
	toDay := strings.TrimSpace(kv["to"])
	if toDay == "" {
		toDay = time.Now().Format("20060102")
	}
	fromDay := strings.TrimSpace(kv["from"])
	if fromDay == "" {
		fromDay = time.Now().Add(-30 * 24 * time.Hour).Format("20060102")
	}
	dir := orderbookLogDir()
	var shortAllow []string
	if raw, ok := kv["short-patterns"]; ok {
		shortAllow = vt.SplitPatterns(raw)
	}
	// 実証セル許可制の検証用（--allow-file）。検証と執行の乖離防止。
	var allowCells []vt.AllowCell
	if af := strings.TrimSpace(kv["allow-file"]); af != "" {
		if cells, err := loadAllowCells(af); err != nil {
			cli.logger.Info("vt-replay: allow file unreadable (%v); unrestricted", err)
		} else {
			allowCells = cells
		}
	}
	// 自動調整ファイル（--deny-file）。live の pattern-deny と同じ集合を適用し、
	// 検証と執行の乖離を防ぐ（vt_pattern_allow.json / vt_daily_deny.json 両対応）。
	var replayLongDeny, replayShortDeny []string
	if df := strings.TrimSpace(kv["deny-file"]); df != "" {
		if ld, sd, err := loadPatternDeny(df); err != nil {
			cli.logger.Info("vt-replay: deny file unreadable (%v); no deny", err)
		} else {
			replayLongDeny, replayShortDeny = ld, sd
		}
	}
	// ソフト出口の禁止（--exit-deny=rsi_weak,signal 等。空＝全許可）。
	var replayExitDeny []string
	for _, p := range strings.Split(kv["exit-deny"], ",") {
		if s := strings.TrimSpace(p); s != "" {
			replayExitDeny = append(replayExitDeny, s)
		}
	}
	// グリッド記録は既定で有効（paper と同一。記録のみで発注しないため安全）。
	gridEnabled := true
	if raw, ok := kv["grid-enabled"]; ok {
		if s := strings.TrimSpace(raw); strings.EqualFold(s, "false") || s == "0" {
			gridEnabled = false
		}
	}
	// Maker検証：入口を指値待機にする（手数料は分析側でleg別に計上）。
	maker := false
	if raw, ok := kv["maker"]; ok {
		if s := strings.TrimSpace(raw); !(strings.EqualFold(s, "false") || s == "0" || s == "") {
			maker = true
		}
	}
	// コスト・ゲート／回転抑制／ボラ数量／グリッド原価／Maker吸収条件。
	costGateFeeMode := strings.TrimSpace(kv["cost-gate-fee-mode"])
	if costGateFeeMode == "" {
		costGateFeeMode = "taker"
	}
	gateEntryFee, gateExitFee, gateModeOK := feeModeRates(costGateFeeMode)
	if !gateModeOK {
		gateEntryFee, gateExitFee = 0.0005, 0.0005
	}
	makerRequireAbsorb := maker
	if raw, ok := kv["maker-require-absorb"]; ok {
		s := strings.TrimSpace(raw)
		makerRequireAbsorb = !(strings.EqualFold(s, "false") || s == "0")
	}
	// 利確の指値待機（Maker出口）検証。--tp-ratio と併用する。
	exitMaker := false
	if raw, ok := kv["exit-maker"]; ok {
		s := strings.TrimSpace(raw)
		exitMaker = !(strings.EqualFold(s, "false") || s == "0" || s == "")
	}
	exitMakerTimeoutMs := parseFloatKV(kv, "exit-maker-timeout-sec", 120) * 1000
	recs, files, err := replaySymbol(dir, symbol, fromDay, toDay, barsList[0],
		int64(parseFloatKV(kv, "trend-tf", 3600)),
		int64(parseFloatKV(kv, "fast-tf", 60)),
		parseFloatKV(kv, "trail-activate", 0.015),
		parseFloatKV(kv, "trail-back", 0.008),
		parseFloatKV(kv, "balance-jpy", 100000),
		parseFloatKV(kv, "risk-fraction", 0.2),
		shortAllow,
		allowCells,
		parseFloatKV(kv, "min-confidence", 0),
		parseFloatKV(kv, "short-rsi-floor", 0),
		parseFloatKV(kv, "grid-risk-fraction", 0.05),
		gridEnabled,
		maker,
		parseFloatKV(kv, "cost-gate-k", 0),
		gateEntryFee, gateExitFee,
		parseFloatKV(kv, "entry-cooldown-sec", 0),
		parseFloatKV(kv, "vol-target", 0),
		parseFloatKV(kv, "grid-cost-rate", 0),
		makerRequireAbsorb,
		parseFloatKV(kv, "trail-activate-atr", 0),
		parseFloatKV(kv, "trail-back-atr", 0),
		parseFloatKV(kv, "sl-atr", 0),
		parseFloatKV(kv, "maker-cross-bps", 0),
		int(parseFloatKV(kv, "exit-confirm-votes", 0)),
		replayLongDeny, replayShortDeny, replayExitDeny,
		parseFloatKV(kv, "tp-ratio", 0), exitMaker, exitMakerTimeoutMs)
	if err != nil {
		return err
	}
	if out := strings.TrimSpace(kv["out"]); out != "" {
		f, err := os.Create(out)
		if err != nil {
			return fmt.Errorf("vt-replay: create out: %w", err)
		}
		enc := json.NewEncoder(f)
		for _, r := range recs {
			if err := enc.Encode(r); err != nil {
				f.Close()
				return fmt.Errorf("vt-replay: write out: %w", err)
			}
		}
		f.Close()
	}
	sum := summarizeVtRecords(recs, "replay", 0, files, "none")
	return printJSON(map[string]any{
		"status": "ok", "symbol": symbol, "bars": barsList[0],
		"from": fromDay, "to": toDay, "rows": len(recs), "summary": sum,
	})
}
