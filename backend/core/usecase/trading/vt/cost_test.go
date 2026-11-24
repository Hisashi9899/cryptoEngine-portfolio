package vt

import "testing"

func TestATRRatio(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BarsSec = 300
	eng := NewEngine(cfg)
	if got := eng.ATRRatio("TST"); got != 0 {
		t.Fatalf("insufficient bars must be 0, got %v", got)
	}
	// 17スロット分を交互の値動きで供給し、確定足を16本つくる。
	for i := 0; i < 17; i++ {
		ts := int64(i) * 300000
		p := 100.0
		if i%2 == 1 {
			p = 101.0
		}
		// 各足に2tick入れ、レンジ（ATR）を作る。1tickだとH=L=CでATR=0になる。
		eng.Observe("TST", ts, p, 1, 9)
		eng.Observe("TST", ts+1000, p+1, 1, 9)
	}
	got := eng.ATRRatio("TST")
	if got <= 0 {
		t.Fatalf("ATRRatio must be positive with bars, got %v", got)
	}
	if got > 1 {
		t.Fatalf("ATRRatio implausibly large: %v", got)
	}
}

func TestAbsorbConfirmedNoData(t *testing.T) {
	eng := NewEngine(DefaultConfig())
	if eng.AbsorbConfirmed("TST", 1) {
		t.Fatal("no data must not confirm absorption")
	}
	if eng.AbsorbConfirmed("TST", -1) {
		t.Fatal("no data must not confirm absorption (sell)")
	}
	if eng.AbsorbConfirmed("TST", 0) {
		t.Fatal("side 0 must be false")
	}
}
