package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// vt_report.go は週次レポートを提供する（Pループ3・読み取り専用）。
//
// 完成基準・調整値・停止集合・建玉状態を1文書に束ね、人間が1分で読める
// 要約を出す。通知は cron 側（ログ＋SMTP設定時のみメール）が担う。
// 例: core vt-weekly-report --dir=/trade_logs --out=/trade_logs/vt_weekly_report.json

// vtWeeklyReport は週次レポートの形式。
type vtWeeklyReport struct {
	Date       string         `json:"date"`
	Accept     map[string]any `json:"acceptance"`
	Tune       map[string]any `json:"tune"`
	DenyWeekly map[string]any `json:"deny_weekly"`
	DenyDaily  map[string]any `json:"deny_daily"`
	Heartbeat  map[string]any `json:"heartbeat"`
	LiveFills7 int            `json:"live_fills_7d"`
	Mode       string         `json:"mode"`
}

func readJSONMap(path string) map[string]any {
	out := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil {
		out["missing"] = true
		return out
	}
	if err := json.Unmarshal(data, &out); err != nil {
		out["broken"] = true
		return out
	}
	return out
}

// countRecentFills は直近日数の trade_log filled 行数を数える。
func countRecentFills(dir string, days int) int {
	f, err := os.Open(filepath.Join(dir, "trade_log.csv"))
	if err != nil {
		return -1
	}
	defer f.Close()
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	n := 0
	r := csv.NewReader(f)
	rows, err := r.ReadAll()
	if err != nil {
		return -1
	}
	for i, row := range rows {
		if i == 0 || len(row) < 9 {
			continue
		}
		ts, err := time.Parse(time.RFC3339, strings.TrimSpace(row[0]))
		if err != nil || ts.Before(cutoff) {
			continue
		}
		if strings.TrimSpace(row[8]) == "filled" {
			n++
		}
	}
	return n
}

func (cli *CLI) vtWeeklyReport(args []string) error {
	kv := parseKeyValues(args)
	dir := kv["dir"]
	if dir == "" {
		dir = orderbookLogDir()
	}
	out := strings.TrimSpace(kv["out"])
	rep := vtWeeklyReport{
		Date:       vtTodayJST(),
		Accept:     readJSONMap(filepath.Join(dir, "vt_acceptance.json")),
		Tune:       readJSONMap(filepath.Join(dir, "vt_tune.json")),
		DenyWeekly: readJSONMap(filepath.Join(dir, "vt_pattern_allow.json")),
		DenyDaily:  readJSONMap(filepath.Join(dir, "vt_daily_deny.json")),
		Heartbeat:  readJSONMap(filepath.Join(dir, "vt_live_heartbeat.json")),
		LiveFills7: countRecentFills(dir, 7),
	}
	if m, ok := rep.Heartbeat["mode"].(string); ok {
		rep.Mode = m
	}
	pass, _ := rep.Accept["pass"].(bool)
	pf, _ := rep.Accept["pf"].(float64)
	conf, _ := rep.Tune["min_confidence"].(float64)
	cli.logger.Info("vt-weekly-report: mode=%s pass=%v pf=%.2f conf=%.2f fills7d=%d",
		rep.Mode, pass, pf, conf, rep.LiveFills7)
	if out != "" {
		data, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return fmt.Errorf("vt-weekly-report: marshal: %w", err)
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			return fmt.Errorf("vt-weekly-report: write: %w", err)
		}
	}
	return printJSON(rep)
}
