package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// vt_acceptance.go はP5完成基準の合否判定を提供する（読み取り専用・発注なし）。
//
// paper 記録を往復ペアにし、手数料込みPFとブートストラップ95%信頼区間から
// 完成基準の合否を出す。合格するまで実弾は寝かせ、合格したら触らない。
// 例: core vt-acceptance --dir=/trade_logs --days=14 --bars=300 --fee-rate=0.0015 --min-pairs=200 --min-pf=1.2 --out=/trade_logs/vt_acceptance.json

// vtAcceptResult は合否判定の出力。
type vtAcceptResult struct {
	Pairs     int     `json:"pairs"`
	Wins      int     `json:"wins"`
	WinRate   float64 `json:"winrate"`
	GrossWin  float64 `json:"gross_win"`
	GrossLoss float64 `json:"gross_loss"`
	PF        float64 `json:"pf"`
	PFLow     float64 `json:"pf_ci_low"`
	PFHigh    float64 `json:"pf_ci_high"`
	Net       float64 `json:"net"`
	Fees      float64 `json:"fees"`
	FeeMode   string  `json:"fee_mode"`
	EntryFee  float64 `json:"entry_fee_rate"`
	ExitFee   float64 `json:"exit_fee_rate"`
	MinPairs  int     `json:"min_pairs"`
	MinPF     float64 `json:"min_pf"`
	Pass      bool    `json:"pass"`
	FailWhy   string  `json:"fail_why"`
}

// acceptBootstrapN は信頼区間のリサンプル回数。
const acceptBootstrapN = 2000

// profitFactor は net 損益列のPF（勝ち合計/負け絶対合計）を返す。
// 負けなしは +Inf、往復なしは 0。
func profitFactor(nets []float64) float64 {
	win, loss := 0.0, 0.0
	for _, v := range nets {
		if v > 0 {
			win += v
		} else {
			loss -= v
		}
	}
	if len(nets) == 0 {
		return 0
	}
	if loss <= 0 {
		return 1e9
	}
	return win / loss
}

// pfCI はブートストラップでPFの95%信頼区間を返す（seed固定で再現性あり）。
func pfCI(nets []float64, seed int64) (low, high float64) {
	n := len(nets)
	if n == 0 {
		return 0, 0
	}
	rng := rand.New(rand.NewSource(seed))
	pfs := make([]float64, 0, acceptBootstrapN)
	for b := 0; b < acceptBootstrapN; b++ {
		win, loss := 0.0, 0.0
		for i := 0; i < n; i++ {
			v := nets[rng.Intn(n)]
			if v > 0 {
				win += v
			} else {
				loss -= v
			}
		}
		if loss <= 0 {
			pfs = append(pfs, 1e9)
		} else {
			pfs = append(pfs, win/loss)
		}
	}
	sort.Float64s(pfs)
	return pfs[acceptBootstrapN*25/1000], pfs[acceptBootstrapN*975/1000-1]
}

// judgeAcceptance は完成基準の合否を出す（純粋関数・テスト対象）。
// 基準: pairs>=minPairs かつ PF>minPF かつ CI下限>1.0。
func judgeAcceptance(nets []float64, feeRate, minPF float64, minPairs int) vtAcceptResult {
	res := vtAcceptResult{Pairs: len(nets), MinPairs: minPairs, MinPF: minPF}
	win, loss, net := 0.0, 0.0, 0.0
	for _, v := range nets {
		net += v
		if v > 0 {
			res.Wins++
			win += v
		} else {
			loss -= v
		}
	}
	res.Net = net
	res.GrossWin = win
	res.GrossLoss = loss
	if len(nets) > 0 {
		res.WinRate = float64(res.Wins) / float64(len(nets))
	}
	res.PF = profitFactor(nets)
	res.PFLow, res.PFHigh = pfCI(nets, 7)
	_ = feeRate
	switch {
	case len(nets) < minPairs:
		res.FailWhy = "pairs"
	case res.PF <= minPF:
		res.FailWhy = "pf"
	case res.PFLow <= 1.0:
		res.FailWhy = "ci"
	default:
		res.Pass = true
	}
	return res
}

// dryrunTrade は vt-live の dry-run open/close から復元した往復1件。
type dryrunTrade struct {
	symbol string
	entry  float64
	qty    float64
	exit   float64
	pnl    float64 // グロス（手数料前）
	maker  bool    // 入口が Maker 約定したか
}

// loadDryrunTrades は /trade_logs/vt_live_dryrun_*.jsonl を読み、open→close を
// 往復に復元する（読み取り専用）。実弾の前向き検証（dry-run）を採点するため。
func loadDryrunTrades(dir string, days int) []dryrunTrade {
	cutoff := int64(0)
	if days > 0 {
		cutoff = time.Now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
	}
	files, _ := filepath.Glob(filepath.Join(dir, "vt_live_dryrun_*.jsonl"))
	sort.Strings(files)
	type pos struct {
		entry float64
		qty   float64
		maker bool
	}
	open := map[string]pos{}
	var trades []dryrunTrade
	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1024*1024), 1024*1024)
		for sc.Scan() {
			line := bytes.TrimSpace(sc.Bytes())
			if len(line) == 0 {
				continue
			}
			var r struct {
				Action string  `json:"action"`
				Symbol string  `json:"symbol"`
				Price  float64 `json:"price"`
				Qty    float64 `json:"qty"`
				Pnl    float64 `json:"pnl"`
				Maker  bool    `json:"maker"`
				Ts     int64   `json:"ts"`
			}
			if err := json.Unmarshal(line, &r); err != nil {
				continue
			}
			if cutoff > 0 && r.Ts < cutoff {
				continue
			}
			sym := strings.ToUpper(strings.TrimSpace(r.Symbol))
			switch r.Action {
			case "open":
				open[sym] = pos{entry: r.Price, qty: r.Qty, maker: r.Maker}
			case "close":
				p, ok := open[sym]
				if !ok {
					continue
				}
				trades = append(trades, dryrunTrade{symbol: sym, entry: p.entry, qty: p.qty,
					exit: r.Price, pnl: r.Pnl, maker: p.maker})
				delete(open, sym)
			}
		}
		f.Close()
	}
	return trades
}

// dryrunFeeRates は手数料モードごとの入口/出口率を返す（Maker入口の実測を反映）。
func dryrunFeeRates(mode string, makerEntry bool) (float64, float64) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "maker":
		return -0.0001, 0.0005
	case "taker":
		return 0.0005, 0.0005
	case "perleg", "auto":
		if makerEntry {
			return -0.0001, 0.0005
		}
		return 0.0005, 0.0005
	default: // conservative
		return 0.0015, 0.0015
	}
}

// dryrunNets は往復列に手数料を適用し、net損益列と手数料合計を返す（純粋関数）。
// 入口は Maker 時に rebate（負のコスト）、出口は taker。
func dryrunNets(trades []dryrunTrade, mode string) (nets []float64, fees float64) {
	for _, t := range trades {
		e, x := dryrunFeeRates(mode, t.maker)
		cost := e*t.entry*t.qty + x*t.exit*t.qty
		nets = append(nets, t.pnl-cost)
		fees += cost
	}
	return nets, fees
}

func filterDryrunBySymbol(trades []dryrunTrade, symbols map[string]bool) []dryrunTrade {
	if len(symbols) == 0 {
		return trades
	}
	out := trades[:0]
	for _, t := range trades {
		if symbols[t.symbol] {
			out = append(out, t)
		}
	}
	return out
}

func (cli *CLI) vtAcceptance(args []string) error {
	kv := parseKeyValues(args)
	dir := kv["dir"]
	if dir == "" {
		dir = orderbookLogDir()
	}
	days := int(parseFloatKV(kv, "days", 14))
	barsOnly := int64(parseFloatKV(kv, "bars", 300))
	feeRate := parseFloatKV(kv, "fee-rate", 0.0015)
	minPairs := int(parseFloatKV(kv, "min-pairs", 200))
	minPF := parseFloatKV(kv, "min-pf", 1.2)
	// 手数料モード：明示時は実勢の片道率で上書きする（従来 --fee-rate は
	// conservative の対称率としてそのまま使える）。
	entryFee, exitFee, feeMode := feeRate, feeRate, "conservative"
	if mode := strings.TrimSpace(kv["fee-mode"]); mode != "" {
		if strings.EqualFold(mode, "perleg") {
			feeMode = "perleg"
		} else if strings.EqualFold(mode, "auto") {
			feeMode = "auto"
			entryFee, exitFee = -0.0001, 0.0005
		} else if e, x, ok := feeModeRates(mode); ok {
			entryFee, exitFee, feeMode = e, x, strings.ToLower(strings.TrimSpace(mode))
		} else {
			cli.logger.Info("vt-acceptance: unknown fee-mode %q; using fee-rate=%.5f", mode, feeRate)
		}
	}
	// perleg：脚ごとに Maker/Taker を判定する（Maker入口＋利確Maker出口の実測）。
	perLeg := strings.EqualFold(feeMode, "perleg")
	if perLeg {
		entryFee, exitFee = -0.0001, 0.0005
	}
	var nets []float64
	var fees float64
	if strings.EqualFold(strings.TrimSpace(kv["src"]), "dryrun") {
		// 実弾ドライランの前向き検証：open/close を往復に復元して採点する。
		// 入口の maker フラグを perleg で反映する（Maker入口は rebate）。
		trades := loadDryrunTrades(dir, days)
		if syms := parseSymbolSet(kv["symbols"]); len(syms) > 0 {
			trades = filterDryrunBySymbol(trades, syms)
		}
		nets, fees = dryrunNets(trades, feeMode)
	} else {
		recs, _ := loadSignalRecords(dir, days, barsOnly, "signals")
		// 少数の流動性銘柄（ライブ対象）だけで判定するための任意フィルタ。
		if syms := parseSymbolSet(kv["symbols"]); len(syms) > 0 {
			recs = filterRecordsBySymbol(recs, syms)
		}
		// 負けパターンの寄与分析用：入口パターンで往復を除外する。
		if ex := strings.TrimSpace(kv["exclude-patterns"]); ex != "" {
			recs = dropPatterns(recs, ex)
		}
		// live の short-rsi-floor と同一の売られ過ぎショート除外。
		if f := parseFloatKV(kv, "short-rsi-floor", 0); f > 0 {
			recs = dropShortOversold(recs, f)
		}
		if perLeg {
			nets, fees = pairNetPnlFeesPerLeg(recs, -0.0001, 0.0005)
		} else if strings.EqualFold(feeMode, "auto") {
			nets, fees = pairNetPnlFeesAuto(recs, -0.0001, 0.0005)
		} else {
			nets, fees = pairNetPnlFeesRates(recs, entryFee, exitFee)
		}
	}
	res := judgeAcceptance(nets, feeRate, minPF, minPairs)
	res.Fees = fees
	res.FeeMode = feeMode
	res.EntryFee = entryFee
	res.ExitFee = exitFee
	if out := strings.TrimSpace(kv["out"]); out != "" {
		data, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return fmt.Errorf("vt-acceptance: marshal: %w", err)
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			return fmt.Errorf("vt-acceptance: write: %w", err)
		}
	}
	cli.logger.Info("vt-acceptance: pairs=%d winrate=%.1f%% pf=%.2f ci=[%.2f,%.2f] net=%.1f fees=%.1f(%s %+.4f/%+.4f) pass=%v (%s)",
		res.Pairs, res.WinRate*100, res.PF, res.PFLow, res.PFHigh, res.Net, res.Fees,
		res.FeeMode, res.EntryFee, res.ExitFee, res.Pass, res.FailWhy)
	return printJSON(res)
}
