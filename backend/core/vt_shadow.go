package main

import (
	"encoding/json"
	"os"
	"strconv"
	"time"

	"github.com/cryptoEngine/core/domain"
	vt "github.com/cryptoEngine/core/usecase/trading/vt"
)

// vt_shadow.go は trader 内 V.Tシャドー評価を提供する。
//
// v2 本番判定とは独立に、全17銘柄へ毎tickテクニカル判定を回し、
// 入口・出口を vt_shadow_<SYM>_<日>.jsonl に残す。執行・較正還流・
// risk には一切触らない。人間が添削するための判定記録である。
// テープ（CVD・吸収）は cron 側 vt-paper が担い、ここでは板不均衡
// ゲートまで（fail-open のため欠測でも安全）。

// vtShadowEngine は銘柄別の V.Tエンジンを返す（遅延生成）。
func (cli *CLI) vtShadowEngine(symbol string) *vt.Engine {
	if cli.vtEngines == nil {
		cli.vtEngines = map[string]*vt.Engine{}
	}
	eng, ok := cli.vtEngines[symbol]
	if !ok {
		cfg := vt.DefaultConfig()
		eng = vt.NewEngine(cfg)
		cli.vtEngines[symbol] = eng
	}
	return eng
}

// vtShadowPath はシャドー記録先を返す。足種ごとに分ける。
func vtShadowPath(dir, symbol string, barsSec int64, day string) string {
	return dir + "/vt_shadow_" + symbol + "_" + strconv.FormatInt(barsSec, 10) + "_" + day + ".jsonl"
}

// vtShadowAppend は1行を追記する（単一 write で原子性確保）。
func vtShadowAppend(path string, rec vtSignalRecord) {
	line, err := json.Marshal(rec)
	if err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(string(line) + "\n")
}

// vtShadowTick は1銘柄分のシャドー評価を行う。OrderBook から板出来高を
// 合計し、V.T判定の入口・出口を記録する。balance<0 の時は個数 0 で記録する。
func (cli *CLI) vtShadowTick(symbol string, ob *domain.OrderBook, balance, riskFraction float64) {
	if ob == nil || ob.MidPrice <= 0 {
		return
	}
	if cli.vtOpen == nil {
		cli.vtOpen = map[string]vt.OpenPos{}
	}
	eng := cli.vtShadowEngine(symbol)
	bidVol, askVol := 0.0, 0.0
	for _, o := range ob.Bids {
		bidVol += o.Volume
	}
	for _, o := range ob.Asks {
		askVol += o.Volume
	}
	ts := ob.Timestamp
	if ts <= 0 {
		ts = time.Now().UnixMilli()
	}
	sig, bar := eng.Observe(symbol, ts, ob.MidPrice, bidVol, askVol)
	if bar == nil || sig == nil {
		return
	}
	day := time.UnixMilli(ts).Format("20060102")
	path := vtShadowPath(orderbookLogDir(), symbol, vt.DefaultConfig().BarsSec, day)
	if open, ok := cli.vtOpen[symbol]; ok {
		exitSide := "sell"
		if open.Side == vt.Sell {
			exitSide = "buy"
		}
		exitReason := ""
		if ok, reason := eng.EvalExit(symbol, open, ob.MidPrice, ts); ok {
			exitReason = reason
		} else if (open.Side == vt.Buy && sig.Type == vt.Sell) ||
			(open.Side == vt.Sell && sig.Type == vt.Buy) {
			exitReason = "opposite_" + sig.Pattern
		}
		if exitReason != "" {
			vtShadowAppend(path, vtSignalRecord{
				Timestamp: ts, Symbol: symbol, Type: exitSide,
				Price: ob.MidPrice, Reason: exitReason, Pattern: "exit",
				Regime: open.EntryRegime, Confidence: open.EntryConf,
				RSI: open.EntryRSI, PercentB: open.EntryPB, MACDHist: open.EntryHist,
				Qty: open.Volume, MidPrice: ob.MidPrice, RawScore: open.EntryConf,
				EntryTime: open.EntryTime, EntryPattern: open.EntryPattern,
				BarsSec: vt.DefaultConfig().BarsSec,
			})
			delete(cli.vtOpen, symbol)
		}
		return
	}
	if sig.Type != vt.Buy && sig.Type != vt.Sell {
		return
	}
	qty := vtPaperQty(symbol, sig.Price, sig.Confidence, balance, riskFraction, 0)
	vtShadowAppend(path, vtSignalRecord{
		Timestamp: sig.Timestamp, Symbol: symbol, Type: string(sig.Type),
		Price: sig.Price, Reason: sig.Reason, Pattern: sig.Pattern,
		Regime: sig.Regime, Confidence: sig.Confidence,
		RSI: sig.RSI, PercentB: sig.PercentB, MACDHist: sig.MACDHist,
		Qty: qty, MidPrice: sig.Price, RawScore: sig.Confidence,
		MMState: sig.MMState,
		BarO:    sig.BarO, BarH: sig.BarH, BarL: sig.BarL, BarV: sig.BarV,
		BarsSec: vt.DefaultConfig().BarsSec,
	})
	cli.vtOpen[symbol] = vt.OpenPos{Side: sig.Type, EntryPrice: sig.Price, EntryTime: sig.Timestamp, Volume: qty,
		EntryRegime: sig.Regime, EntryPattern: sig.Pattern,
		EntryRSI: sig.RSI, EntryPB: sig.PercentB, EntryHist: sig.MACDHist, EntryConf: sig.Confidence}
}
