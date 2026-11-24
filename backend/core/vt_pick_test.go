package main

import (
	"testing"
)

// TestRankVtCandidates は効率順の優先付けを検証する。
func TestRankVtCandidates(t *testing.T) {
	now := int64(1_000_000_000)
	rows := []vtSignalRecord{
		{Timestamp: now - 60_000, Symbol: "AAA", Type: "buy", Price: 100, Pattern: "p1", Regime: "trend_up", Confidence: 0.6, PercentB: 0.9, Qty: 1, BarsSec: 300},
		{Timestamp: now - 60_000, Symbol: "BBB", Type: "buy", Price: 100, Pattern: "p2", Regime: "trend_up", Confidence: 0.6, PercentB: 0.5, Qty: 1, BarsSec: 300},
		{Timestamp: now - 60_000, Symbol: "CCC", Type: "hold", Price: 100, Pattern: "", Qty: 1, BarsSec: 300},
		{Timestamp: now - 3_600_000, Symbol: "OLD", Type: "buy", Price: 100, Pattern: "p3", Confidence: 0.9, PercentB: 0.9, Qty: 1, BarsSec: 300},
	}
	ranked := rankVtCandidates(rows, 5, now, 15*60*1000, 100000, 0.2)
	// hold と古い行は除外され、%B 乖離の大きい AAA が先頭。
	if len(ranked) != 2 {
		t.Fatalf("candidates = %d, want 2", len(ranked))
	}
	if ranked[0].Symbol != "AAA" || ranked[1].Symbol != "BBB" {
		t.Fatalf("wrong order: %+v", ranked)
	}
	if ranked[0].Score <= ranked[1].Score {
		t.Fatalf("scores not ordered: %v <= %v", ranked[0].Score, ranked[1].Score)
	}
}

func TestVtEfficiencyFeeSensitivity(t *testing.T) {
	a := vtEfficiency(0.6, 0.9, 5)
	b := vtEfficiency(0.6, 0.9, 10)
	if !(a > b && b > 0) {
		t.Fatalf("higher fee must lower score: %v vs %v", a, b)
	}
	if got := vtEfficiency(2, 0.9, 5); got != vtEfficiency(1, 0.9, 5) {
		t.Fatal("confidence must clamp to 1")
	}
}

func TestVtEfficiencyStretchCap(t *testing.T) {
	// 帯外の行き過ぎ（揉み合いの伸び切り追撃）を効率上位に置かない。
	// stretch は 1.0 で頭打ち。ロング・ショート対称（同一関数）。
	if got := vtEfficiency(0.6, 1.5, 5); got != vtEfficiency(0.6, 1.0, 5) {
		t.Fatalf("overextended stretch must cap at 1.0: %v", got)
	}
	if got := vtEfficiency(0.6, -1.0, 5); got != vtEfficiency(0.6, 2.0, 5) {
		t.Fatalf("negative side must mirror the cap: %v", got)
	}
	want := 0.6 * 0.9 / 5
	if got := vtEfficiency(0.6, 0.9, 5); got < want-1e-12 || got > want+1e-12 {
		t.Fatalf("in-band score must be unchanged: %v", got)
	}
}
