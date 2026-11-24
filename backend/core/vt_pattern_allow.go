package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	vt "github.com/cryptoEngine/core/usecase/trading/vt"
)

// vt_pattern_allow.go は入口パターンの自動調整を提供する（読み取り専用・発注なし）。
//
// paper 記録（vt_signals_*.jsonl）を入口→出口で対にし、方向別（long/short）
// のパターン成績から「負け越し確定」パターンを deny 集合として出力する。
// live/paper は起動時に --pattern-allow-file で読み込み、該当パターンの
// 新規入口だけを止める。出口の未加工反転判定には影響しない
// （入口の厳選と出口の確実性を分離する設計）。
// 例: core vt-pattern-allow --dir=/trade_logs --days=14 --bars=300 --min-pairs=5 --out=/trade_logs/vt_pattern_allow.json

// vtPatternDeny は自動調整ファイルの形式。
type vtPatternDeny struct {
	Date         string   `json:"date"`
	Days         int      `json:"days"`
	BarsSec      int64    `json:"bars_sec"`
	MinPairs     int      `json:"min_pairs"`
	BlockedLong  []string `json:"blocked_long"`
	BlockedShort []string `json:"blocked_short"`
	// Allowed は実証セル（手数料込みPF基準）。空＝無制限。
	Allowed []vtAllowOut `json:"allowed"`
}

// vtAllowOut は実証セルの1件。
type vtAllowOut struct {
	Side    string  `json:"side"`
	Pattern string  `json:"pattern"`
	Regime  string  `json:"regime"`
	Pairs   int     `json:"pairs"`
	PF      float64 `json:"pf"`
}

// patDirStat は方向付きパターン成績。
type patDirStat struct {
	pairs     int
	wins      int
	pnl       float64
	grossWin  float64
	grossLoss float64
}

// computeBlocked は方向別成績から除外集合を返す（純粋関数・テスト対象）。
// 標本不足（pairs < minPairs）は除外しない。勝率5割超または損益プラスの
// パターンは残す。負け越し確定（勝率5割以下かつ損益マイナス）のみ落とす。
// 攻めの入口を殺さないため、条件は緩め（疑わしきは残す）。
// 損益は手数料込み（pairDirectedStats で控除済み）。
func computeBlocked(stats map[string]*patDirStat, minPairs int) []string {
	var out []string
	for pat, st := range stats {
		if st.pairs < minPairs {
			continue
		}
		if st.wins*2 > st.pairs || st.pnl >= 0 {
			continue
		}
		out = append(out, pat)
	}
	sort.Strings(out)
	return out
}

// computeAllowed は実証セル（手数料込みPF基準）を返す（純粋関数・テスト対象）。
// pairs>=minPairs かつ PF>minPF のセルのみ。PF順にソートする。
func computeAllowed(stats map[string]*patDirStat, minPairs int, minPF float64) []vtAllowOut {
	var out []vtAllowOut
	for key, st := range stats {
		if st.pairs < minPairs || st.grossLoss <= 0 {
			continue
		}
		pf := st.grossWin / st.grossLoss
		if pf <= minPF {
			continue
		}
		parts := strings.SplitN(key, "\x00", 3)
		if len(parts) != 3 {
			continue
		}
		out = append(out, vtAllowOut{Side: parts[0], Pattern: parts[1], Regime: parts[2], Pairs: st.pairs, PF: pf})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PF > out[j].PF })
	return out
}

// pairDirectedStats は記録を方向別パターン成績に束ねる（純粋関数・テスト対象）。
// キーは "buy\x00<pattern>\x00<regime>" / "sell\x00<pattern>\x00<regime>"。
// 損益は手数料込み（feeRate、往復2回分×額面を決済時に控除）。
func pairDirectedStats(recs []vtSignalRecord, feeRate float64) map[string]*patDirStat {
	byKey := map[string][]vtSignalRecord{}
	for _, r := range recs {
		sym := strings.ToUpper(strings.TrimSpace(r.Symbol))
		if sym == "" {
			continue
		}
		r.Symbol = sym
		r.Type = strings.ToLower(strings.TrimSpace(r.Type))
		byKey[sym+"\x00"+strconv.FormatInt(r.BarsSec, 10)] = append(byKey[sym+"\x00"+strconv.FormatInt(r.BarsSec, 10)], r)
	}
	stats := map[string]*patDirStat{}
	for _, rs := range byKey {
		sort.Slice(rs, func(i, j int) bool { return rs[i].Timestamp < rs[j].Timestamp })
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
				fee := 2 * feeRate * open.Price * open.Qty
				net := pnl - fee
				key := open.Type + "\x00" + open.Pattern + "\x00" + open.Regime
				st := stats[key]
				if st == nil {
					st = &patDirStat{}
					stats[key] = st
				}
				st.pairs++
				if net > 0 {
					st.wins++
					st.grossWin += net
				} else {
					st.grossLoss -= net
				}
				st.pnl += net
				open = nil
				continue
			}
			if r.Type == "buy" || r.Type == "sell" {
				if r.Pattern == "" {
					continue
				}
				cp := r
				open = &cp
			}
		}
	}
	return stats
}

// splitDirected は方向別成績を long/short の集計に分ける。
// セルキー（side・pattern・regime）をパターン単位に合算する
// （deny はパターン単位の従来意味を維持するため）。
func splitDirected(stats map[string]*patDirStat) (long, short map[string]*patDirStat) {
	long, short = map[string]*patDirStat{}, map[string]*patDirStat{}
	add := func(dst map[string]*patDirStat, pat string, st *patDirStat) {
		a := dst[pat]
		if a == nil {
			a = &patDirStat{}
			dst[pat] = a
		}
		a.pairs += st.pairs
		a.wins += st.wins
		a.pnl += st.pnl
		a.grossWin += st.grossWin
		a.grossLoss += st.grossLoss
	}
	for k, st := range stats {
		parts := strings.SplitN(k, "\x00", 3)
		if len(parts) < 2 {
			continue
		}
		if parts[0] == "buy" {
			add(long, parts[1], st)
		} else if parts[0] == "sell" {
			add(short, parts[1], st)
		}
	}
	return long, short
}

// unionPatterns はdeny集合の和集合を返す（週次＋日次の統合用）。
// 空・重複を除きソートする（ログの安定表示のため）。
func unionPatterns(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, xs := range [][]string{a, b} {
		for _, p := range xs {
			if p = strings.TrimSpace(p); p != "" && !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	sort.Strings(out)
	return out
}

// unionAllowCells は実証セルの和集合を返す（週次＋日次の統合用）。
// side・pattern・regime の三つ組で重複除去し、PF降順にソートする。
func unionAllowCells(a, b []vt.AllowCell) []vt.AllowCell {
	seen := map[string]bool{}
	var out []vt.AllowCell
	for _, xs := range [][]vt.AllowCell{a, b} {
		for _, c := range xs {
			side := strings.ToLower(strings.TrimSpace(c.Side))
			pat := strings.TrimSpace(c.Pattern)
			reg := strings.TrimSpace(c.Regime)
			if (side != "buy" && side != "sell") || pat == "" || reg == "" {
				continue
			}
			k := side + "\x00" + pat + "\x00" + reg
			if !seen[k] {
				seen[k] = true
				out = append(out, vt.AllowCell{Side: side, Pattern: pat, Regime: reg})
			}
		}
	}
	return out
}

// loadPatternDeny は自動調整ファイルを読み、deny 集合を返す。
// 不在・破損時は空集合とエラーを返す（呼び出し側は既定動作に退化する）。
func loadPatternDeny(path string) (longDeny, shortDeny []string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var doc vtPatternDeny
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("vt-pattern-allow: parse %s: %w", path, err)
	}
	clean := func(xs []string) []string {
		var out []string
		for _, p := range xs {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return clean(doc.BlockedLong), clean(doc.BlockedShort), nil
}

// loadAllowCells は自動調整ファイルの実証セルを読み、エンジン用に変換する。
// 不在・破損時は空集合とエラーを返す（呼び出し側は無制限に退化する）。
func loadAllowCells(path string) ([]vt.AllowCell, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc vtPatternDeny
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("vt-pattern-allow: parse %s: %w", path, err)
	}
	var out []vt.AllowCell
	for _, a := range doc.Allowed {
		out = append(out, vt.AllowCell{Side: a.Side, Pattern: a.Pattern, Regime: a.Regime})
	}
	return out, nil
}

func (cli *CLI) vtPatternAllow(args []string) error {
	kv := parseKeyValues(args)
	dir := kv["dir"]
	if dir == "" {
		dir = orderbookLogDir()
	}
	days := int(parseFloatKV(kv, "days", 14))
	barsOnly := int64(parseFloatKV(kv, "bars", 300))
	minPairs := int(parseFloatKV(kv, "min-pairs", 5))
	if minPairs < 2 {
		minPairs = 2
	}
	feeRate := parseFloatKV(kv, "fee-rate", 0.0015)
	minPF := parseFloatKV(kv, "min-pf", 1.2)
	recs, _ := loadSignalRecords(dir, days, barsOnly, "signals")
	cells := pairDirectedStats(recs, feeRate)
	long, short := splitDirected(cells)
	doc := vtPatternDeny{
		Date:         vtTodayJST(),
		Days:         days,
		BarsSec:      barsOnly,
		MinPairs:     minPairs,
		BlockedLong:  computeBlocked(long, minPairs),
		BlockedShort: computeBlocked(short, minPairs),
		Allowed:      computeAllowed(cells, minPairs, minPF),
	}
	if doc.BlockedLong == nil {
		doc.BlockedLong = []string{}
	}
	if doc.BlockedShort == nil {
		doc.BlockedShort = []string{}
	}
	if doc.Allowed == nil {
		doc.Allowed = []vtAllowOut{}
	}
	if out := strings.TrimSpace(kv["out"]); out != "" {
		data, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return fmt.Errorf("vt-pattern-allow: marshal: %w", err)
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			return fmt.Errorf("vt-pattern-allow: write: %w", err)
		}
	}
	return printJSON(doc)
}
