package main

import "testing"

func TestMakerCrossed(t *testing.T) {
	// crossBps=0 は従来のタッチ約定。
	if !makerCrossed("buy", 100, 100, 0) {
		t.Fatal("touch buy must fill at limit")
	}
	if makerCrossed("buy", 100.01, 100, 0) {
		t.Fatal("buy above limit must not fill")
	}
	if !makerCrossed("sell", 100, 100, 0) {
		t.Fatal("touch sell must fill at limit")
	}
	// crossBps=10（0.1%）は貫通を要求する。
	if makerCrossed("buy", 100, 100, 10) {
		t.Fatal("touch must not fill when cross required")
	}
	if !makerCrossed("buy", 99.89, 100, 10) {
		t.Fatal("cross-through buy must fill")
	}
	if !makerCrossed("sell", 100.11, 100, 10) {
		t.Fatal("cross-through sell must fill")
	}
	// 不正入力は偽。
	if makerCrossed("buy", 0, 100, 0) || makerCrossed("buy", 100, 0, 0) {
		t.Fatal("non-positive mid/limit must be false")
	}
}

func TestFilterRecordsBySymbol(t *testing.T) {
	recs := []vtSignalRecord{
		{Symbol: "BTC"}, {Symbol: "eth"}, {Symbol: "XRP"}, {Symbol: "DOGE"},
	}
	// 空集合は全通過。
	if got := filterRecordsBySymbol(recs, parseSymbolSet("")); len(got) != 4 {
		t.Fatalf("empty filter must pass all, got %d", len(got))
	}
	// 大文字小文字を吸収して絞る。
	got := filterRecordsBySymbol(recs, parseSymbolSet("btc, XRP"))
	if len(got) != 2 || got[0].Symbol != "BTC" || got[1].Symbol != "XRP" {
		t.Fatalf("filter = %v, want BTC,XRP", got)
	}
}
