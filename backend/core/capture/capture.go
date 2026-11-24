// Package capture は Binance の部分深度ストリーム（<symbol>@depth<levels>）から
// オーダーブックを短期間（10〜60 分）キャプチャし、バックテスト用 CSV
// （timestamp, midPrice, spread, bids(json), asks(json)）へ書き出す。
//
// リアルタイム処理（API の binance_ws.py → gRPC DetectSignal）とは完全に独立した
// プロセス/ループとして動作し、DB・API・gRPC には一切触れない。bids/asks は
// [{"price":..,"volume":..}, ...] のコンパクト JSON で、バックテストの parseRow と
// 同一形式の CSV を生成する。
package capture

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cryptoEngine/core/infrastructure/exchange"
	"golang.org/x/net/websocket"
)

const (
	DefaultSymbol      = "btcusdt"
	DefaultLevels      = 5
	DefaultSampleEvery = time.Second
	DefaultURLTemplate = "wss://stream.binance.com:9443/ws/%s@depth%d"
	reconnectDelay     = 2 * time.Second
	// wsReadTimeout は WS 受信のデッドライン。half-open の無言停滞を
	// 検出して張り替える。板は通常数秒で流れるため余裕のある閾値。
	wsReadTimeout = 120 * time.Second
)

// Options はキャプチャのパラメータ。
type Options struct {
	Symbol      string
	Levels      int
	Duration    time.Duration
	SampleEvery time.Duration
	OutputPath  string
	// Append が true の場合、OutputPath を切り詰めず追記する。
	// ヘッダはファイルが新規・空の場合のみ書き込む。
	// 日次ローテート（capture-daily）用。既定 false（従来通り truncate）。
	Append bool
	// Source は板情報の取得元。"binance"（既定）または "gmo"。
	// gmo の場合は GMO Public WS（orderbooks チャネル）を購読する。
	Source string
}

// useGMO は GMO ソースかどうかを返す（空文字は Binance 扱いで後方互換）。
func useGMO(opts Options) bool {
	return opts.Source == "gmo"
}

// Order は公開用の板レベル（paper 等の外部利用向け）。
type Order struct {
	Price  float64 `json:"price"`
	Volume float64 `json:"volume"`
}

// Snapshot は公開用の 1 サンプリング時点のスナップショット。
type Snapshot struct {
	Timestamp int64   `json:"timestamp"`
	MidPrice  float64 `json:"midPrice"`
	Spread    float64 `json:"spread"`
	Bids      []Order `json:"bids"`
	Asks      []Order `json:"asks"`
}

// snapshotToPublic は内部 snapshot を公開型へ変換する。
func snapshotToPublic(s *snapshot) Snapshot {
	out := Snapshot{
		Timestamp: s.timestamp,
		MidPrice:  s.midPrice,
		Spread:    s.spread,
		Bids:      make([]Order, 0, len(s.bids)),
		Asks:      make([]Order, 0, len(s.asks)),
	}
	for _, o := range s.bids {
		out.Bids = append(out.Bids, Order{Price: o.Price, Volume: o.Volume})
	}
	for _, o := range s.asks {
		out.Asks = append(out.Asks, Order{Price: o.Price, Volume: o.Volume})
	}
	return out
}

// Stream は Binance WS を購読し、SampleEvery 間隔で Snapshot を送信する。
// stop チャネルを閉じると終了し、送信チャネルを閉じる。
// Duration は無視する（paper 等の常駐利用向け）。OutputPath は使わない。
func Stream(opts Options, stop <-chan struct{}) (<-chan Snapshot, error) {
	if opts.Symbol == "" {
		opts.Symbol = DefaultSymbol
	}
	if opts.Levels <= 0 {
		opts.Levels = DefaultLevels
	}
	if opts.SampleEvery <= 0 {
		opts.SampleEvery = DefaultSampleEvery
	}
	if stop == nil {
		return nil, fmt.Errorf("capture: stop channel required")
	}

	state := &streamState{}
	streamDone := make(chan struct{})
	if useGMO(opts) {
		go readLoopGMO(opts, state, streamDone)
	} else {
		go readLoop(opts, state, streamDone)
	}

	out := make(chan Snapshot, 16)
	go func() {
		defer close(out)
		defer close(streamDone)
		ticker := time.NewTicker(opts.SampleEvery)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				state.mu.Lock()
				s := state.latest
				state.mu.Unlock()
				if s == nil {
					continue
				}
				select {
				case out <- snapshotToPublic(s):
				case <-stop:
					return
				}
			}
		}
	}()
	return out, nil
}

// depthMessage は Binance 部分深度ストリームの 1 メッセージ。
type depthMessage struct {
	LastUpdateID int64      `json:"lastUpdateId"`
	Bids         [][]string `json:"bids"`
	Asks         [][]string `json:"asks"`
}

// order は CSV の bids / asks フィールドに書き出す単一レベル。
type order struct {
	Price  float64 `json:"price"`
	Volume float64 `json:"volume"`
}

// snapshot は 1 サンプリング時点の orderbook。
type snapshot struct {
	timestamp int64
	midPrice  float64
	spread    float64
	bids      []order
	asks      []order
}

type streamState struct {
	mu     sync.Mutex
	latest *snapshot
}

// Run は Binance WS を購読し、SampleEvery 間隔で最新スナップショットを
// OutputPath の CSV へ追記する。Duration 経過で終了し、書き込んだ行数を返す。
// 1 行も書き出せなかった場合はエラーを返す。
func Run(opts Options) (int, error) {
	if opts.Symbol == "" {
		opts.Symbol = DefaultSymbol
	}
	if opts.Levels <= 0 {
		opts.Levels = DefaultLevels
	}
	if opts.SampleEvery <= 0 {
		opts.SampleEvery = DefaultSampleEvery
	}
	if opts.Duration <= 0 {
		return 0, fmt.Errorf("capture: duration must be positive")
	}
	if opts.OutputPath == "" {
		return 0, fmt.Errorf("capture: output path required")
	}

	f, writeHeader, err := openOutput(opts.OutputPath, opts.Append)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	if writeHeader {
		if _, err := f.WriteString("timestamp,midPrice,spread,bids,asks\n"); err != nil {
			return 0, fmt.Errorf("capture: write header: %w", err)
		}
	}

	state := &streamState{}
	streamDone := make(chan struct{})
	defer close(streamDone)
	if useGMO(opts) {
		go readLoopGMO(opts, state, streamDone)
	} else {
		go readLoop(opts, state, streamDone)
	}

	deadline := time.Now().Add(opts.Duration)
	ticker := time.NewTicker(opts.SampleEvery)
	defer ticker.Stop()

	rows := 0
	for {
		select {
		case <-ticker.C:
			if time.Now().After(deadline) {
				break
			}
			state.mu.Lock()
			s := state.latest
			state.mu.Unlock()
			if s == nil {
				continue
			}
			if err := writeRow(f, s); err != nil {
				return 0, fmt.Errorf("capture: write row: %w", err)
			}
			rows++
		}
		if time.Now().After(deadline) {
			break
		}
	}

	if rows == 0 {
		return 0, fmt.Errorf("capture: no snapshots captured (source=%s ws unreachable?)", sourceName(opts))
	}
	return rows, nil
}

// sourceName はエラーメッセージ用の取得元名を返す。
func sourceName(opts Options) string {
	if useGMO(opts) {
		return "gmo"
	}
	return "binance"
}

// maxWSMessageBytes は WS メッセージの上限（継続フレーム結合時の fail-safe）。
const maxWSMessageBytes = 4 << 20

// recvJSONMessage は継続フレームを結合し、完全な JSON 1文書を返す。
// GMO の板スナップショットは大きく、サーバが複数フレームに分割して送る。
// x/net/websocket の Receive はフレーム単位で返すため、素朴に読むと
// 途切れた JSON で unmarshal 失敗→切断誤認→無限リトライで0行になる。
// json.Valid で完全性を確かめながら結合する。
func recvJSONMessage(ws *websocket.Conn) ([]byte, error) {
	var buf []byte
	for {
		var frame []byte
		if err := websocket.Message.Receive(ws, &frame); err != nil {
			return nil, err
		}
		buf = append(buf, frame...)
		if len(buf) > maxWSMessageBytes {
			return nil, fmt.Errorf("capture: ws message too large (%d bytes)", len(buf))
		}
		if json.Valid(buf) {
			return buf, nil
		}
	}
}

// openOutput は出力ファイルを開く。appendMode が true の場合は追記し、
// ヘッダ要否（新規・空ファイル時のみ true）を返す。false の場合は従来通り truncate。
func openOutput(path string, appendMode bool) (*os.File, bool, error) {
	if !appendMode {
		f, err := os.Create(path)
		if err != nil {
			return nil, false, fmt.Errorf("capture: create output: %w", err)
		}
		return f, true, nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, false, fmt.Errorf("capture: open output: %w", err)
	}
	st, serr := f.Stat()
	if serr != nil {
		f.Close()
		return nil, false, fmt.Errorf("capture: stat output: %w", serr)
	}
	return f, st.Size() == 0, nil
}

// readLoop は WS を購読し、切断時はリトライしながら最新スナップショットを
// state へ保持し続ける。streamDone が閉じられたら終了する。
func readLoop(opts Options, state *streamState, streamDone chan struct{}) {
	wsURL := fmt.Sprintf(DefaultURLTemplate, opts.Symbol, opts.Levels)
	for {
		select {
		case <-streamDone:
			return
		default:
		}
		ws, err := websocket.Dial(wsURL, "", "http://localhost")
		if err != nil {
			if !sleepReconnect(opts, streamDone, err) {
				return
			}
			continue
		}
		for {
			// 受信デッドライン：half-open の無言停滞を検出して張り替える。
			// 板は通常数秒で流れるため 120s は余裕のある閾値。
			// タイムアウトは recv の error として既存の再接続経路に載る。
			_ = ws.SetDeadline(time.Now().Add(wsReadTimeout))
			var msg depthMessage
			raw, err := recvJSONMessage(ws)
			if err != nil {
				ws.Close()
				if !sleepReconnect(opts, streamDone, err) {
					return
				}
				break
			}
			if err := json.Unmarshal(raw, &msg); err != nil {
				continue
			}
			s := parseMessage(&msg)
			if s == nil {
				continue
			}
			state.mu.Lock()
			state.latest = s
			state.mu.Unlock()
		}
	}
}

// gmoWSMessage は GMO Public WS orderbooks チャネルのメッセージ。
type gmoWSMessage struct {
	Channel string `json:"channel"`
	Symbol  string `json:"symbol"`
	Asks    []struct {
		Price string `json:"price"`
		Size  string `json:"size"`
	} `json:"asks"`
	Bids []struct {
		Price string `json:"price"`
		Size  string `json:"size"`
	} `json:"bids"`
	Timestamp string `json:"timestamp"`
}

// gmoWSSymbol は Public WS orderbooks 用のシンボル形式（XXX_JPY）を返す。
// REST現物の短縮形（BTC）とは異なる。主要銘柄は suffix 必須だが、
// 一部（ASTR/FCR/NAC/WILD/XLM 等）は `_JPY` で ERR-5106 となり短縮形で
// のみ配信されるため、購読側は短縮形フォールバックを持つ（readLoopGMO）。
func gmoWSSymbol(short string) string {
	s := strings.ToUpper(strings.TrimSpace(short))
	if s == "" {
		s = exchange.DefaultGMOSymbol
	}
	if !strings.HasSuffix(s, "_JPY") {
		s += "_JPY"
	}
	return s
}

// gmoWSError は購読エラー応答（例 {"error":"ERR-5003 Request too many."}）。
type gmoWSError struct {
	Error string `json:"error"`
}

// gmoSubPace は購読リクエストの間隔。Public WS は同一IPで1秒1リクエスト
// 制限のため、複数銘柄はこの間隔でずらす（ERR-5003対策）。
const gmoSubPace = 1200 * time.Millisecond

// gmoWSURL は Public WS の接続先。テストで差し替え可能にするため変数化。
var gmoWSURL = exchange.DefaultGMOWSURL

// readLoopGMO は GMO Public WS を購読する。Binance と異なり接続後に
// subscribe メッセージ送信が必要な点以外、readLoop と同じ契約
// （最新スナップショットを state へ保持、切断時リトライ）で動作する。
// エラー応答はログして捨てる（無言リトライにしない）。
//
// シンボル形式フォールバック：`XXX_JPY` を送っても ERR-5106（invalid symbol）
// または 30 秒無配信の銘柄は短縮形（XXX）で購読し直す。これが無いと
// capture-daily / vt-paper が一部銘柄（ASTR/FCR/NAC/WILD/XLM）で
// 0行のまま沈黙する（板CSVがヘッダのみに留まる長期欠測の原因）。
func readLoopGMO(opts Options, state *streamState, streamDone chan struct{}) {
	symbol, err := exchange.ToGMOSymbol(opts.Symbol)
	if err != nil {
		symbol = exchange.DefaultGMOSymbol
	}
	wsSymbol := gmoWSSymbol(symbol)
	shortForm := strings.TrimSuffix(wsSymbol, "_JPY")
	// triedShort が真なら以降の接続は短縮形で購読する（成功形式の記憶）。
	triedShort := false
	loggedFirst := false
	for {
		select {
		case <-streamDone:
			return
		default:
		}
		ws, err := websocket.Dial(gmoWSURL, "", "http://localhost")
		if err != nil {
			log.Printf("capture: gmo ws dial %s: %v", symbol, err)
			if !sleepReconnect(opts, streamDone, err) {
				return
			}
			continue
		}
		cur := wsSymbol
		if triedShort {
			cur = shortForm
		}
		sub, _ := json.Marshal(map[string]string{
			"command": "subscribe", "channel": "orderbooks", "symbol": cur,
		})
		if _, err := ws.Write(sub); err != nil {
			ws.Close()
			log.Printf("capture: gmo ws subscribe %s: %v", symbol, err)
			if !sleepReconnect(opts, streamDone, err) {
				return
			}
			continue
		}
		// 形式フォールバックは接続を張り直して行う（同一接続で即再送すると
		// GMO の 1 秒 1 リクエスト制限に触れ ERR-5003 になる）。sleepReconnect
		// の待機がそのままレート制限の間隔を兼ねる。
		connStart := time.Now()
		for {
			// 受信デッドライン：half-open の無言停滞を検出して張り替える。
			_ = ws.SetDeadline(time.Now().Add(wsReadTimeout))
			raw, err := recvJSONMessage(ws)
			if err != nil {
				ws.Close()
				if !sleepReconnect(opts, streamDone, err) {
					return
				}
				break
			}
			// 無配信30秒の形式フォールバック（ERR が来ない銘柄向け）。
			if !triedShort && !loggedFirst && time.Since(connStart) >= 30*time.Second {
				triedShort = true
				log.Printf("capture: gmo ws retry short form %s", symbol)
				ws.Close()
				if !sleepReconnect(opts, streamDone, fmt.Errorf("no book in 30s")) {
					return
				}
				break
			}
			var em gmoWSError
			if json.Unmarshal(raw, &em) == nil && em.Error != "" {
				log.Printf("capture: gmo ws error %s: %s", symbol, em.Error)
				// ERR-5106（invalid symbol）は短縮形へ切り替えて再接続する。
				if !triedShort && strings.Contains(em.Error, "5106") {
					triedShort = true
					log.Printf("capture: gmo ws retry short form %s", symbol)
					ws.Close()
					if !sleepReconnect(opts, streamDone, fmt.Errorf("invalid symbol")) {
						return
					}
					break
				}
				continue
			}
			var msg gmoWSMessage
			if err := json.Unmarshal(raw, &msg); err != nil {
				continue
			}
			s := parseGMOMessage(&msg)
			if s == nil {
				continue
			}
			if !loggedFirst {
				loggedFirst = true
				log.Printf("capture: gmo ws first book %s mid=%.8g", symbol, s.midPrice)
			}
			state.mu.Lock()
			state.latest = s
			state.mu.Unlock()
		}
	}
}

// readLoopGMOMulti は1接続で複数銘柄の orderbooks を購読する。
// Public WS は同一IPで1秒1リクエスト制限のため、subscribe を
// gmoSubPace 間隔でずらす（17並列接続の ERR-5003 枯渇の恒久対策）。
// 受信は msg.Symbol（XXX_JPY）で仕分けし、銘柄別 state に保持する。
func readLoopGMOMulti(symbols []string, states map[string]*streamState, streamDone chan struct{}) {
	type sub struct {
		our string
		wss string
	}
	var subs []sub
	back := map[string]string{}
	for _, s := range symbols {
		short, err := exchange.ToGMOSymbol(s)
		if err != nil {
			short = exchange.DefaultGMOSymbol
		}
		wss := gmoWSSymbol(short)
		subs = append(subs, sub{our: s, wss: wss})
		back[wss] = s
	}
	// 形式フォールバック：_JPY で ERR-5106 の銘柄は短縮形でも試す。
	// 一部の銘柄は WS が別名体系のため。成功した形式を記憶して使う。
	form := map[string]string{}
	for _, sb := range subs {
		form[sb.our] = sb.wss
	}
	triedShort := map[string]bool{}
	haveBook := map[string]bool{}
	dummy := Options{}
	logged := map[string]bool{}
	for {
		select {
		case <-streamDone:
			return
		default:
		}
		ws, err := websocket.Dial(gmoWSURL, "", "http://localhost")
		if err != nil {
			log.Printf("capture: gmo ws dial multi: %v", err)
			if !sleepReconnect(dummy, streamDone, err) {
				return
			}
			continue
		}
		subOK := true
		for i, sb := range subs {
			wss := form[sb.our]
			msg, _ := json.Marshal(map[string]string{
				"command": "subscribe", "channel": "orderbooks", "symbol": wss,
			})
			if _, err := ws.Write(msg); err != nil {
				ws.Close()
				log.Printf("capture: gmo ws subscribe %s: %v", sb.our, err)
				subOK = false
				break
			}
			if i+1 < len(subs) {
				select {
				case <-streamDone:
					ws.Close()
					return
				case <-time.After(gmoSubPace):
				}
			}
		}
		if !subOK {
			if !sleepReconnect(dummy, streamDone, fmt.Errorf("subscribe failed")) {
				return
			}
			continue
		}
		// 形式フォールバック：接続後30秒で板が来ない銘柄は短縮形で
		// 購読し直す（一部の銘柄は WS が別名体系のため）。
		connStart := time.Now()
		fallbackDone := false
		for {
			_ = ws.SetDeadline(time.Now().Add(wsReadTimeout))
			raw, err := recvJSONMessage(ws)
			if err != nil {
				ws.Close()
				break
			}
			if !fallbackDone && time.Since(connStart) >= 30*time.Second {
				fallbackDone = true
				for _, sb := range subs {
					if haveBook[sb.our] || triedShort[sb.our] {
						continue
					}
					triedShort[sb.our] = true
					short := strings.TrimSuffix(form[sb.our], "_JPY")
					retry, _ := json.Marshal(map[string]string{
						"command": "subscribe", "channel": "orderbooks", "symbol": short,
					})
					if _, err := ws.Write(retry); err != nil {
						break
					}
					form[sb.our] = short
					back[short] = sb.our
					log.Printf("capture: gmo ws retry short form %s", sb.our)
					time.Sleep(gmoSubPace)
				}
			}
			var em gmoWSError
			if json.Unmarshal(raw, &em) == nil && em.Error != "" {
				log.Printf("capture: gmo ws error: %s", em.Error)
				continue
			}
			var msg gmoWSMessage
			if err := json.Unmarshal(raw, &msg); err != nil {
				continue
			}
			s := parseGMOMessage(&msg)
			if s == nil {
				continue
			}
			our, ok := back[msg.Symbol]
			if !ok {
				// echo形式の揺れ（suffix無し等）に備え短縮形でも引く。
				// それでも不明な銘柄は初回のみログして捨てる。
				short := strings.TrimSuffix(strings.ToUpper(strings.TrimSpace(msg.Symbol)), "_JPY")
				for _, sb := range subs {
					if strings.ToUpper(sb.our) == short {
						our, ok = sb.our, true
						back[msg.Symbol] = our
						break
					}
				}
				if !ok {
					if !logged["?"+msg.Symbol] {
						logged["?"+msg.Symbol] = true
						log.Printf("capture: gmo ws unknown symbol echo: %q", msg.Symbol)
					}
					continue
				}
			}
			st := states[our]
			if st == nil {
				continue
			}
			if !logged[our] {
				logged[our] = true
				haveBook[our] = true
				log.Printf("capture: gmo ws first book %s mid=%.8g", our, s.midPrice)
			}
			st.mu.Lock()
			st.latest = s
			st.mu.Unlock()
		}
		if !sleepReconnect(dummy, streamDone, fmt.Errorf("read loop ended")) {
			return
		}
	}
}

// StreamGMOBooks は1接続で複数銘柄の板を購読し、銘柄別の Snapshot
// チャネルを返す。stop を閉じると全チャネルを閉じて終了する。
func StreamGMOBooks(symbols []string, levels int, sampleEvery time.Duration, stop <-chan struct{}) (map[string]<-chan Snapshot, error) {
	if len(symbols) == 0 {
		return nil, fmt.Errorf("capture: symbols required")
	}
	if sampleEvery <= 0 {
		sampleEvery = DefaultSampleEvery
	}
	if stop == nil {
		return nil, fmt.Errorf("capture: stop channel required")
	}
	_ = levels
	states := map[string]*streamState{}
	for _, sym := range symbols {
		states[sym] = &streamState{}
	}
	streamDone := make(chan struct{})
	go readLoopGMOMulti(symbols, states, streamDone)
	chans := map[string]<-chan Snapshot{}
	out := map[string]chan Snapshot{}
	for _, sym := range symbols {
		out[sym] = make(chan Snapshot, 16)
		chans[sym] = out[sym]
	}
	go func() {
		defer close(streamDone)
		ticker := time.NewTicker(sampleEvery)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				for _, ch := range out {
					close(ch)
				}
				return
			case <-ticker.C:
				for _, sym := range symbols {
					st := states[sym]
					st.mu.Lock()
					s := st.latest
					st.mu.Unlock()
					if s == nil {
						continue
					}
					select {
					case out[sym] <- snapshotToPublic(s):
					case <-stop:
						for _, ch := range out {
							close(ch)
						}
						return
					}
				}
			}
		}
	}()
	return chans, nil
}

// parseGMOMessage は GMO 板メッセージを snapshot に変換する。不正データは nil。
// timestamp は ISO8601。解釈失敗時は受信時刻で代替する。
func parseGMOMessage(msg *gmoWSMessage) *snapshot {
	if msg == nil || len(msg.Bids) == 0 || len(msg.Asks) == 0 {
		return nil
	}
	ts := time.Now().UnixMilli()
	if msg.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339, msg.Timestamp); err == nil {
			ts = t.UnixMilli()
		} else if t, err := time.Parse("2006-01-02T15:04:05.000Z", msg.Timestamp); err == nil {
			ts = t.UnixMilli()
		}
	}
	bids := make([]order, 0, len(msg.Bids))
	for _, row := range msg.Bids {
		price, perr := parseFloat(row.Price)
		volume, verr := parseFloat(row.Size)
		if perr != nil || verr != nil || price <= 0 || volume <= 0 {
			continue
		}
		bids = append(bids, order{Price: price, Volume: volume})
	}
	asks := make([]order, 0, len(msg.Asks))
	for _, row := range msg.Asks {
		price, perr := parseFloat(row.Price)
		volume, verr := parseFloat(row.Size)
		if perr != nil || verr != nil || price <= 0 || volume <= 0 {
			continue
		}
		asks = append(asks, order{Price: price, Volume: volume})
	}
	if len(bids) == 0 || len(asks) == 0 {
		return nil
	}
	return &snapshot{
		timestamp: ts,
		midPrice:  (bids[0].Price + asks[0].Price) / 2.0,
		spread:    asks[0].Price - bids[0].Price,
		bids:      bids,
		asks:      asks,
	}
}

// sleepReconnect は接続断時の待機を行う。streamDone で中断されたら false を返す。
func sleepReconnect(opts Options, streamDone chan struct{}, err error) bool {
	select {
	case <-streamDone:
		return false
	case <-time.After(reconnectDelay):
		return true
	}
}

// parseMessage は Binance のメッセージを snapshot に変換する。不正なデータは nil。
func parseMessage(msg *depthMessage) *snapshot {
	if msg == nil || len(msg.Bids) == 0 || len(msg.Asks) == 0 {
		return nil
	}
	bids := make([]order, 0, len(msg.Bids))
	for _, row := range msg.Bids {
		if len(row) < 2 {
			continue
		}
		price, perr := parseFloat(row[0])
		volume, verr := parseFloat(row[1])
		if perr != nil || verr != nil || price <= 0 || volume <= 0 {
			continue
		}
		bids = append(bids, order{Price: price, Volume: volume})
	}
	asks := make([]order, 0, len(msg.Asks))
	for _, row := range msg.Asks {
		if len(row) < 2 {
			continue
		}
		price, perr := parseFloat(row[0])
		volume, verr := parseFloat(row[1])
		if perr != nil || verr != nil || price <= 0 || volume <= 0 {
			continue
		}
		asks = append(asks, order{Price: price, Volume: volume})
	}
	if len(bids) == 0 || len(asks) == 0 {
		return nil
	}
	return &snapshot{
		timestamp: time.Now().UnixMilli(),
		midPrice:  (bids[0].Price + asks[0].Price) / 2.0,
		spread:    asks[0].Price - bids[0].Price,
		bids:      bids,
		asks:      asks,
	}
}

// writeRow は 1 スナップショットを CSV 1 行へ書き出す（JSON はコンパクト形式）。
// 1行を単一 write(2) で書き込む。O_APPEND 共有ファイルへの並行追記でも
// 行が分割されず、他プロセスの行と混線しない（行サイズ < PIPE_BUF 前提）。
func writeRow(f *os.File, s *snapshot) error {
	bids, err := json.Marshal(s.bids)
	if err != nil {
		return err
	}
	asks, err := json.Marshal(s.asks)
	if err != nil {
		return err
	}
	_, err = f.WriteString(fmt.Sprintf("%d,%.2f,%.2f,%s,%s\n",
		s.timestamp, s.midPrice, s.spread, bids, asks))
	return err
}

// parseFloat は string を float64 へ変換する。
func parseFloat(s string) (float64, error) {
	var v float64
	if _, err := fmt.Sscanf(s, "%f", &v); err != nil {
		return 0, err
	}
	return v, nil
}
