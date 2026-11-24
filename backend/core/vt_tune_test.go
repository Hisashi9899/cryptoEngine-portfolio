package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAdjustConfidence(t *testing.T) {
	// 負け越しで引き締め、勝ち越しで緩和、標本なし・中間は維持。
	eq := func(a, b float64) bool {
		d := a - b
		return d < 1e-9 && d > -1e-9
	}
	if got := adjustConfidence(0.58, 0.8, 10); !eq(got, 0.60) {
		t.Fatalf("losing must tighten, got %v", got)
	}
	if got := adjustConfidence(0.58, 1.8, 10); !eq(got, 0.57) {
		t.Fatalf("winning must relax, got %v", got)
	}
	if got := adjustConfidence(0.58, 1.2, 10); !eq(got, 0.58) {
		t.Fatalf("neutral must hold, got %v", got)
	}
	if got := adjustConfidence(0.58, 0.5, 0); !eq(got, 0.58) {
		t.Fatalf("thin sample must hold, got %v", got)
	}
	// 上下限で止まる。
	if got := adjustConfidence(0.70, 0.5, 10); !eq(got, 0.70) {
		t.Fatalf("cap must hold, got %v", got)
	}
	if got := adjustConfidence(0.50, 2.0, 10); !eq(got, 0.50) {
		t.Fatalf("floor must hold, got %v", got)
	}
}

func TestLoadTune(t *testing.T) {
	// 不在時は既定値、範囲外は既定値に丸める。
	d := loadTune(filepath.Join(t.TempDir(), "missing.json"))
	if d.MinConfidence != 0.58 || d.ExitConfirmVotes != 2 {
		t.Fatalf("bad defaults: %+v", d)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "vt_tune.json")
	doc := `{"date":"20261005","min_confidence":9.99,"exit_confirm_votes":99,"wall_firm_min":0.7,"exp_cooldown_bars":24,"exp_progress_min":0.005,"spoof_halt_ms":1800000}`
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	got := loadTune(path)
	if got.MinConfidence != 0.58 || got.ExitConfirmVotes != 2 {
		t.Fatalf("wild values must clamp to defaults: %+v", got)
	}
	if got.WallFirmMin != 0.7 || got.ExpCooldownBars != 24 {
		t.Fatalf("in-range values must load: %+v", got)
	}
}
