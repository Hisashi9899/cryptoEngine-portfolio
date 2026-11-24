package backtest

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// csvio.go は板 CSV の透過読みを提供する。
//
// 背景：90日保持は gzip 化が前提（全銘柄×90日は非圧縮で数十GB）。
// .csv と .csv.gz を区別せず読めるよう、系列ローダーはすべて
// OpenSeries・GlobSeries 経由にする。圧縮は prune が担い、
// 同一データの .csv/.csv.gz 二重保持は作らない約束。

// OpenSeries は板 CSV を開く。.gz  suffix は透過展開する。
// 呼び出し側は Close すること。存在しない・壊れた .gz は error。
func OpenSeries(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(strings.ToLower(path), ".gz") {
		return f, nil
	}
	zr, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return &gzReadCloser{Reader: zr, closer: f}, nil
}

type gzReadCloser struct {
	io.Reader
	closer io.Closer
}

func (g *gzReadCloser) Close() error {
	_ = g.closer.Close()
	if c, ok := g.Reader.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// GlobSeries は prefix に続く .csv／.csv.gz を時刻順に返す。
// 例: GlobSeries(dir, "orderbook_BTC_") → 日次ファイル群。
// 同一日の二重保持は運用で作らない（prune が gzip 置換する）。
func GlobSeries(dir, prefix string) []string {
	var out []string
	for _, ext := range []string{".csv", ".csv.gz"} {
		files, _ := filepath.Glob(filepath.Join(dir, prefix+"*"+ext))
		out = append(out, files...)
	}
	sort.Strings(out)
	return out
}
