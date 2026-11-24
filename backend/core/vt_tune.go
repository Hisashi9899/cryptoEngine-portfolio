package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	vt "github.com/cryptoEngine/core/usecase/trading/vt"
)

// vt_tune.go は夜間チューナーの調整ファイルとCLIを提供する（Pループ1）。
//
// /trade_logs/vt_tune.json に安全ノブを保持し、vtlive は mtime 変化で
// 再起動なしに反映する。チューナー v1 が動かすのは min_confidence のみ
// （前日の paper PF に連動）。他のノブは外部化済みで将来の規則・手動用。
// 例: core vt-tune --dir=/trade_logs --days=1 --bars=300 --fee-rate=0.0015 --out=/trade_logs/vt_tune.json

// vtTune は調整ファイルの形式。
type vtTune struct {
	Date             string  `json:"date"`
	MinConfidence    float64 `json:"min_confidence"`
	ExitConfirmVotes int     `json:"exit_confirm_votes"`
	WallFirmMin      float64 `json:"wall_firm_min"`
	ExpCooldownBars  int     `json:"exp_cooldown_bars"`
	ExpProgressMin   float64 `json:"exp_progress_min"`
	SpoofHaltMs      int64   `json:"spoof_halt_ms"`
}

// tuneBounds は安全範囲。
const (
	tuneConfMin = 0.50
	tuneConfMax = 0.70
	tuneConfUp  = 0.02
	tuneConfDn  = 0.01
)

// defaultTune は既定の調整値。
func defaultTune() vtTune {
	return vtTune{
		Date:             vtTodayJST(),
		MinConfidence:    0.58,
		ExitConfirmVotes: 2,
		WallFirmMin:      vt.WallFirmMinDefault,
		ExpCooldownBars:  48,
		ExpProgressMin:   0.003,
		SpoofHaltMs:      3600000,
	}
}

// loadTune は調整ファイルを読み、なければ既定値を返す。
func loadTune(path string) vtTune {
	d := defaultTune()
	data, err := os.ReadFile(path)
	if err != nil {
		return d
	}
	var doc vtTune
	if err := json.Unmarshal(data, &doc); err != nil {
		return d
	}
	if doc.MinConfidence >= tuneConfMin && doc.MinConfidence <= tuneConfMax {
		d.MinConfidence = doc.MinConfidence
	}
	if doc.ExitConfirmVotes >= 0 && doc.ExitConfirmVotes <= 4 {
		d.ExitConfirmVotes = doc.ExitConfirmVotes
	}
	if doc.WallFirmMin >= 0.2 && doc.WallFirmMin <= 0.8 {
		d.WallFirmMin = doc.WallFirmMin
	}
	if doc.ExpCooldownBars >= 12 && doc.ExpCooldownBars <= 144 {
		d.ExpCooldownBars = doc.ExpCooldownBars
	}
	if doc.ExpProgressMin >= 0.001 && doc.ExpProgressMin <= 0.01 {
		d.ExpProgressMin = doc.ExpProgressMin
	}
	if doc.SpoofHaltMs >= 900000 && doc.SpoofHaltMs <= 14400000 {
		d.SpoofHaltMs = doc.SpoofHaltMs
	}
	return d
}

// tuneToParams は調整値をエンジンパラメータに変換する。
func tuneToParams(t vtTune) vt.TuneParams {
	return vt.TuneParams{
		ExitConfirmVotes: t.ExitConfirmVotes,
		WallFirmMin:      t.WallFirmMin,
		ExpCooldownBars:  t.ExpCooldownBars,
		ExpProgressMin:   t.ExpProgressMin,
		SpoofHaltMs:      t.SpoofHaltMs,
	}
}

// adjustConfidence は前日PF連動の確信度調整（純粋関数・テスト対象）。
// PF<1.0で引き締め、PF>1.5で緩和、それ以外は維持。標本なしは維持。
func adjustConfidence(cur, pf float64, pairs int) float64 {
	if pairs <= 0 {
		return cur
	}
	if pf < 1.0 && cur < tuneConfMax {
		cur += tuneConfUp
		if cur > tuneConfMax {
			cur = tuneConfMax
		}
		return cur
	}
	if pf > 1.5 && cur > tuneConfMin {
		cur -= tuneConfDn
		if cur < tuneConfMin {
			cur = tuneConfMin
		}
		return cur
	}
	return cur
}

func (cli *CLI) vtTune(args []string) error {
	kv := parseKeyValues(args)
	dir := kv["dir"]
	if dir == "" {
		dir = orderbookLogDir()
	}
	days := int(parseFloatKV(kv, "days", 1))
	barsOnly := int64(parseFloatKV(kv, "bars", 300))
	feeRate := parseFloatKV(kv, "fee-rate", 0.0015)
	out := strings.TrimSpace(kv["out"])
	if out == "" {
		return fmt.Errorf("vt-tune: --out required")
	}
	tune := loadTune(out)
	recs, _ := loadSignalRecords(dir, days, barsOnly, "signals")
	nets, _ := pairNetPnlFees(recs, feeRate)
	pf := profitFactor(nets)
	tune.Date = vtTodayJST()
	tune.MinConfidence = adjustConfidence(tune.MinConfidence, pf, len(nets))
	data, err := json.MarshalIndent(tune, "", "  ")
	if err != nil {
		return fmt.Errorf("vt-tune: marshal: %w", err)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return fmt.Errorf("vt-tune: write: %w", err)
	}
	cli.logger.Info("vt-tune: pairs=%d pf=%.2f min_conf=%.2f -> %s", len(nets), pf, tune.MinConfidence, out)
	return printJSON(tune)
}
