package main

import (
	"os"
	"strings"
	"time"

	vt "github.com/cryptoEngine/core/usecase/trading/vt"
)

// vt_why.go は「なぜ約定しないか」の診断器（読み取り専用・発注なし）。
//
// 銘柄×足種ごとに当日＋前日の板 CSV でウォームアップし、最新足の
// EvalEntry 結果（hold 理由・レジーム・指標値）をそのまま出す。
// 不発理由（warming up / low activity / against orderflow / no trigger /
// range 等）の分布で、パラメータ・データ・相場のどれが律速か切り分ける。
// 例: core vt-why --symbols=BTC,XRP --bars=60,300,3600

// vtWhyRow は1系列の診断結果。
type vtWhyRow struct {
	Symbol     string  `json:"symbol"`
	BarsSec    int64   `json:"bars_sec"`
	Type       string  `json:"type"`
	Reason     string  `json:"reason"`
	Pattern    string  `json:"pattern"`
	Regime     string  `json:"regime"`
	Confidence float64 `json:"confidence"`
	RSI        float64 `json:"rsi"`
	PercentB   float64 `json:"percent_b"`
	MACDHist   float64 `json:"macd_hist"`
	WarmTicks  int     `json:"warm_ticks"`
}

func (cli *CLI) vtWhy(args []string) error {
	kv := parseKeyValues(args)
	symbols := []string{}
	if raw := kv["symbols"]; raw != "" {
		for _, s := range strings.Split(raw, ",") {
			if s = strings.ToUpper(strings.TrimSpace(s)); s != "" {
				symbols = append(symbols, s)
			}
		}
	}
	if len(symbols) == 0 {
		symbols = []string{"BTC", "XRP", "XLM", "BCH", "LTC"}
	}
	barsList := parseBarsList(kv["bars"])
	if len(barsList) == 0 {
		barsList = []int64{60, 300, 3600}
	}
	trendTF := int64(parseFloatKV(kv, "trend-tf", 3600))
	dir := orderbookLogDir()
	nowMs := time.Now().UnixMilli()
	rows := []vtWhyRow{}
	for _, sym := range symbols {
		for _, b := range barsList {
			cfg := vt.DefaultConfig()
			cfg.BarsSec = b
			cfg.TrendTFSec = trendTF
			eng := vt.NewEngine(cfg)
			n := vtWarmup(eng, dir, sym, []int64{b}, trendTF)
			sig := eng.EvalEntry(sym, nowMs)
			row := vtWhyRow{Symbol: sym, BarsSec: b, WarmTicks: n}
			if sig == nil {
				row.Type = "(nil)"
				row.Reason = "no signal object"
			} else {
				row.Type = string(sig.Type)
				row.Reason = sig.Reason
				row.Pattern = sig.Pattern
				row.Regime = sig.Regime
				row.Confidence = sig.Confidence
				row.RSI = sig.RSI
				row.PercentB = sig.PercentB
				row.MACDHist = sig.MACDHist
			}
			rows = append(rows, row)
		}
	}
	if os.Getenv("VT_WHY_QUIET") == "" {
		cli.logger.Info("vt-why: diagnosed %d series", len(rows))
	}
	return printJSON(map[string]any{"status": "ok", "rows": rows})
}
