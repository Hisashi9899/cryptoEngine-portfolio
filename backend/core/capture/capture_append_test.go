package capture

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOpenOutputAppend は追記モードのヘッダ判定（新規・空のみ true）を確認する。
// truncate モードの既存挙動は変えない。
func TestOpenOutputAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "orderbook_20240101.csv")

	// 1) 新規ファイル：ヘッダ要。
	f, hdr, err := openOutput(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if !hdr {
		t.Fatal("new file must require header")
	}
	if _, err := f.WriteString("timestamp,midPrice,spread,bids,asks\nrow1\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	// 2) 既存・非空ファイル：ヘッダ不要、追記されること。
	f, hdr, err = openOutput(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if hdr {
		t.Fatal("non-empty file must not require header")
	}
	if _, err := f.WriteString("row2\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "timestamp,midPrice,spread,bids,asks\nrow1\nrow2\n"; got != want {
		t.Fatalf("append mismatch:\n%q", got)
	}

	// 3) truncate モード：切り詰め＋ヘッダ要（従来動作）。
	f, hdr, err = openOutput(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if !hdr {
		t.Fatal("truncate mode must require header")
	}
	f.Close()
	if st, _ := os.Stat(path); st.Size() != 0 {
		t.Fatal("truncate mode must empty the file")
	}
}

// TestConcurrentAppendIntegrity は複数ライタの並行追記でも行が混線せず、
// 全行が5カラムの正規形式を保つことを確認する（bufio バッチ書き時代の
// 行連結破損の回帰テスト。各行は単一 write(2) で書き込む前提）。
func TestConcurrentAppendIntegrity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "orderbook_20240101.csv")

	const writers = 8
	const rowsEach = 200
	// ヘッダは先行して1行だけ書き込む（実運用では最初のプロセスが行う）。
	hf, hdr, err := openOutput(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if !hdr {
		t.Fatal("fresh file must require header")
	}
	if _, err := hf.WriteString("timestamp,midPrice,spread,bids,asks\n"); err != nil {
		t.Fatal(err)
	}
	hf.Close()

	done := make(chan error, writers)
	for w := 0; w < writers; w++ {
		go func(w int) {
			f, _, err := openOutput(path, true)
			if err != nil {
				done <- err
				return
			}
			defer f.Close()
			for i := 0; i < rowsEach; i++ {
				mid := 65000.0 + float64(w*1000+i)*0.5
				s := &snapshot{
					timestamp: int64(1700000000000 + w*1000000 + i*1000),
					midPrice:  mid, spread: 5,
					bids: []order{{Price: mid - 2.5, Volume: 8}},
					asks: []order{{Price: mid + 2.5, Volume: 2}},
				}
				if err := writeRow(f, s); err != nil {
					done <- err
					return
				}
			}
			done <- nil
		}(w)
	}
	for w := 0; w < writers; w++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	// ヘッダ1行＋全ライタ分。ヘッダ重複があれば行数が合わなくなる。
	if len(lines) != 1+writers*rowsEach {
		t.Fatalf("line count=%d, want %d (interleaved or duplicated header?)",
			len(lines), 1+writers*rowsEach)
	}
	if lines[0] != "timestamp,midPrice,spread,bids,asks" {
		t.Fatalf("header corrupted: %q", lines[0])
	}
	for i, ln := range lines[1:] {
		if !validRow(ln) {
			t.Fatalf("line %d unparsable (interleaved?): %.80q", i+1, ln)
		}
	}
}

// validRow は CSV 行が timestamp,midPrice,spread,bids(JSON),asks(JSON) の
// 5 フィールド構成で、bids/asks が JSON 配列として解釈できるかを検証する。
func validRow(ln string) bool {
	var fields []string
	depth := 0
	start := 0
	for i := 0; i < len(ln); i++ {
		switch ln[i] {
		case '[', '{':
			depth++
		case ']', '}':
			depth--
		case ',':
			if depth == 0 {
				fields = append(fields, ln[start:i])
				start = i + 1
			}
		}
	}
	fields = append(fields, ln[start:])
	if len(fields) != 5 || depth != 0 {
		return false
	}
	var bids, asks []map[string]float64
	if err := json.Unmarshal([]byte(fields[3]), &bids); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(fields[4]), &asks); err != nil {
		return false
	}
	return len(bids) > 0 && len(asks) > 0
}
