package main

import (
	"path/filepath"
	"testing"

	"github.com/cryptoEngine/core/domain"
	"github.com/cryptoEngine/core/infrastructure/logging"
)

// TestVtShadowTickColdStart は温まり前の何もしないことを確認する。
func TestVtShadowTickColdStart(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ORDERBOOK_LOG_DIR", dir)
	cli := &CLI{logger: logging.NewLogger()}
	ob := &domain.OrderBook{
		Symbol: "TST", MidPrice: 100, Spread: 0.01, Timestamp: 1700000000000,
		Bids: []domain.Order{{Price: 99.9, Volume: 1}},
		Asks: []domain.Order{{Price: 100.1, Volume: 1}},
	}
	cli.vtShadowTick("TST", ob, 5000, 0.2)
	// 温まり前は記録ファイルを作らない。
	matches, _ := filepath.Glob(filepath.Join(dir, "vt_shadow_*.jsonl"))
	if len(matches) != 0 {
		t.Fatalf("expected no shadow file during warmup, got %v", matches)
	}
	// nil・不正入力は無害。
	cli.vtShadowTick("TST", nil, 5000, 0.2)
	cli.vtShadowTick("TST", &domain.OrderBook{}, 5000, 0.2)
}

// TestVtShadowTickFires は十分な履歴で入口が記録されることを確認する。
// 上位足上昇＋入口足の赤三兵ドリフトで発火し、建玉と記録が残る。
func TestVtShadowTickFires(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ORDERBOOK_LOG_DIR", dir)
	cli := &CLI{logger: logging.NewLogger()}
	// 上位足（1時間）を上昇で種付けする（公開 Observe 経由）。
	var hts int64 = 1700000000000
	hp := 100.0
	for i := 0; i < 40; i++ {
		hp += 0.5
		cli.vtShadowEngine("TST").Observe("TST", hts, hp, 5, 5)
		hts += 3600000
	}
	// 入口足（5分）を35本分流す（往復揉み合い→終盤3本の赤三兵）。
	// RSIを過熱させないため終盤以外は方向を交互にする。
	var ts int64 = hts
	m := 120.0
	for i := 0; i < 35*60; i++ {
		barIdx := i / 60
		d := 0.005
		if barIdx < 32 && barIdx%2 == 1 {
			d = -0.005
		}
		m += d
		ob := &domain.OrderBook{
			Symbol: "TST", MidPrice: m, Spread: 0.1, Timestamp: ts,
			Bids: []domain.Order{{Price: m - 0.05, Volume: 1}},
			Asks: []domain.Order{{Price: m + 0.05, Volume: 1}},
		}
		ts += 5000
		cli.vtShadowTick("TST", ob, 5000, 0.2)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "vt_shadow_*.jsonl"))
	if len(matches) == 0 {
		t.Fatal("expected shadow record file after soldiers")
	}
	if _, ok := cli.vtOpen["TST"]; !ok {
		t.Fatal("expected open paper position after entry")
	}
}
