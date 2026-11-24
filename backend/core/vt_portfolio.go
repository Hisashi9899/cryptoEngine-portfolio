package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// vt_portfolio.go は paper 記録のリプレイで同時保有上限の最適値を測る。
//
// 読み取り専用・発注なし。銘柄別の直列記録を時系列で束ね、枠数・日次停止を
// 掛けて約定可否を再現し、上限ごとの損益・回転・見送りを比較する。
// 例: core vt-portfolio --dir=/trade_logs --days=14 --bars=300

// vtPortResult は1上限値のシミュレーション結果。
type vtPortResult struct {
	MaxPos      int     `json:"max_pos"`
	Pairs       int     `json:"pairs"`
	Wins        int     `json:"wins"`
	Gross       float64 `json:"gross"`
	Fees        float64 `json:"fees"`
	Net         float64 `json:"net"`
	SkippedFull int     `json:"skipped_full"`
	SkippedHalt int     `json:"skipped_halt"`
	DaysHalted  int     `json:"days_halted"`
	WorstDay    float64 `json:"worst_day"`
	Unpaired    int     `json:"unpaired"`
}

// vtPortDay は JST 日付を返す（live の日次損益と同一区切り）。
func vtPortDay(ts int64) string {
	return time.UnixMilli(ts).UTC().Add(9 * time.Hour).Format("20060102")
}

type vtPortPos struct {
	side   string
	price  float64
	qty    float64
	reason string
}

// simulatePortfolio は記録を上限付きでリプレイする（純粋関数・テスト対象）。
// 手数料は往復 feeRate*2×額面で控除し、日次停止の判定は手数料込みで行う。
func simulatePortfolio(recs []vtSignalRecord, maxPos int, maxDaily, feeRate float64) vtPortResult {
	out := vtPortResult{MaxPos: maxPos}
	sorted := append([]vtSignalRecord(nil), recs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Timestamp < sorted[j].Timestamp })
	opens := map[string]vtPortPos{}
	daily := map[string]float64{}
	halted := map[string]bool{}
	for _, r := range sorted {
		sym := strings.ToUpper(strings.TrimSpace(r.Symbol))
		if sym == "" {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(r.Type))
		if r.Pattern == "exit" {
			op, ok := opens[sym]
			if !ok {
				continue
			}
			var pnl float64
			if op.side == "buy" {
				pnl = (r.Price - op.price) * op.qty
			} else if op.side == "sell" {
				pnl = (op.price - r.Price) * op.qty
			} else {
				delete(opens, sym)
				continue
			}
			fee := 2 * feeRate * op.price * op.qty
			out.Pairs++
			if pnl-fee > 0 {
				out.Wins++
			}
			out.Gross += pnl
			out.Fees += fee
			out.Net += pnl - fee
			day := vtPortDay(r.Timestamp)
			daily[day] += pnl - fee
			if daily[day] < out.WorstDay {
				out.WorstDay = daily[day]
			}
			if maxDaily > 0 && daily[day] <= -maxDaily {
				halted[day] = true
			}
			delete(opens, sym)
			continue
		}
		if typ != "buy" && typ != "sell" {
			continue
		}
		if r.Qty <= 0 || r.Price <= 0 {
			continue
		}
		if _, ok := opens[sym]; ok {
			continue
		}
		day := vtPortDay(r.Timestamp)
		if halted[day] {
			out.SkippedHalt++
			continue
		}
		if maxPos > 0 && len(opens) >= maxPos {
			out.SkippedFull++
			continue
		}
		opens[sym] = vtPortPos{side: typ, price: r.Price, qty: r.Qty}
	}
	out.Unpaired = len(opens)
	out.DaysHalted = len(halted)
	return out
}

// pairNetPnlFees は記録を往復ペアにし、手数料控除後の1往復損益列と
// 手数料合計を返す（P5完成基準のPF・信頼区間用。純粋関数・テスト対象）。
// ペアリング規則は simulatePortfolio と同一（銘柄別・exitパターンで決済）。
// 従来の対称手数料（入口価格基準・往復2倍）を維持する。--fee-rate 指定時の
// 既存の数値・テストを変えない（後方互換）。
func pairNetPnlFees(recs []vtSignalRecord, feeRate float64) ([]float64, float64) {
	sorted := append([]vtSignalRecord(nil), recs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Timestamp < sorted[j].Timestamp })
	opens := map[string]vtPortPos{}
	var out []float64
	fees := 0.0
	for _, r := range sorted {
		sym := strings.ToUpper(strings.TrimSpace(r.Symbol))
		if sym == "" {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(r.Type))
		if r.Pattern == "exit" {
			op, ok := opens[sym]
			if !ok {
				continue
			}
			var pnl float64
			if op.side == "buy" {
				pnl = (r.Price - op.price) * op.qty
			} else if op.side == "sell" {
				pnl = (op.price - r.Price) * op.qty
			} else {
				delete(opens, sym)
				continue
			}
			fee := 2 * feeRate * op.price * op.qty
			out = append(out, pnl-fee)
			fees += fee
			delete(opens, sym)
			continue
		}
		if typ != "buy" && typ != "sell" {
			continue
		}
		if r.Qty <= 0 || r.Price <= 0 {
			continue
		}
		if _, ok := opens[sym]; ok {
			continue
		}
		opens[sym] = vtPortPos{side: typ, price: r.Price, qty: r.Qty}
	}
	return out, fees
}

// pairNetPnlFeesRates は入口・出口で別々の片道手数料率を適用する版。
// Maker リベート（負の entryRate）や Taker 実勢（0.05%/片道）を正しく
// 反映し、受入判定が仮定0.3%固定で歪むのを防ぐ。負の手数料は利益加算。
func pairNetPnlFeesRates(recs []vtSignalRecord, entryRate, exitRate float64) ([]float64, float64) {
	sorted := append([]vtSignalRecord(nil), recs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Timestamp < sorted[j].Timestamp })
	opens := map[string]vtPortPos{}
	var out []float64
	fees := 0.0
	for _, r := range sorted {
		sym := strings.ToUpper(strings.TrimSpace(r.Symbol))
		if sym == "" {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(r.Type))
		if r.Pattern == "exit" {
			op, ok := opens[sym]
			if !ok {
				continue
			}
			var pnl float64
			if op.side == "buy" {
				pnl = (r.Price - op.price) * op.qty
			} else if op.side == "sell" {
				pnl = (op.price - r.Price) * op.qty
			} else {
				delete(opens, sym)
				continue
			}
			fee := entryRate*op.price*op.qty + exitRate*r.Price*op.qty
			out = append(out, pnl-fee)
			fees += fee
			delete(opens, sym)
			continue
		}
		if typ != "buy" && typ != "sell" {
			continue
		}
		if r.Qty <= 0 || r.Price <= 0 {
			continue
		}
		if _, ok := opens[sym]; ok {
			continue
		}
		opens[sym] = vtPortPos{side: typ, price: r.Price, qty: r.Qty, reason: r.Reason}
	}
	return out, fees
}

// dropPatterns は入口パターンが除外集合に該当する往復を丸ごと取り除く。
// 入口を落とした直後の同一銘柄の出口も落とし、誤対付けを防ぐ。
func dropPatterns(recs []vtSignalRecord, raw string) []vtSignalRecord {
	exclude := map[string]bool{}
	for _, s := range strings.Split(raw, ",") {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" {
			exclude[s] = true
		}
	}
	if len(exclude) == 0 {
		return recs
	}
	sorted := append([]vtSignalRecord(nil), recs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Timestamp < sorted[j].Timestamp })
	skip := map[string]bool{}
	out := make([]vtSignalRecord, 0, len(sorted))
	for _, r := range sorted {
		sym := strings.ToUpper(strings.TrimSpace(r.Symbol))
		if r.Pattern == "exit" {
			if skip[sym] {
				delete(skip, sym)
				continue
			}
			out = append(out, r)
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(r.Type))
		if typ != "buy" && typ != "sell" {
			out = append(out, r)
			continue
		}
		if exclude[strings.ToLower(strings.TrimSpace(r.Pattern))] {
			skip[sym] = true
			continue
		}
		out = append(out, r)
	}
	return out
}

// dropShortOversold は売り入口のRSIが下限未満の往復を除外する。
// live/paper の short-rsi-floor と同じ判定（買いには適用しない）。
func dropShortOversold(recs []vtSignalRecord, floor float64) []vtSignalRecord {
	if floor <= 0 {
		return recs
	}
	sorted := append([]vtSignalRecord(nil), recs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Timestamp < sorted[j].Timestamp })
	skip := map[string]bool{}
	out := make([]vtSignalRecord, 0, len(sorted))
	for _, r := range sorted {
		sym := strings.ToUpper(strings.TrimSpace(r.Symbol))
		if r.Pattern == "exit" {
			if skip[sym] {
				delete(skip, sym)
				continue
			}
			out = append(out, r)
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(r.Type))
		if typ != "buy" && typ != "sell" {
			out = append(out, r)
			continue
		}
		if typ == "sell" && r.RSI < floor {
			skip[sym] = true
			continue
		}
		out = append(out, r)
	}
	return out
}

// pairNetPnlFeesAuto は現物Makerが買い限定（売りは証拠金成行=taker）という
// 実装制約を反映する。買い入口=makerRate、売り入口=takerRate、出口は常にtakerRate。
func pairNetPnlFeesAuto(recs []vtSignalRecord, makerRate, takerRate float64) ([]float64, float64) {
	sorted := append([]vtSignalRecord(nil), recs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Timestamp < sorted[j].Timestamp })
	opens := map[string]vtPortPos{}
	var out []float64
	fees := 0.0
	for _, r := range sorted {
		sym := strings.ToUpper(strings.TrimSpace(r.Symbol))
		if sym == "" {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(r.Type))
		if r.Pattern == "exit" {
			op, ok := opens[sym]
			if !ok {
				continue
			}
			var pnl float64
			if op.side == "buy" {
				pnl = (r.Price - op.price) * op.qty
			} else if op.side == "sell" {
				pnl = (op.price - r.Price) * op.qty
			} else {
				delete(opens, sym)
				continue
			}
			entryRate := takerRate
			if op.side == "buy" {
				entryRate = makerRate
			}
			fee := entryRate*op.price*op.qty + takerRate*r.Price*op.qty
			out = append(out, pnl-fee)
			fees += fee
			delete(opens, sym)
			continue
		}
		if typ != "buy" && typ != "sell" {
			continue
		}
		if r.Qty <= 0 || r.Price <= 0 {
			continue
		}
		if _, ok := opens[sym]; ok {
			continue
		}
		opens[sym] = vtPortPos{side: typ, price: r.Price, qty: r.Qty, reason: r.Reason}
	}
	return out, fees
}

// pairNetPnlFeesPerLeg は脚ごとに Maker/Taker 率を判定して控除する。
// Reason に "/maker" を含む脚（指値待機で成立）は makerRate、他は takerRate。
// Maker入口＋（利確のみ）Maker出口の効果を実勢どおりに測るための分析用。
func pairNetPnlFeesPerLeg(recs []vtSignalRecord, makerRate, takerRate float64) ([]float64, float64) {
	sorted := append([]vtSignalRecord(nil), recs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Timestamp < sorted[j].Timestamp })
	opens := map[string]vtPortPos{}
	var out []float64
	fees := 0.0
	legRate := func(reason string) float64 {
		if strings.Contains(reason, "/maker") {
			return makerRate
		}
		return takerRate
	}
	for _, r := range sorted {
		sym := strings.ToUpper(strings.TrimSpace(r.Symbol))
		if sym == "" {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(r.Type))
		if r.Pattern == "exit" {
			op, ok := opens[sym]
			if !ok {
				continue
			}
			var pnl float64
			if op.side == "buy" {
				pnl = (r.Price - op.price) * op.qty
			} else if op.side == "sell" {
				pnl = (op.price - r.Price) * op.qty
			} else {
				delete(opens, sym)
				continue
			}
			fee := legRate(op.reason)*op.price*op.qty + legRate(r.Reason)*r.Price*op.qty
			out = append(out, pnl-fee)
			fees += fee
			delete(opens, sym)
			continue
		}
		if typ != "buy" && typ != "sell" {
			continue
		}
		if r.Qty <= 0 || r.Price <= 0 {
			continue
		}
		if _, ok := opens[sym]; ok {
			continue
		}
		opens[sym] = vtPortPos{side: typ, price: r.Price, qty: r.Qty, reason: r.Reason}
	}
	return out, fees
}

func (cli *CLI) vtPortfolio(args []string) error {
	kv := parseKeyValues(args)
	dir := kv["dir"]
	if dir == "" {
		dir = orderbookLogDir()
	}
	days := int(parseFloatKV(kv, "days", 14))
	barsOnly := int64(parseFloatKV(kv, "bars", 300))
	maxDaily := parseFloatKV(kv, "max-daily", 500)
	feeRate := parseFloatKV(kv, "fee-rate", 0.0005)
	excluded := parseBlockSymbols(kv["exclude"])
	maxList := []int64{}
	for _, raw := range strings.Split(kv["max-list"], ",") {
		var v int64
		if _, err := fmt.Sscanf(strings.TrimSpace(raw), "%d", &v); err == nil && v >= 1 && v <= 20 {
			maxList = append(maxList, v)
		}
	}
	if len(maxList) == 0 {
		maxList = []int64{1, 2, 3, 4, 5, 6}
	}
	recs, files := loadSignalRecords(dir, days, barsOnly, "signals")
	kept := recs[:0]
	for _, r := range recs {
		if excluded[strings.ToUpper(strings.TrimSpace(r.Symbol))] {
			continue
		}
		kept = append(kept, r)
	}
	recs = kept
	type row struct {
		MaxPos int64        `json:"max_pos"`
		Result vtPortResult `json:"result"`
	}
	rows := []row{}
	for _, m := range maxList {
		rows = append(rows, row{MaxPos: m, Result: simulatePortfolio(recs, int(m), maxDaily, feeRate)})
	}
	return printJSON(map[string]any{
		"status": "ok", "days": days, "bars": barsOnly, "files": files,
		"rows_total": len(recs), "max_daily": maxDaily, "fee_rate": feeRate,
		"results": rows,
	})
}
