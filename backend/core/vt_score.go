package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// vt_score.go は V.T記録（vt_signals_*/vt_shadow_* JSONL）の採点器。
//
// 読み取り専用・DB不要。1銘柄1建玉の記録を入口→出口の順で対にし、
// 勝率・損益・保有時間・パターン別/regime別/出口理由別の内訳を出す。
// V.T検証ループの計測器。例:
//   core vt-score --dir=/trade_logs --days=7 --src=signals
//   core vt-score --dir=/trade_logs --days=0 --src=all --out=/trade_logs/vt_score_weekly.json

type vtScorePair struct {
	Symbol     string
	Side       string
	Pattern    string
	Regime     string
	ExitReason string
	PnL        float64
	HoldSec    float64
	Win        bool
}

type vtScoreStat struct {
	Pairs   int     `json:"pairs"`
	Wins    int     `json:"wins"`
	PnL     float64 `json:"pnl"`
	AvgHold int64   `json:"avg_hold_sec"`
}

type vtScoreSummary struct {
	Total     vtScoreStat            `json:"total"`
	BySymbol  map[string]vtScoreStat `json:"by_symbol"`
	BySide    map[string]vtScoreStat `json:"by_side"`
	ByPattern map[string]vtScoreStat `json:"by_pattern"`
	ByRegime  map[string]vtScoreStat `json:"by_regime"`
	ByExit    map[string]vtScoreStat `json:"by_exit_reason"`
	ByBars    map[string]vtScoreStat `json:"by_bars"`
	Files     int                    `json:"files"`
	Rows      int                    `json:"rows"`
	Unpaired  int                    `json:"unpaired_entries"`
	Days      int                    `json:"days"`
	Source    string                 `json:"src"`
	FeeMode   string                 `json:"fee_mode"`
	Fees      float64                `json:"fees"`
}

// vtScoreFeeRates は fee-mode ごとの入口/出口手数料率を返す。
// auto は現物Makerが買い限定（売りは証拠金成行=taker）という実装制約を反映する。
func vtScoreFeeRates(mode, side string) (entry, exit float64) {
	switch mode {
	case "taker":
		return 0.0005, 0.0005
	case "maker":
		return -0.0001, 0.0005
	case "auto":
		if side == "buy" {
			return -0.0001, 0.0005
		}
		return 0.0005, 0.0005
	case "conservative":
		return 0.0015, 0.0015
	}
	return 0, 0
}

func vtScoreAdd(stat map[string]vtScoreStat, key string, p vtScorePair) {
	s := stat[key]
	s.Pairs++
	if p.Win {
		s.Wins++
	}
	s.PnL += p.PnL
	s.AvgHold += int64(p.HoldSec)
	stat[key] = s
}

func vtScoreRound4(v float64) float64 {
	if v >= 0 {
		return float64(int(v*10000+0.5)) / 10000
	}
	return float64(int(v*10000-0.5)) / 10000
}

func vtScoreFinalizeStat(stat map[string]vtScoreStat) {
	for k, s := range stat {
		if s.Pairs > 0 {
			s.AvgHold = s.AvgHold / int64(s.Pairs)
			s.PnL = vtScoreRound4(s.PnL)
		}
		stat[k] = s
	}
}

// summarizeVtRecords は記録行を（銘柄＋足種）別に時系列で対にし、集計する
// （純粋関数・テスト対象）。足種混在でも誤対付けしない。
func summarizeVtRecords(recs []vtSignalRecord, src string, days, files int, feeMode string) vtScoreSummary {
	bySym := map[string][]vtSignalRecord{}
	for _, r := range recs {
		sym := strings.ToUpper(strings.TrimSpace(r.Symbol))
		if sym == "" {
			continue
		}
		r.Symbol = sym
		r.Type = strings.ToLower(strings.TrimSpace(r.Type))
		bySym[sym+"\x00"+strconv.FormatInt(r.BarsSec, 10)] = append(bySym[sym+"\x00"+strconv.FormatInt(r.BarsSec, 10)], r)
	}
	sum := vtScoreSummary{
		BySymbol: map[string]vtScoreStat{}, BySide: map[string]vtScoreStat{},
		ByPattern: map[string]vtScoreStat{},
		ByRegime:  map[string]vtScoreStat{}, ByExit: map[string]vtScoreStat{},
		ByBars: map[string]vtScoreStat{},
		Files:  files, Rows: len(recs), Days: days, Source: src, FeeMode: feeMode,
	}
	for _, rs := range bySym {
		sort.Slice(rs, func(i, j int) bool { return rs[i].Timestamp < rs[j].Timestamp })
		sym := rs[0].Symbol
		barsLabel := strconv.FormatInt(rs[0].BarsSec, 10) + "s"
		var open *vtSignalRecord
		for _, r := range rs {
			r := r
			if r.Pattern == "exit" {
				if open == nil {
					continue
				}
				var pnl float64
				if open.Type == "buy" {
					pnl = (r.Price - open.Price) * open.Qty
				} else if open.Type == "sell" {
					pnl = (open.Price - r.Price) * open.Qty
				} else {
					open = nil
					continue
				}
				entryRate, exitRate := vtScoreFeeRates(feeMode, open.Type)
				fee := entryRate*open.Price*open.Qty + exitRate*r.Price*r.Qty
				net := pnl - fee
				sum.Fees += fee
				p := vtScorePair{Symbol: sym, Side: open.Type, Pattern: open.Pattern, Regime: open.Regime,
					ExitReason: r.Reason, PnL: net, HoldSec: float64(r.Timestamp-open.Timestamp) / 1000.0, Win: net > 0}
				sum.Total.Pairs++
				if p.Win {
					sum.Total.Wins++
				}
				sum.Total.PnL += p.PnL
				sum.Total.AvgHold += int64(p.HoldSec)
				vtScoreAdd(sum.BySymbol, sym, p)
				vtScoreAdd(sum.BySide, p.Side, p)
				pat := open.Pattern
				if pat == "" {
					pat = "(unknown)"
				}
				vtScoreAdd(sum.ByPattern, pat, p)
				reg := open.Regime
				if reg == "" {
					reg = "(unknown)"
				}
				vtScoreAdd(sum.ByRegime, reg, p)
				rsn := r.Reason
				if rsn == "" {
					rsn = "(unknown)"
				}
				vtScoreAdd(sum.ByExit, rsn, p)
				vtScoreAdd(sum.ByBars, barsLabel, p)
				open = nil
				continue
			}
			if r.Type == "buy" || r.Type == "sell" {
				if open != nil {
					sum.Unpaired++
				}
				open = &r
			}
		}
		if open != nil {
			sum.Unpaired++
		}
	}
	if sum.Total.Pairs > 0 {
		sum.Total.AvgHold = sum.Total.AvgHold / int64(sum.Total.Pairs)
		sum.Total.PnL = vtScoreRound4(sum.Total.PnL)
	}
	vtScoreFinalizeStat(sum.BySymbol)
	vtScoreFinalizeStat(sum.BySide)
	vtScoreFinalizeStat(sum.ByPattern)
	vtScoreFinalizeStat(sum.ByRegime)
	vtScoreFinalizeStat(sum.ByExit)
	vtScoreFinalizeStat(sum.ByBars)
	return sum
}

func (cli *CLI) vtScore(args []string) error {
	kv := parseKeyValues(args)
	dir := kv["dir"]
	if dir == "" {
		dir = orderbookLogDir()
	}
	days := int(parseFloatKV(kv, "days", 7))
	barsOnly := int64(parseFloatKV(kv, "bars", 0))
	src := strings.ToLower(strings.TrimSpace(kv["src"]))
	if src == "" {
		src = "signals"
	}
	if src != "signals" && src != "shadow" && src != "all" {
		return fmt.Errorf("vt-score: --src must be signals|shadow|all")
	}
	feeMode := strings.ToLower(strings.TrimSpace(kv["fee-mode"]))
	if feeMode == "" {
		feeMode = "none"
	}
	switch feeMode {
	case "none", "taker", "maker", "auto", "conservative":
	default:
		return fmt.Errorf("vt-score: --fee-mode must be none|taker|maker|auto|conservative")
	}
	recs, files := loadSignalRecords(dir, days, barsOnly, src)
	if syms := parseSymbolSet(kv["symbols"]); len(syms) > 0 {
		recs = filterRecordsBySymbol(recs, syms)
	}
	sum := summarizeVtRecords(recs, src, days, files, feeMode)
	sum.Fees = vtScoreRound4(sum.Fees)
	if out := kv["out"]; out != "" {
		data, err := json.MarshalIndent(sum, "", "  ")
		if err != nil {
			return fmt.Errorf("vt-score: marshal: %w", err)
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			return fmt.Errorf("vt-score: write: %w", err)
		}
	}
	return printJSON(sum)
}
