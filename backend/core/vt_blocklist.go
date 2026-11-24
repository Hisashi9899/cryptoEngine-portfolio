package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// vt_blocklist.go は実弾の負け銘柄除外を提供する（読み取り専用・発注なし）。
//
// paper 記録（vt_signals_*.jsonl）の採点を読み、負け越し銘柄を live 候補から
// 外す。paper は全銘柄を記録し続け、live だけが除外する（検証ループ維持）。
// 例: core vt-live --symbols=BTC,XRP --block-days=7 --block-min-pairs=5

// parseBlockSymbols は "SUI,WILD" を集合に変換する（純粋関数・テスト対象）。
func parseBlockSymbols(raw string) map[string]bool {
	out := map[string]bool{}
	for _, s := range strings.Split(raw, ",") {
		if s = strings.ToUpper(strings.TrimSpace(s)); s != "" {
			out[s] = true
		}
	}
	return out
}

// loserBlocklist は paper 採点で負け越しの銘柄集合を返す（純粋関数・テスト対象）。
// Pairs 未満の薄い系列は除外する（少数約定での調整禁止の運用に合わせる）。
func loserBlocklist(sum vtScoreSummary, minPairs int) map[string]bool {
	out := map[string]bool{}
	for sym, st := range sum.BySymbol {
		if st.Pairs >= minPairs && st.PnL < 0 {
			out[sym] = true
		}
	}
	return out
}

// loadSignalRecords は JSONL 記録行を読み出す（vt-score と live の共有部）。
func loadSignalRecords(dir string, days int, barsOnly int64, src string) ([]vtSignalRecord, int) {
	patterns := []string{}
	if src == "signals" || src == "all" {
		patterns = append(patterns, "vt_signals_*.jsonl")
	}
	if src == "shadow" || src == "all" {
		patterns = append(patterns, "vt_shadow_*.jsonl")
	}
	cutoff := int64(0)
	if days > 0 {
		cutoff = time.Now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
	}
	var recs []vtSignalRecord
	files := 0
	for _, pat := range patterns {
		matches, _ := filepath.Glob(filepath.Join(dir, pat))
		sort.Strings(matches)
		for _, path := range matches {
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 1024*1024), 1024*1024)
			n := 0
			for sc.Scan() {
				line := strings.TrimSpace(sc.Text())
				if line == "" {
					continue
				}
				var r vtSignalRecord
				if err := json.Unmarshal([]byte(line), &r); err != nil {
					continue
				}
				if r.Timestamp < cutoff {
					continue
				}
				if barsOnly > 0 && r.BarsSec != barsOnly {
					continue
				}
				n++
				recs = append(recs, r)
			}
			f.Close()
			if n > 0 {
				files++
			}
		}
	}
	return recs, files
}

// parseSymbolSet は "BTC,ETH,..." を大文字の集合にする（空は空集合＝無制限）。
func parseSymbolSet(raw string) map[string]bool {
	out := map[string]bool{}
	for _, s := range strings.Split(raw, ",") {
		s = strings.ToUpper(strings.TrimSpace(s))
		if s != "" {
			out[s] = true
		}
	}
	return out
}

// filterRecordsBySymbol は銘柄集合で記録を絞る（空集合は全通過）。
// 少数の流動性銘柄だけで受入判定するために使う。
func filterRecordsBySymbol(recs []vtSignalRecord, symbols map[string]bool) []vtSignalRecord {
	if len(symbols) == 0 {
		return recs
	}
	out := recs[:0]
	for _, r := range recs {
		if symbols[strings.ToUpper(strings.TrimSpace(r.Symbol))] {
			out = append(out, r)
		}
	}
	return out
}

// sortedKeys は集合を表示用に安定ソートして返す。
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// mergeDailyPnl は銘柄別退避ファイルの日次損益を全体値に統合する。
// 各ファイルが全体値を保持するため加算は禁止（銘柄数倍になる）。
func mergeDailyPnl(daily map[string]float64, today string, v float64) {
	cur, ok := daily[today]
	if !ok || v > cur {
		daily[today] = v
	}
}

// scaleDailyLoss は日次損失上限を残高連動で換算する（純粋関数・テスト対象）。
// ratio<=0 のときは固定額 fallback をそのまま返す。
func scaleDailyLoss(balance, ratio, fallback float64) float64 {
	if ratio > 0 && balance > 0 {
		if scaled := balance * ratio; scaled > 0 {
			return scaled
		}
	}
	return fallback
}

// balanceChanged は残高の有意な変化（±1%以上）を判定する（純粋関数）。
// 微差でのログ・再計算の連発を防ぐ。
func balanceChanged(old, new float64) bool {
	if old <= 0 {
		return new > 0
	}
	diff := new - old
	if diff < 0 {
		diff = -diff
	}
	return diff/old >= 0.01
}

// tickStale は最終 tick から thresholdSec 超が経過したかを返す（純粋関数）。
// 未受信（zero）は停滞扱いにする。
func tickStale(last time.Time, now time.Time, thresholdSec float64) bool {
	if thresholdSec <= 0 {
		return false
	}
	if last.IsZero() {
		return true
	}
	return now.Sub(last).Seconds() > thresholdSec
}

// refreshBlocklist は手動指定と paper 負け越しの和集合から許可分を除いて返す。
// live 足（300秒）に絞って採点する。記録なしでは手動分のみ返す。
func refreshBlocklist(dir string, days, minPairs int, manual, allow map[string]bool) map[string]bool {
	out := map[string]bool{}
	for s := range manual {
		out[s] = true
	}
	recs, files := loadSignalRecords(dir, days, 300, "signals")
	if len(recs) > 0 {
		sum := summarizeVtRecords(recs, "signals", days, files, "none")
		for s := range loserBlocklist(sum, minPairs) {
			out[s] = true
		}
	}
	for s := range allow {
		delete(out, s)
	}
	return out
}
