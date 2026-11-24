package backtest

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// writeCSVMids は合成板 CSV（ts 1秒刻み・mid 線形）を書く。
func writeCSVMids(t *testing.T, dir, symbol string, startTs int64, mids []float64) {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("timestamp,midPrice,spread,bids,asks\n")
	for i, m := range mids {
		ts := startTs + int64(i)*1000
		sb.WriteString(strconv.FormatInt(ts, 10) + "," + strconv.FormatFloat(m, 'f', 4, 64) + ",1.0," +
			"[{\"price\":" + strconv.FormatFloat(m-0.5, 'f', 4, 64) + ",\"volume\":5}]" + "," +
			"[{\"price\":" + strconv.FormatFloat(m+0.5, 'f', 4, 64) + ",\"volume\":5}]\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "orderbook_"+symbol+"_20260917.csv"), []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// gzipFixture は同一内容の .csv と .csv.gz を置く（後者は別日名）。
func gzipFixture(t *testing.T, dir string) (plain, gzipped string) {
	t.Helper()
	mids := make([]float64, 120)
	for i := range mids {
		mids[i] = 1000.0 + float64(i)*0.5
	}
	writeCSVMids(t, dir, "BTC", 1_700_000_000_000, mids)
	plain = filepath.Join(dir, "orderbook_BTC_20260917.csv")
	raw, err := os.ReadFile(plain)
	if err != nil {
		t.Fatal(err)
	}
	gzipped = filepath.Join(dir, "orderbook_BTC_20260918.csv.gz")
	f, err := os.Create(gzipped)
	if err != nil {
		t.Fatal(err)
	}
	w := gzip.NewWriter(f)
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return plain, gzipped
}

func TestGlobSeriesFindsBoth(t *testing.T) {
	dir := t.TempDir()
	plain, gzipped := gzipFixture(t, dir)
	got := GlobSeries(dir, "orderbook_BTC_")
	if len(got) != 2 || got[0] != plain || got[1] != gzipped {
		t.Fatalf("expected [plain gz], got %v", got)
	}
}

func TestLoadSeriesFilesGzParity(t *testing.T) {
	dir := t.TempDir()
	plain, gzipped := gzipFixture(t, dir)
	readAll := func(path string) string {
		t.Helper()
		f, err := OpenSeries(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		data, err := io.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	p1, p2 := readAll(plain), readAll(gzipped)
	if p1 != p2 {
		t.Fatal("plain and gzipped series differ")
	}
	if strings.Count(p1, "\n") != 121 { // header + 120 rows
		t.Fatalf("expected 121 lines, got %d", strings.Count(p1, "\n"))
	}
}

func TestOpenSeriesMissing(t *testing.T) {
	if _, err := OpenSeries(filepath.Join(t.TempDir(), "nope.csv")); err == nil {
		t.Fatal("expected error for missing file")
	}
}
