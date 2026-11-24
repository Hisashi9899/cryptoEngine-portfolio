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

// vt_pick.go は複数銘柄の同時シグナルに優先順位を付ける（記録専用・発注なし）。
//
// 資金効率・手数料を加味した効率スコアでランク付けし、最効率の候補から
// 約定する判断材料を出す。効率の定義:
//   効率 = 信頼度 × 伸びしろ / 手数料率
// 伸びしろ = 0.5 + |percent_b - 0.5|（バンド端への偏りが大きいほど
// 伸び余地が大きいと読む）。予測の的中保証ではなく、記録値の序列化である。
//
// 例: core vt-pick --dir=/trade_logs --bars=300 --within-min=15 \
//       --symbols=BTC,XRP,XLM,BCH,LTC --balance-jpy=100000 --risk-fraction=0.2 --fee-bps=5

// vtRanked は1候補の採点結果。
type vtRanked struct {
	Symbol     string  `json:"symbol"`
	Type       string  `json:"type"`
	Price      float64 `json:"price"`
	Pattern    string  `json:"pattern"`
	Regime     string  `json:"regime"`
	Confidence float64 `json:"confidence"`
	Score      float64 `json:"score"`
	Qty        float64 `json:"qty"`
	Timestamp  int64   `json:"ts"`
	BarsSec    int64   `json:"bars_sec"`
}

// vtEfficiency は効率スコアを返す。feeBps<=0 は 5bps 扱い。
// stretch（バンド端への偏り）は 1.0 で頭打ちにする。帯外の行き過ぎ
// （揉み合いの往復で狩られる伸び切り追撃）を効率上位に置かないため。
// ロング・ショート対称（同一関数を両sideに使う）。
func vtEfficiency(conf, percentB, feeBps float64) float64 {
	if conf < 0 {
		conf = 0
	}
	if conf > 1 {
		conf = 1
	}
	if feeBps <= 0 {
		feeBps = 5
	}
	stretch := 0.5 + (percentB - 0.5)
	if stretch < 0 {
		stretch = -stretch
	}
	if stretch > 1.0 {
		stretch = 1.0
	}
	return conf * stretch / feeBps
}

// rankVtCandidates は新鮮な入口シグナルを効率順に並べる（純粋関数・テスト対象）。
func rankVtCandidates(rows []vtSignalRecord, feeBps float64, nowMs, withinMs int64, balance, riskFraction float64) []vtRanked {
	out := []vtRanked{}
	for _, r := range rows {
		typ := strings.ToLower(strings.TrimSpace(r.Type))
		if typ != "buy" && typ != "sell" {
			continue
		}
		if r.Pattern == "exit" || r.Pattern == "" {
			continue
		}
		if withinMs > 0 && nowMs-r.Timestamp > withinMs {
			continue
		}
		if r.Price <= 0 {
			continue
		}
		sym := strings.ToUpper(strings.TrimSpace(r.Symbol))
		out = append(out, vtRanked{
			Symbol: sym, Type: typ, Price: r.Price,
			Pattern: r.Pattern, Regime: r.Regime, Confidence: r.Confidence,
			Score:     vtEfficiency(r.Confidence, r.PercentB, feeBps),
			Qty:       vtPaperQty(sym, r.Price, r.Confidence, balance, riskFraction, 0),
			Timestamp: r.Timestamp, BarsSec: r.BarsSec,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].Confidence > out[j].Confidence
		}
		return out[i].Score > out[j].Score
	})
	return out
}

func (cli *CLI) vtPick(args []string) error {
	kv := parseKeyValues(args)
	dir := kv["dir"]
	if dir == "" {
		dir = orderbookLogDir()
	}
	barsOnly := int64(parseFloatKV(kv, "bars", 300))
	withinMin := parseFloatKV(kv, "within-min", 15)
	balance := parseFloatKV(kv, "balance-jpy", 100000)
	riskFraction := parseFloatKV(kv, "risk-fraction", 0.2)
	feeBps := parseFloatKV(kv, "fee-bps", 5)
	symbols := map[string]bool{}
	if raw := kv["symbols"]; raw != "" {
		for _, s := range strings.Split(raw, ",") {
			if s = strings.ToUpper(strings.TrimSpace(s)); s != "" {
				symbols[s] = true
			}
		}
	}
	nowMs := time.Now().UnixMilli()
	withinMs := int64(withinMin * 60 * 1000)
	var rows []vtSignalRecord
	files := 0
	for _, path := range globVtFiles(dir, "vt_signals_*.jsonl") {
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
			if len(symbols) > 0 && !symbols[strings.ToUpper(strings.TrimSpace(r.Symbol))] {
				continue
			}
			if barsOnly > 0 && r.BarsSec != barsOnly {
				continue
			}
			n++
			rows = append(rows, r)
		}
		f.Close()
		if n > 0 {
			files++
		}
	}
	ranked := rankVtCandidates(rows, feeBps, nowMs, withinMs, balance, riskFraction)
	return printJSON(map[string]any{
		"status": "ok", "candidates": ranked, "files": files, "rows": len(rows),
		"within_min": withinMin, "bars": barsOnly,
	})
}

// globVtFiles は dir 内の一致ファイルをソートして返す。
func globVtFiles(dir, pattern string) []string {
	matches, _ := filepath.Glob(filepath.Join(dir, pattern))
	sort.Strings(matches)
	return matches
}
