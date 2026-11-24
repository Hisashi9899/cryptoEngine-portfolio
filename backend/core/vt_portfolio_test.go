package main

import (
	"testing"
	"time"
)

func portBase() int64 {
	return time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC).UnixMilli()
}

func portEntry(sym, typ string, price, qty float64, ts int64) vtSignalRecord {
	return vtSignalRecord{Timestamp: ts, Symbol: sym, Type: typ, Price: price,
		Pattern: "morning_star", Regime: "trend_up", Confidence: 0.65, Qty: qty, BarsSec: 300}
}

func portExit(sym string, price float64, ts int64) vtSignalRecord {
	return vtSignalRecord{Timestamp: ts, Symbol: sym, Type: "sell", Price: price,
		Pattern: "exit", Reason: "signal", Qty: 1, BarsSec: 300}
}

// TestPairNetPnlFeesPerLeg は脚ごとの Maker/Taker 判定を検証する。
func TestPairNetPnlFeesPerLeg(t *testing.T) {
	base := portBase()
	mkEntry := vtSignalRecord{Timestamp: base, Symbol: "A", Type: "buy", Price: 100,
		Pattern: "morning_star", Reason: "pullback/maker", Qty: 1, BarsSec: 300}
	mkExit := vtSignalRecord{Timestamp: base + 3600000, Symbol: "A", Type: "sell", Price: 102,
		Pattern: "exit", Reason: "take_profit/maker", Qty: 1, BarsSec: 300}
	nets, _ := pairNetPnlFeesPerLeg([]vtSignalRecord{mkEntry, mkExit}, -0.0001, 0.0005)
	// pnl=2。fee=maker*100 + maker*102 = -0.0100 -0.0102 = -0.0202（rebate）。
	want := 2.0 - (-0.0001*100 + -0.0001*102)
	if len(nets) != 1 || nets[0] < want-1e-9 || nets[0] > want+1e-9 {
		t.Fatalf("perleg maker maker = %v, want %v", nets, want)
	}
	tkExit := vtSignalRecord{Timestamp: base + 3600000, Symbol: "A", Type: "sell", Price: 102,
		Pattern: "exit", Reason: "signal", Qty: 1, BarsSec: 300}
	nets, _ = pairNetPnlFeesPerLeg([]vtSignalRecord{mkEntry, tkExit}, -0.0001, 0.0005)
	want = 2.0 - (-0.0001*100 + 0.0005*102)
	if len(nets) != 1 || nets[0] < want-1e-9 || nets[0] > want+1e-9 {
		t.Fatalf("perleg maker taker = %v, want %v", nets, want)
	}
}

// TestSimulateRespectsMaxPos は枠超過の見送りを検証する。
func TestSimulateRespectsMaxPos(t *testing.T) {
	base := portBase()
	h := int64(3600 * 1000)
	recs := []vtSignalRecord{
		portEntry("A", "buy", 100, 1, base),
		portEntry("B", "buy", 200, 1, base+30*60*1000),
		portExit("A", 110, base+h),
		portExit("B", 210, base+90*60*1000),
	}
	got := simulatePortfolio(recs, 1, 500, 0)
	if got.Pairs != 1 || got.SkippedFull != 1 {
		t.Fatalf("maxPos=1: pairs=%d skipped=%d, want 1/1", got.Pairs, got.SkippedFull)
	}
	got = simulatePortfolio(recs, 2, 500, 0)
	if got.Pairs != 2 || got.SkippedFull != 0 {
		t.Fatalf("maxPos=2: pairs=%d skipped=%d, want 2/0", got.Pairs, got.SkippedFull)
	}
}

// TestSimulateDailyHalt は日次停止を検証する。
func TestSimulateDailyHalt(t *testing.T) {
	base := portBase()
	h := int64(3600 * 1000)
	recs := []vtSignalRecord{
		portEntry("A", "buy", 100, 1, base),
		portExit("A", 80, base+h),
		portEntry("B", "buy", 200, 1, base+2*h),
		portExit("B", 210, base+3*h),
	}
	got := simulatePortfolio(recs, 5, 10, 0)
	if got.Pairs != 1 || got.SkippedHalt != 1 || got.DaysHalted != 1 {
		t.Fatalf("halt: pairs=%d skippedHalt=%d days=%d, want 1/1/1",
			got.Pairs, got.SkippedHalt, got.DaysHalted)
	}
	if got.WorstDay != -20 {
		t.Fatalf("worst day = %v, want -20", got.WorstDay)
	}
}

// TestSimulateFees は手数料控除を検証する。
func TestSimulateFees(t *testing.T) {
	base := portBase()
	recs := []vtSignalRecord{
		portEntry("A", "buy", 100, 2, base),
		portExit("A", 110, base+3600*1000),
	}
	got := simulatePortfolio(recs, 5, 500, 0.0005)
	if got.Gross != 20 || got.Fees != 0.2 || got.Net != 19.8 {
		t.Fatalf("gross/fees/net = %v/%v/%v, want 20/0.2/19.8", got.Gross, got.Fees, got.Net)
	}
	if got.Wins != 1 {
		t.Fatalf("wins = %d, want 1", got.Wins)
	}
}

func TestPairNetPnlFees(t *testing.T) {
	// 往復ペアの手数料込み損益列と手数料合計。未決済は落とす。
	base := portBase()
	recs := []vtSignalRecord{
		portEntry("A", "buy", 100, 1, base),
		portExit("A", 110, base+1000),
		portEntry("B", "buy", 100, 2, base+2000),
		portExit("B", 95, base+3000),
		portEntry("C", "buy", 100, 1, base+4000),
	}
	nets, fees := pairNetPnlFees(recs, 0.0005)
	if len(nets) != 2 {
		t.Fatalf("pairs = %d, want 2", len(nets))
	}
	// A: +10-0.1=+9.9、B: -10-0.2=-10.2。手数料合計0.3。
	eq := func(a, b float64) bool {
		d := a - b
		return d < 1e-9 && d > -1e-9
	}
	if !eq(nets[0], 9.9) || !eq(nets[1], -10.2) {
		t.Fatalf("nets = %v, want [9.9 -10.2]", nets)
	}
	if !eq(fees, 0.3) {
		t.Fatalf("fees = %v, want 0.3", fees)
	}
}

func TestJudgeAcceptance(t *testing.T) {
	// 標本不足・PF不足・CI不足は不合格、全部満たせば合格。
	mk := func(wins, losses int, wAmt, lAmt float64) []float64 {
		var out []float64
		for i := 0; i < wins; i++ {
			out = append(out, wAmt)
		}
		for i := 0; i < losses; i++ {
			out = append(out, -lAmt)
		}
		return out
	}
	if r := judgeAcceptance(mk(135, 45, 2, 1), 0, 1.2, 200); r.Pass || r.FailWhy != "pairs" {
		t.Fatalf("thin sample must fail on pairs: %+v", r)
	}
	// 200往復・PF3.0だが分散大→CI下限を確認する（ここでは合格するはず）。
	if r := judgeAcceptance(mk(150, 50, 2, 1), 0, 1.2, 200); !r.Pass {
		t.Fatalf("strong edge must pass: %+v", r)
	}
	// PF不足（勝ち1×100、負け1×100 → PF1.0）。
	if r := judgeAcceptance(mk(100, 100, 1, 1), 0, 1.2, 200); r.Pass || r.FailWhy != "pf" {
		t.Fatalf("weak edge must fail on pf: %+v", r)
	}
	// PFは1.2超だがギリギリ（CI下限が1.0以下になりうる）。
	// 決定論的seedのため結果は固定。fail理由がciかpassのいずれかであること。
	r := judgeAcceptance(mk(120, 80, 1.1, 1), 0, 1.2, 200)
	t.Logf("borderline: pass=%v why=%s pf=%.3f ci=[%.3f,%.3f]", r.Pass, r.FailWhy, r.PF, r.PFLow, r.PFHigh)
	if r.PF <= 1.2 {
		t.Fatalf("setup must clear pf gate: %.3f", r.PF)
	}
}
