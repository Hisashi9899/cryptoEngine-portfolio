package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"

	"os/signal"

	"github.com/cryptoEngine/core/capture"
	"github.com/cryptoEngine/core/domain"
	"github.com/cryptoEngine/core/usecase/trading"
	vt "github.com/cryptoEngine/core/usecase/trading/vt"
)

// vt_live.go は V.Tエンジンの実弾執行ループ（GMO現物・成行・bounded/daemon両対応）。
//
// paper と同一の判定・数量ロジックを使い、複数銘柄の同時シグナルは
// 効率順（vtEfficiency）で1件に絞って約定する。設計原則:
//   - 全状態は coordinator 単一 goroutine が所有する（競合なし）
//   - worker は板スナップショットの転送のみ行う
// 安全装置:
//   - 既定 dry-run（--live が無い限り発注しない。意図記録のみ）
//   - 現物なので空売り入口は出さない（Sell 信号は Buy 建玉の出口専用）
//   - 1銘柄1建玉・全体 --max-positions 件上限（既定1）
//   - 日次損失 --max-daily-loss-jpy 到達で新規停止（SL 2% 既定と併用）
//   - 建玉・日次損益は vt_live_<SYM>.json / vt_live_daily.json に退避する
//   - 実弾約定は trade_log.csv に filled 行で残す（watchdog・週次束ねの監視対象）
// 例: core vt-live --symbols=BTC,XRP --bars=300 --seconds=600
//     core vt-live --bars=300 --seconds=0 --live

// vtLiveState は銘柄別の執行状態（退避ファイルと同形）。
type vtLiveState struct {
	Open      *vt.OpenPos   `json:"open,omitempty"`
	Maker     *makerPending `json:"maker,omitempty"`
	MakerExit *makerExit    `json:"maker_exit,omitempty"`
	Date      string        `json:"date"`
	DailyPnL  float64       `json:"daily_pnl"`
}

// vtLiveCandidate は入口候補。
type vtLiveCandidate struct {
	symbol string
	sig    *vt.Signal
	price  float64
	qty    float64
	spread float64
}

// makerLimitPrice は指値待機の指値を返す（純粋関数・テスト対象）。
// 買いはbidタッチ、売りはaskタッチに置き、板に残る（Maker確定）。
// スプレッド不明時はmidの5bps内側に置く。必ず非市場性（約定の衝突なし）。
func makerLimitPrice(mid, spreadRatio float64, side vt.Side) float64 {
	sp := spreadRatio
	if sp <= 0 {
		sp = 0.001
	}
	if side == vt.Sell {
		return mid * (1 + sp/2)
	}
	return mid * (1 - sp/2)
}

// makerPending は指値待機中の入口注文。退避ファイルと同形にする。
type makerPending struct {
	OrderID string     `json:"order_id"`
	Limit   float64    `json:"limit"`
	Qty     float64    `json:"qty"`
	Sig     *vt.Signal `json:"sig"`
	Placed  int64      `json:"placed"`
}

// makerExit は利確の指値待機（現物ロングの Maker 出口）。入口の待機とは
// 別管理にし、建玉と同時に存在しうる（入口待機は建玉なしが前提）。
type makerExit struct {
	OrderID string  `json:"order_id"`
	Limit   float64 `json:"limit"`
	Qty     float64 `json:"qty"`
	Placed  int64   `json:"placed"`
	Reason  string  `json:"reason"`
}

// makerTPLimit は利確の指値価格を返す（純粋関数・テスト対象）。
// 買い建玉は entry×(1+tp)、売り建玉は entry×(1-tp)。tp<=0 は 0（無効）。
func makerTPLimit(entryPrice, tpRatio float64, side vt.Side) float64 {
	if entryPrice <= 0 || tpRatio <= 0 {
		return 0
	}
	if side == vt.Sell {
		return entryPrice * (1 - tpRatio)
	}
	return entryPrice * (1 + tpRatio)
}

// vtLiveResumeMaxAgeSec は退避建玉の再開上限（秒）。実弾は保守的に24時間。
const vtLiveResumeMaxAgeSec = int64(24 * 3600)

// vtLiveEnabled は実弾フラグを判定する（純粋関数・テスト対象）。
// --live 単独でも有効。--live=false は明示無効。
func vtLiveEnabled(args []string, kv map[string]string) bool {
	if strings.EqualFold(strings.TrimSpace(kv["live"]), "true") {
		return true
	}
	for _, a := range args {
		if strings.TrimSpace(a) == "--live" {
			return true
		}
	}
	return false
}

// vtMarginEnabled は証拠金サブシステムの有効化を判定する（純粋関数）。
// --margin 単独でも有効。ON のとき Sell 入口を証拠金で執行する。
// Buy は常に現物で執行する（either/or ではなく side 別ルーティング）。
func vtMarginEnabled(args []string, kv map[string]string) bool {
	if strings.EqualFold(strings.TrimSpace(kv["margin"]), "true") {
		return true
	}
	for _, a := range args {
		if strings.TrimSpace(a) == "--margin" {
			return true
		}
	}
	return false
}

// vtLiveStatePath は銘柄別の退避パスを返す。
func vtLiveStatePath(dir, symbol string) string {
	return dir + "/vt_live_" + symbol + ".json"
}

// maintJST は時刻を JST に変換する（GMO メンテ判定と同一基準）。
func maintJST(t time.Time) time.Time {
	return t.In(time.FixedZone("JST", 9*60*60))
}

// inMaintWindowT は GMO 定期メンテ時間帯（土曜 09:00-11:00 JST）かを返す。
// 執行層の inMaintenanceWindow と同一定義のループ側ミラー。
// 窓中は発注不可のため、新規も決済も出さない（保持のみ）。
func inMaintWindowT(now time.Time) bool {
	jst := maintJST(now)
	if jst.Weekday() != trading.GMOMaintenanceWeekday {
		return false
	}
	h := jst.Hour()
	return h >= trading.GMOMaintenanceStartH && h < trading.GMOMaintenanceEndH
}

// inMaintPreWindow はメンテ開始前の手仕舞い帯かを返す（preMin<=0 で無効）。
// 帯中は新規入口を止め、保有建玉を理由 maintenance で全決済する。
// メンテ突入時に指値も成行も出せない執行停止へ建玉を持ち越さないため。
func inMaintPreWindow(now time.Time, preMin int) bool {
	if preMin <= 0 {
		return false
	}
	if inMaintWindowT(now) {
		return false
	}
	jst := maintJST(now)
	if jst.Weekday() != trading.GMOMaintenanceWeekday {
		return false
	}
	startMin := trading.GMOMaintenanceStartH * 60
	curMin := jst.Hour()*60 + jst.Minute()
	return curMin >= startMin-preMin && curMin < startMin
}

// maintBlocked は新規入口の停止帯かを返す（窓前＋窓中）。
func maintBlocked(now time.Time, preMin int) bool {
	return inMaintWindowT(now) || inMaintPreWindow(now, preMin)
}

func vtTodayJST() string {
	return time.Now().UTC().Add(9 * time.Hour).Format("20060102")
}

// slipBps は約定の不利乖離をbpsで返す（純粋関数・テスト対象）。
// Buy は fill>ref、Sell は fill<ref が不利（正値）。数量ゼロ・価格ゼロは 0。
// 測定専用で発注判断には使わない。
func slipBps(fill, ref float64, side vt.Side) float64 {
	if fill <= 0 || ref <= 0 {
		return 0
	}
	if side == vt.Sell {
		return (ref - fill) / ref * 10000
	}
	return (fill - ref) / ref * 10000
}

// momBoost は方向一致のMACDモメンタムによる倍率（最大1.3倍）。
// 下降率・上昇率の高さを効率順に上乗せする。不明・逆行時は1。
func momBoost(c vtLiveCandidate) float64 {
	if c.sig == nil || c.price <= 0 {
		return 1
	}
	h := c.sig.MACDHist
	if c.sig.Type == vt.Buy && h > 0 {
		return 1 + math.Min(0.3, 2*h/c.price)
	}
	if c.sig.Type == vt.Sell && h < 0 {
		return 1 + math.Min(0.3, -2*h/c.price)
	}
	return 1
}

// pickTopK は保留候補を効率順に並べ、空き枠の範囲で複数件を選ぶ（純粋関数）。
// 効率＝確信度×バンド位置÷手数料（vtEfficiency）×方向一致モメンタムで、
// 手数料・強さ・位置・下降率（上昇率）を総括する。
// 同一銘柄重複は1件に絞り、建玉済み銘柄は飛ばす。
func pickTopK(pending []vtLiveCandidate, feeBps float64, openSyms map[string]bool, maxPos, opensCount int, dailyLoss, maxDaily float64) ([]vtLiveCandidate, string) {
	return pickTopKCorr(pending, feeBps, openSyms, maxPos, opensCount, dailyLoss, maxDaily, false)
}

// pickTopKCorr は相関クラスタ統制付きの選抜（corrIsolate=true で有効）。
// 同一クラスタの建玉・同時選抜を1件に制限し、見かけの分散を実分散へ寄せる。
// 既存 pickTopK は corrIsolate=false で完全に従来動作を維持する。
func pickTopKCorr(pending []vtLiveCandidate, feeBps float64, openSyms map[string]bool, maxPos, opensCount int, dailyLoss, maxDaily float64, corrIsolate bool) ([]vtLiveCandidate, string) {
	if len(pending) == 0 {
		return nil, "no candidates"
	}
	if maxPos > 0 && opensCount >= maxPos {
		return nil, "max positions"
	}
	if maxDaily > 0 && dailyLoss <= -maxDaily {
		return nil, "daily loss limit"
	}
	ranked := append([]vtLiveCandidate(nil), pending...)
	sort.Slice(ranked, func(i, j int) bool {
		si := vtEfficiency(ranked[i].sig.Confidence, ranked[i].sig.PercentB, feeBps) * momBoost(ranked[i])
		sj := vtEfficiency(ranked[j].sig.Confidence, ranked[j].sig.PercentB, feeBps) * momBoost(ranked[j])
		if si == sj {
			return ranked[i].sig.Confidence > ranked[j].sig.Confidence
		}
		return si > sj
	})
	var out []vtLiveCandidate
	for _, c := range ranked {
		if openSyms[c.symbol] {
			continue
		}
		dup := false
		for _, o := range out {
			if o.symbol == c.symbol {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		// 相関クラスタ統制：同一クラスタに既存建玉・同時選抜があれば見送る。
		if corrIsolate {
			clash := false
			for osym := range openSyms {
				if sameCluster(osym, c.symbol) {
					clash = true
					break
				}
			}
			if !clash {
				for _, o := range out {
					if sameCluster(o.symbol, c.symbol) {
						clash = true
						break
					}
				}
			}
			if clash {
				continue
			}
		}
		out = append(out, c)
		if maxPos > 0 && opensCount+len(out) >= maxPos {
			break
		}
	}
	if len(out) == 0 {
		return nil, "all candidates already open"
	}
	return out, ""
}

// vtTick は worker から coordinator への板スナップショット転送。
type vtTick struct {
	sym                 string
	ts                  int64
	mid, bidVol, askVol float64
	spreadRatio         float64
	// wallImb は上位3段の板厚imb。wallOK が偽のときは不明扱いにする。
	wallImb float64
	wallOK  bool
	// wallBid3/wallAsk3 は上位3段の買い・売り数量合計（壁ライフサイクル用）。
	wallBid3, wallAsk3 float64
	wallSizeOK         bool
}

func (cli *CLI) vtLive(args []string) error {
	kv := parseKeyValues(args)
	seconds := parseFloatKV(kv, "seconds", 0)
	live := vtLiveEnabled(args, kv)
	symbols := []string{}
	if raw := kv["symbols"]; raw != "" {
		for _, s := range strings.Split(raw, ",") {
			if s = strings.ToUpper(strings.TrimSpace(s)); s != "" {
				symbols = append(symbols, s)
			}
		}
	}
	if len(symbols) == 0 {
		symbols = []string{"BTC", "XRP", "XLM", "BCH", "LTC"}
	}
	barsList := parseBarsList(kv["bars"])
	if len(barsList) != 1 {
		return fmt.Errorf("vt-live: --bars must be a single value (60, 300, 900 or 3600)")
	}
	barsSec := barsList[0]
	trendTF := int64(parseFloatKV(kv, "trend-tf", 3600))
	balance := parseFloatKV(kv, "balance-jpy", 100000)
	riskFraction := parseFloatKV(kv, "risk-fraction", 0.2)
	maxPos := int(parseFloatKV(kv, "max-positions", 1))
	maxDaily := parseFloatKV(kv, "max-daily-loss-jpy", 2000)
	pickWindowSec := parseFloatKV(kv, "pick-window-sec", 20)
	feeBps := parseFloatKV(kv, "fee-bps", 5)
	fastTF := int64(parseFloatKV(kv, "fast-tf", 60))
	trailActivate := parseFloatKV(kv, "trail-activate", 0.015)
	trailBack := parseFloatKV(kv, "trail-back", 0.008)
	// 出口の安全網：TP・SL・時間切れ。既定は SL 2%＋12時間タイムストップ。
	// MaxHoldSec=0（無効）のままでは反転見逃し時に無期限放置になるため、
	// 必ず正の既定値を持つ（FCR/BCH/SOL放置の再発防止）。
	tpRatio := parseFloatKV(kv, "tp-ratio", 0)
	slRatio := parseFloatKV(kv, "sl-ratio", 0.02)
	maxHoldSec := parseFloatKV(kv, "max-hold-sec", 43200)
	// 建玉再開の上限（秒）。既定24時間。検証済み実建玉の回収時など、
	// 正当な理由がある場合のみ引き上げること。恒常的な緩和は
	// ゾンビ再開の元になるため既定値に戻す（2026-09-26 回収実績あり）。
	resumeMaxAgeSec := int64(parseFloatKV(kv, "resume-max-age-sec", float64(vtLiveResumeMaxAgeSec)))
	// メンテ前手仕舞い帯（分）。既定30分。0で無効。
	// 帯中は新規を止め保有を全決済し、窓中は保持のみ（発注不可のため）。
	maintPreMin := int(parseFloatKV(kv, "maintenance-exit-min", 30))
	// ATR 倍率の安全網（0＝無効＝固定値動作）。実測後に調整する。
	trailActivateATR := parseFloatKV(kv, "trail-activate-atr", 0)
	trailBackATR := parseFloatKV(kv, "trail-back-atr", 0)
	slATR := parseFloatKV(kv, "sl-atr", 0)
	// 板系離脱の最低保有秒数（0＝無効）。足未確定の即切り往復を防ぐ。
	imbMinHoldSec := parseFloatKV(kv, "imbalance-min-hold-sec", 0)
	// 弱い出口の確証票数（0＝無効＝従来動作）。単一要因の誤射を防ぐ。
	exitConfirmVotes := int(parseFloatKV(kv, "exit-confirm-votes", 0))
	// 入口ゲート（0＝無効＝従来動作）。低確信・売られ過ぎショートの誤射削減。
	// 出口には適用しない。夜間チューナーが --tune-file 経由で動かす。
	minConf := parseFloatKV(kv, "min-confidence", 0)
	shortRSIFloor := parseFloatKV(kv, "short-rsi-floor", 0)
	// Maker執行（既定 false）。trueで現物BUY入口をFAS指値待機にする
	// （約定はMaker手数料・マイナスもあり）。証拠金SELLは対象外
	// （証拠金指値APIがないため従来通り）。dry-runでは指値シミュレーション
	// 記録のみ。タイムアウト既定900秒。
	makerEntries := false
	if raw, ok := kv["maker-entries"]; ok {
		if s := strings.TrimSpace(raw); !(strings.EqualFold(s, "false") || s == "0" || s == "") {
			makerEntries = true
		}
	}
	makerTimeoutMs := int64(parseFloatKV(kv, "maker-timeout-sec", 900) * 1000)
	if makerTimeoutMs <= 0 {
		makerTimeoutMs = 900000
	}
	// ○3 Maker の逆選択抑制：吸収（壁の縮小＋テープ優勢）を確認できた
	// 押し目/戻りにだけ受動約定を置く。未確認なら従来の成行に退化する。
	// 既定は maker-entries 有効時に true（明示的に false/0 で無効化できる）。
	makerRequireAbsorb := makerEntries
	if raw, ok := kv["maker-require-absorb"]; ok {
		s := strings.TrimSpace(raw)
		makerRequireAbsorb = !(strings.EqualFold(s, "false") || s == "0")
	}
	// ○1 コスト・ゲート：ATR比率が往復コスト×k 未満の入口を見送る。
	// k=0 で無効（従来動作）。fee-mode は taker/maker/conservative。
	costGateK := parseFloatKV(kv, "cost-gate-k", 0)
	costGateFeeMode := strings.TrimSpace(kv["cost-gate-fee-mode"])
	if costGateFeeMode == "" {
		costGateFeeMode = "taker"
	}
	gateEntryFee, gateExitFee, gateModeOK := feeModeRates(costGateFeeMode)
	if !gateModeOK {
		gateEntryFee, gateExitFee = 0.0005, 0.0005
		costGateFeeMode = "taker"
	}
	// ○2 回転抑制：決済直後の同一銘柄への即再入を cooldown 秒だけ止める。
	entryCooldownSec := parseFloatKV(kv, "entry-cooldown-sec", 0)
	// ○5 相関クラスタ統制：同一クラスタの同時保有を1件に制限する。
	corrIsolation := false
	if raw, ok := kv["corr-isolation"]; ok {
		s := strings.TrimSpace(raw)
		corrIsolation = !(strings.EqualFold(s, "false") || s == "0" || s == "")
	}
	// ○5 ボラ逆比例の等リスク数量倍率（target=ATR比率。0で無効）。
	volTarget := parseFloatKV(kv, "vol-target", 0)
	// ○4 グリッドのコスト基準（0で既定 0.0015）。Maker前提で下げる場合に設定。
	gridCostRate := parseFloatKV(kv, "grid-cost-rate", 0)
	// ソフト出口の禁止（--exit-deny=rsi_weak,signal 等。空＝全許可）。
	// paper実績で勝率0・損失のみと確認できた出口を切るために使う。
	var exitDeny []string
	for _, p := range strings.Split(kv["exit-deny"], ",") {
		if s := strings.TrimSpace(p); s != "" {
			exitDeny = append(exitDeny, s)
		}
	}
	// 執行シミュレーション（dry-run のみ）：maker 約定を「タッチ」ではなく
	// 指値の crossBps 貫通で認定し、キュー・部分約定の過大評価を抑える。
	// 実運用（live）の約定は取引所に従うため本値の影響を受けない。
	makerCrossBps := parseFloatKV(kv, "maker-cross-bps", 0)
	// Make出口（利確を指値待機にする）：既定 OFF。ON でも安全出口（SL・
	// 時間・反転・板）は即成行へ退避する。現物ロングのみ対象。
	makerExitTP := false
	if raw, ok := kv["maker-exit-tp"]; ok {
		s := strings.TrimSpace(raw)
		makerExitTP = !(strings.EqualFold(s, "false") || s == "0" || s == "")
	}
	makerExitTimeoutMs := int64(parseFloatKV(kv, "maker-exit-timeout-sec", 120) * 1000)
	if makerExitTimeoutMs <= 0 {
		makerExitTimeoutMs = 120000
	}
	// 指値待機の充足ポーリング間隔（秒）。既定30秒。短いほど約定検出が速い。
	makerPollSec := parseFloatKV(kv, "maker-poll-sec", 30)
	if makerPollSec <= 0 {
		makerPollSec = 30
	}
	// tuneファイルがあれば min-confidence より優先する（安全範囲内のみ）。
	// baseCfgへの適用は生成後に行う。
	tunePath := strings.TrimSpace(kv["tune-file"])
	tune := loadTune("")
	if tunePath != "" {
		tune = loadTune(tunePath)
		if tune.MinConfidence > 0 {
			minConf = tune.MinConfidence
		}
		cli.logger.Info("vt-live: tune from %s conf=%.2f votes=%d", tunePath, tune.MinConfidence, tune.ExitConfirmVotes)
	}
	// 証拠金フォールバック上限倍率（基準×倍率）。余力の大半を一発に投げる
	// hogging を防ぐ。既定2.0（基準の2倍＝余力の約40%上限）。
	marginFallbackMult := parseFloatKV(kv, "margin-fallback-mult", 2.0)
	// グリッド（二刀流のレンジ担当）。既定は無効（明示的に有効化すること）。
	// 有効時は regime==range でのみ現物ロングの押し目を拾う。trend 側と
	// 同一ループ・同一枠で動き、パターン名 grid_ で損益分離できる。
	// 小口開始が前提：risk は trend の1/4、枠は1が既定。
	gridEnabled := false
	if raw, ok := kv["grid-enabled"]; ok {
		if s := strings.TrimSpace(raw); !(strings.EqualFold(s, "false") || s == "0" || s == "") {
			gridEnabled = true
		}
	}
	gridRiskFraction := parseFloatKV(kv, "grid-risk-fraction", 0.05)
	gridMaxPos := int(parseFloatKV(kv, "grid-max-positions", 1))
	if gridMaxPos < 0 {
		gridMaxPos = 0
	}
	blockSymRaw := kv["block-symbols"]
	allowSymRaw := kv["allow-symbols"]
	dailyLossRatio := parseFloatKV(kv, "max-daily-loss-ratio", 0)
	blockDays := int(parseFloatKV(kv, "block-days", 7))
	blockMinPairs := int(parseFloatKV(kv, "block-min-pairs", 5))
	balanceRefreshSec := parseFloatKV(kv, "balance-refresh-sec", 300)
	if balanceRefreshSec > 0 && balanceRefreshSec < 60 {
		balanceRefreshSec = 60
	}
	intervalSec := parseFloatKV(kv, "interval", 5.0)
	if maxPos < 1 {
		maxPos = 1
	}
	if pickWindowSec < 1 {
		pickWindowSec = 1
	}

	dir := orderbookLogDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("vt-live: mkdir: %w", err)
	}

	baseCfg := vt.DefaultConfig()
	baseCfg.BarsSec = barsSec
	baseCfg.TrendTFSec = trendTF
	baseCfg.FastTFSec = fastTF
	baseCfg.TrailActivateR = trailActivate
	baseCfg.TrailBackR = trailBack
	baseCfg.TPRatio = tpRatio
	baseCfg.SLRatio = slRatio
	baseCfg.MaxHoldSec = maxHoldSec
	baseCfg.TrailActivateATR = trailActivateATR
	baseCfg.TrailBackATR = trailBackATR
	baseCfg.SLATR = slATR
	baseCfg.ImbMinHoldSec = imbMinHoldSec
	baseCfg.ExitConfirmVotes = exitConfirmVotes
	baseCfg.GridCostRate = gridCostRate
	if raw, ok := kv["short-patterns"]; ok {
		baseCfg.ShortAllow = vt.SplitPatterns(raw)
	}
	// パターン自動調整ファイル（週次生成）。不在・破損時は既定動作に退化する。
	if pf := strings.TrimSpace(kv["pattern-allow-file"]); pf != "" {
		if ld, sd, err := loadPatternDeny(pf); err != nil {
			cli.logger.Info("vt-live: pattern file unreadable (%v); using defaults", err)
		} else {
			baseCfg.LongDeny, baseCfg.ShortDeny = ld, sd
			cli.logger.Info("vt-live: pattern deny long=%d short=%d from %s", len(ld), len(sd), pf)
		}
	}
	// 日次キルスイッチ（P4）。前日負け越しパターンを翌日停止する。
	// 週次ファイルと和集合で適用し、mtime変化で再起動なしに再読込する。
	denyDailyPath := strings.TrimSpace(kv["pattern-deny-file"])
	denyWatch := []string{}
	if pf := strings.TrimSpace(kv["pattern-allow-file"]); pf != "" {
		denyWatch = append(denyWatch, pf)
	}
	if denyDailyPath != "" {
		denyWatch = append(denyWatch, denyDailyPath)
		if ld, sd, err := loadPatternDeny(denyDailyPath); err != nil {
			cli.logger.Info("vt-live: daily deny unreadable (%v); weekly only", err)
		} else {
			baseCfg.LongDeny = unionPatterns(baseCfg.LongDeny, ld)
			baseCfg.ShortDeny = unionPatterns(baseCfg.ShortDeny, sd)
			cli.logger.Info("vt-live: daily deny adds long=%d short=%d from %s", len(ld), len(sd), denyDailyPath)
		}
		if cells, err := loadAllowCells(denyDailyPath); err == nil && len(cells) > 0 {
			baseCfg.AllowCells = unionAllowCells(baseCfg.AllowCells, cells)
		}
	}
	// 実証セル許可制：週次・日次ファイルの和集合。空なら無制限。
	if pf := strings.TrimSpace(kv["pattern-allow-file"]); pf != "" {
		if cells, err := loadAllowCells(pf); err == nil && len(cells) > 0 {
			baseCfg.AllowCells = unionAllowCells(baseCfg.AllowCells, cells)
		}
	}
	if len(baseCfg.AllowCells) > 0 {
		cli.logger.Info("vt-live: allow cells=%d (proven cells only)", len(baseCfg.AllowCells))
	}
	if len(exitDeny) > 0 {
		baseCfg.ExitDeny = exitDeny
		cli.logger.Info("vt-live: exit-deny=%v", exitDeny)
	}
	// レンジ帯の非ブレイク停止（既定 true）。false/0 で無効化できる。
	baseCfg.SqueezeGate = true
	if raw, ok := kv["squeeze-gate"]; ok {
		if s := strings.TrimSpace(raw); strings.EqualFold(s, "false") || s == "0" {
			baseCfg.SqueezeGate = false
		}
	}
	// 夜間チューナーの適用（tuneファイルがあればフラグより優先）。
	if tunePath != "" {
		if tune.ExitConfirmVotes > 0 {
			baseCfg.ExitConfirmVotes = tune.ExitConfirmVotes
		}
		baseCfg.WallFirmMin = tune.WallFirmMin
		baseCfg.ExpCooldownBars = tune.ExpCooldownBars
		baseCfg.ExpProgressMin = tune.ExpProgressMin
		baseCfg.SpoofHaltMs = tune.SpoofHaltMs
		denyWatch = append(denyWatch, tunePath)
	}

	executor := trading.NewGMOOrderExecutor(cli.logger)
	tradeLog := trading.NewTradeLog()

	// 証拠金サブシステム：レバレッジは掛けない。Sell 入口だけを等倍の
	// 証拠金建玉で執行し、Buy は常に現物で執行する（side 別ルーティング）。
	// ルール取得に失敗したら証拠金全体を無効化する（安全側）。
	margin := vtMarginEnabled(args, kv)
	if margin {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := executor.LoadMarginRules(ctx)
		cancel()
		if err != nil {
			cli.logger.Error("vt-live: margin rules load failed (%v); margin disabled", err)
			margin = false
		} else {
			cli.logger.Info("vt-live: margin enabled (1x Sell only, Buy stays spot)")
		}
	}

	// 実残高の取得：発注サイズの基準を実口座に合わせる（想定値との乖離防止）。
	// Buy 用の現物JPYと Sell 用の証拠金余力を別々に保持する。
	// 取得失敗時は --balance-jpy フラグにフォールバックする。
	// fetchBalances は残高照会の単一窓口（起動時・定期更新で共有）。
	fetchBalances := func() (spot, marg float64, okSpot, okMarg bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if assets, err := executor.FetchAssets(ctx); err == nil {
			for _, a := range assets {
				if strings.ToUpper(strings.TrimSpace(a.Symbol)) != "JPY" {
					continue
				}
				var avail float64
				if _, err := fmt.Sscanf(strings.TrimSpace(a.Available), "%f", &avail); err == nil && avail > 0 {
					spot, okSpot = avail, true
				}
			}
		}
		if margin {
			if st, err := executor.FetchMargin(ctx); err == nil {
				var avail float64
				if _, err := fmt.Sscanf(strings.TrimSpace(st.AvailableAmount), "%f", &avail); err == nil && avail > 0 {
					marg, okMarg = avail, true
				}
			}
		}
		return spot, marg, okSpot, okMarg
	}
	buyBalance, sellBalance := balance, balance
	baseMaxDaily := maxDaily
	// 日次上限の基準は現物残高（なければ証拠金、なければフラグ）。
	applyBalances := func(spot, marg float64, okSpot, okMarg bool) {
		if okSpot {
			buyBalance = spot
		}
		if okMarg {
			sellBalance = marg
		}
		base := buyBalance
		if !okSpot && okMarg {
			base = sellBalance
		}
		if okSpot || okMarg {
			maxDaily = scaleDailyLoss(base, dailyLossRatio, baseMaxDaily)
		}
	}
	if live {
		spot, marg, okSpot, okMarg := fetchBalances()
		if okSpot || okMarg {
			cli.logger.Info("vt-live: real balances spot=%v margin=%v (flag was %v)", spot, marg, balance)
			applyBalances(spot, marg, okSpot, okMarg)
		} else {
			cli.logger.Error("vt-live: balance fetch failed; using flag %v", balance)
		}
	}
	mode := "dryrun"
	if live {
		mode = "live"
	}
	cli.logger.Info("vt-live: mode=%s margin=%v symbols=%d bars=%d maxpos=%d maxdaily=%.0f fast=%d trail=%.4f/%.4f tp=%.4f sl=%.4f maxhold=%.0f resumeage=%.0f maintpre=%.0f votes=%d grid=%v( risk=%.3f maxpos=%d)", mode, margin, len(symbols), barsSec, maxPos, maxDaily, fastTF, trailActivate, trailBack, tpRatio, slRatio, maxHoldSec, float64(resumeMaxAgeSec), float64(maintPreMin), exitConfirmVotes, gridEnabled, gridRiskFraction, gridMaxPos)
	cli.logger.Info("vt-live: gates costk=%.2f fee=%s(%+.4f/%+.4f) cooldown=%.0fs corr=%v voltarget=%.4f gridcost=%.4f maker=%v absorb=%v exitmaker=%v",
		costGateK, costGateFeeMode, gateEntryFee, gateExitFee, entryCooldownSec, corrIsolation, volTarget, gridCostRate, makerEntries, makerRequireAbsorb, makerExitTP)
	// 負け銘柄除外：手動指定と paper 負け越し（同足種・直近日数）の和集合。
	// paper は全銘柄を記録し続け、live だけが外す（検証ループ維持）。
	blocked := refreshBlocklist(dir, blockDays, blockMinPairs, parseBlockSymbols(blockSymRaw), parseBlockSymbols(allowSymRaw))
	blockDay := vtTodayJST()
	if len(blocked) > 0 {
		cli.logger.Info("vt-live: blocked symbols=%v (days=%d minPairs=%d)", sortedKeys(blocked), blockDays, blockMinPairs)
	}

	stop := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		close(stop)
	}()
	if seconds > 0 {
		deadline := time.Now().Add(time.Duration(seconds) * time.Second)
		go func() {
			<-time.After(time.Until(deadline))
			select {
			case <-stop:
			default:
				close(stop)
			}
		}()
	}

	// engines・建玉・日次損益の読み込み。
	engines := map[string]*vt.Engine{}
	opens := map[string]*vt.OpenPos{}
	// makerPendings は指値待機中の入口注文（銘柄別・同時1件）。
	// 建玉とは排他（待機中は建玉なし）。再起動時は退避から再開する。
	makerPendings := map[string]*makerPending{}
	makerLastPoll := map[string]time.Time{}
	// makerExits は利確の指値待機（現物ロング・Maker出口）。建玉と同時に存在する。
	makerExits := map[string]*makerExit{}
	makerExitPoll := map[string]time.Time{}
	// lastExit は銘柄別の最終決済時刻（ms）。entry-cooldown の基準。
	lastExit := map[string]int64{}
	daily := map[string]float64{} // date -> pnl（JST日付）
	today := vtTodayJST()
	for _, sym := range symbols {
		c := baseCfg
		engines[sym] = vt.NewEngine(c)
		// paper と同じく板 CSV でウォームアップする。
		// 無いと足が温まるまで数時間かかり、その間の入口が全滅する。
		if os.Getenv("VT_WARMUP") != "0" && !strings.EqualFold(os.Getenv("VT_WARMUP"), "false") {
			if n := vtWarmup(engines[sym], dir, sym, []int64{barsSec}, trendTF); n > 0 {
				cli.logger.Info("vt-live: warmed %d ticks for %s", n, sym)
			}
		}
		if data, err := os.ReadFile(vtLiveStatePath(dir, sym)); err == nil {
			var st vtLiveState
			if err := json.Unmarshal(data, &st); err == nil {
				// 各ファイルは全体の日次損益を保持するため加算せず統合する。
				// 加算すると銘柄数倍に膨らみ、日次損失ゲートが早発きする。
				if st.Date == today {
					mergeDailyPnl(daily, today, st.DailyPnL)
				}
				// 退避建玉の再開：買い玉は常に、売り玉は証拠金有効時のみ。
				// 従来は買い玉だけを再開し、売り玉は再起動で失踪（実弾だけ残る
				// 孤児化）していた。Side 不正・古い退避は再開しない。
				if st.Open != nil && st.Open.EntryPrice > 0 && st.Open.Volume > 0 &&
					(st.Open.Side == vt.Buy || (st.Open.Side == vt.Sell && margin)) &&
					time.Now().UnixMilli()-st.Open.EntryTime <= resumeMaxAgeSec*1000 {
					opens[sym] = st.Open
					cli.logger.Info("vt-live: resumed open %s %s entry=%v", sym, st.Open.Side, st.Open.EntryPrice)
				} else if st.Open != nil && st.Open.Side == vt.Sell && !margin {
					cli.logger.Error("vt-live: orphaned sell open %s ignored (margin disabled); close manually", sym)
				} else if st.Open != nil {
					// 再開しなかった建玉は必ず警告する（期限切れ・不正値）。
					// 実弾が取引所に残っている可能性があるため、手動確認が必要。
					// 沈黙のまま捨てると孤児化に気付けない（再発防止）。
					cli.logger.Error("vt-live: stale open %s %s entry=%v not resumed; verify exchange manually",
						sym, st.Open.Side, st.Open.EntryPrice)
				}
				// 指値待機の再開：期限内のみ引き継ぎ、ポーリングで充足確認する。
				// 古い待機は捨てずに引き継ぎ、tick側のタイムアウトで始末する
				// （取消・約定確認つき）。シグナル欠損時は捨てる。
				if st.Maker != nil && st.Maker.Sig != nil && st.Maker.Limit > 0 && st.Maker.Qty > 0 &&
					time.Now().UnixMilli()-st.Maker.Placed <= resumeMaxAgeSec*1000 {
					makerPendings[sym] = st.Maker
					cli.logger.Info("vt-live: resumed maker pending %s %s limit=%v order=%s",
						sym, st.Maker.Sig.Type, st.Maker.Limit, st.Maker.OrderID)
				} else if st.Maker != nil {
					cli.logger.Error("vt-live: stale maker pending %s not resumed; verify exchange manually", sym)
				}
				// Maker出口（利確指値）の再開：建玉があり期限内なら引き継ぐ。
				// 建玉なし・期限切れは再開せず、取引所の残注文は手動確認を促す。
				if st.MakerExit != nil && st.MakerExit.Limit > 0 && st.MakerExit.Qty > 0 {
					if _, hasOpen := opens[sym]; hasOpen &&
						time.Now().UnixMilli()-st.MakerExit.Placed <= resumeMaxAgeSec*1000 {
						me := *st.MakerExit
						makerExits[sym] = &me
						cli.logger.Info("vt-live: resumed maker exit %s limit=%v order=%s",
							sym, me.Limit, me.OrderID)
					} else {
						cli.logger.Error("vt-live: stale maker exit %s not resumed; verify exchange manually", sym)
					}
				}
			}
		}
	}
	saveAll := func() {
		today := vtTodayJST()
		for _, sym := range symbols {
			st := vtLiveState{Date: today, DailyPnL: daily[today]}
			if o, ok := opens[sym]; ok {
				st.Open = o
			}
			if mp, ok := makerPendings[sym]; ok {
				st.Maker = mp
			}
			if me, ok := makerExits[sym]; ok {
				st.MakerExit = me
			}
			data, err := json.Marshal(st)
			if err != nil {
				continue
			}
			_ = os.WriteFile(vtLiveStatePath(dir, sym), append(data, '\n'), 0o644)
		}
	}
	defer saveAll()

	dryAppend := func(rec map[string]any) {
		line, err := json.Marshal(rec)
		if err != nil {
			return
		}
		f, err := os.OpenFile(dir+"/vt_live_dryrun_"+time.Now().Format("20060102")+".jsonl",
			os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		_, _ = f.Write(append(line, '\n'))
		_ = f.Close()
	}
	nowISO := func() string { return time.Now().UTC().Format(time.RFC3339) }
	// スリッページ警告閾値（bps）。約定がシグナル価格から不利方向に
	// これ以上乖離したら警告ログに残す（測定専用・動作は変えない）。
	slipAlertBps := parseFloatKV(kv, "slip-alert-bps", 50)

	// placeOrder は現物発注する。dry-run では注文せず dryrun 結果を返す。
	// 証拠金の建玉・決済は専用メソッド（MarginMarketOrder/CloseMarginPosition）を
	// 直接使うこと。close 側の side で振り分けると逆になるため禁止。
	placeOrder := func(sym string, side domain.TradeSide, qty, refPrice float64, reason string) (*trading.OrderResult, error) {
		_ = reason
		if !live {
			return &trading.OrderResult{Symbol: sym, Side: side, Price: refPrice,
				Quantity: qty, OrderID: "dryrun", Status: "dryrun"}, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		return executor.MarketOrder(ctx, sym, side, qty)
	}

	// ゾンビ建玉の連続失敗カウンタと終端マーク（closeOpen と呼び出し側で共有）。
	// ロング・ショート対称。orphanCleared が立った close は損益加算せず削除する
	// （取引所にポジションが無く実損益不明のため、phantom 計上を防ぐ）。
	orphanFails := map[string]int{}
	orphanCleared := map[string]bool{}

	// closeOpen は建玉を決済する（dry-run では意図記録のみ）。
	// 戻り値は決済完了かどうか。実弾の発注失敗時は false を返し、
	// 建玉を維持して次 bar で再試行する。
	// ただし取引所にポジション不在が連続したゾンビ建玉は orphan として終端し
	// true を返す（呼び出し側は orphanCleared を見て損益加算せず削除する）。
	// ロング・ショート対称の共有経路（side 別の分岐なし）。
	closeOpen := func(sym string, open *vt.OpenPos, price float64, reason string, ts int64) bool {
		// 数量ゼロのゴースト建玉は即時クリアする（部分決済の丸め残差対策）。
		// 呼び出し側が削除・保存するため true（完了扱い）を返す。
		if open.Volume <= 0 {
			cli.logger.Info("vt-live: open %s already flat (vol=%.8f); clearing", sym, open.Volume)
			return true
		}
		pnl := (price - open.EntryPrice) * open.Volume
		if open.Side == vt.Sell {
			pnl = (open.EntryPrice - price) * open.Volume
		}
		if !live {
			dryAppend(map[string]any{"ts": ts, "symbol": sym, "action": "close",
				"side": string(open.Side), "price": price, "qty": open.Volume,
				"pnl": pnl, "reason": reason})
			cli.logger.Info("vt-live DRYRUN close %s %s price=%v pnl=%v reason=%s", sym, open.Side, price, pnl, reason)
			return true
		}
		side := domain.TradeSell
		if open.Side == vt.Sell {
			side = domain.TradeBuy
		}
		// 売り玉は証拠金でのみ決済する。証拠金無効時に現物BUYを出すと
		// 決済ではなく新規ロング建てになる逆注文事故のため、発注せず
		// 保持する（再開ロジックが margin 無効の売り玉をそもそも
		// 引き継がないため到達不能のはずだが、安全側に倒す）。
		if open.Side == vt.Sell && !margin {
			cli.logger.Error("vt-live: refuse to close sell open %s without margin (would open spot long); keeping open", sym)
			_ = tradeLog.Append(trading.TradeLogEntry{Timestamp: nowISO(), Action: "BUY",
				Symbol: sym, Price: price, Amount: open.Volume, Reason: reason,
				OrderID: "", Status: "error"})
			return false
		}
		var res *trading.OrderResult
		var err error
		if margin && open.Side == vt.Sell {
			// 証拠金ショートの決済は positionId 指定の closeOrder を使う。
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			res, err = executor.CloseMarginPosition(ctx, sym, domain.TradeSide(open.Side), open.Volume)
			cancel()
		} else {
			res, err = placeOrder(sym, side, open.Volume, price, reason)
		}
		if err != nil {
			cli.logger.Error("vt-live: close order failed %s (%v); keeping open for retry", sym, err)
			action := "SELL"
			if side == domain.TradeBuy {
				action = "BUY"
			}
			_ = tradeLog.Append(trading.TradeLogEntry{Timestamp: nowISO(), Action: action,
				Symbol: sym, Price: price, Amount: open.Volume, Reason: reason,
				OrderID: "", Status: "error"})
			// ゾンビ終端：取引所にポジション不在が連続したら orphan として
			// ローカル建玉を手放す（枠塞ぎ・error連打の停止）。損益は加算しない。
			// 12回未満は従来通り保持＋再試行する（回帰防止）。
			if isOrphanNoPosition(err.Error()) {
				orphanFails[sym]++
				if orphanShouldClear(orphanFails[sym]) {
					cli.logger.Error("vt-live: orphaned open %s %s cleared after %d no-position errors; verify exchange manually",
						sym, open.Side, orphanFails[sym])
					_ = tradeLog.Append(trading.TradeLogEntry{Timestamp: nowISO(), Action: action,
						Symbol: sym, Price: price, Amount: open.Volume, Reason: reason + "/orphaned",
						OrderID: "", Status: "orphaned"})
					delete(orphanFails, sym)
					orphanCleared[sym] = true
					return true
				}
			} else {
				delete(orphanFails, sym)
			}
			// 部分決済の可能性：決済分だけ建玉を減量し、残りは追跡継続する。
			// 全量失敗時は従来通り建玉維持で再試行する。減量分は退避保存し、
			// 日次損益にも計上する（最終決済時は残量分だけ計上され二重計上しない）。
			// 建玉管理料の按分は省略する（最終決済時のみ控除。微差は許容）。
			if res != nil && res.Quantity > 0 {
				closedQty := res.Quantity
				if closedQty > open.Volume {
					closedQty = open.Volume
				}
				fillPx := res.Price
				if fillPx <= 0 {
					fillPx = price
				}
				partPnl := (fillPx - open.EntryPrice) * closedQty
				if open.Side == vt.Sell {
					partPnl = (open.EntryPrice - fillPx) * closedQty
				}
				open.Volume -= closedQty
				today := vtTodayJST()
				daily[today] += partPnl
				_ = tradeLog.Append(trading.TradeLogEntry{Timestamp: nowISO(), Action: action,
					Symbol: sym, Price: fillPx, Amount: closedQty, Pnl: &partPnl,
					Reason: reason + "/partial", OrderID: res.OrderID, Status: "filled"})
				saveAll()
				cli.logger.Info("vt-live: partial close %s %s qty=%v remaining=%v",
					sym, open.Side, closedQty, open.Volume)
			}
			return false
		}
		fill := res.Price
		if fill <= 0 {
			fill = price
		}
		pnl = (fill - open.EntryPrice) * open.Volume
		if open.Side == vt.Sell {
			pnl = (open.EntryPrice - fill) * open.Volume
		}
		if margin && open.Side == vt.Sell {
			// 建玉管理料（6:00 JST 跨ぎ分）を控除する。
			fee := trading.MarginFee(fill*open.Volume, open.EntryTime, ts)
			if fee > 0 {
				cli.logger.Info("vt-live: margin fee %s %.2f JPY", sym, fee)
				pnl -= fee
			}
		}
		// 決済アクションは建玉と逆向き（買い玉→SELL、売り玉→BUY）。
		// error 枝と同一の対応表。未定義変数のまま残すとビルド不可になる
		// ため、ここで必ず定義する（2026-09-26 コンパイル破損の再発防止）。
		fillAction := "SELL"
		if side == domain.TradeBuy {
			fillAction = "BUY"
		}
		_ = tradeLog.Append(trading.TradeLogEntry{Timestamp: nowISO(), Action: fillAction,
			Symbol: sym, Price: fill, Amount: open.Volume, Pnl: &pnl,
			Reason: reason, OrderID: res.OrderID, Status: "filled"})
		cli.logger.Info("vt-live: closed %s %s fill=%v pnl=%v reason=%s order=%s",
			sym, open.Side, fill, pnl, reason, res.OrderID)
		// スリッページ測定（警告のみ・動作不変）。決済の参照価格は mid。
		if slip := slipBps(fill, price, open.Side); slip >= slipAlertBps {
			cli.logger.Warn("vt-live: slippage on close %s %s ref=%v fill=%v (%.1fbps)",
				sym, open.Side, price, fill, slip)
		}
		delete(orphanFails, sym)
		return true
	}

	// settleMaker は指値待機を始末する（取消＋約定確認）。
	// 約定があれば建玉を開いて (qty, price, true) を返し、
	// なければ (0, 0, false) を返す。dry-run では何もしない
	// （tick側のシミュレーションが担う）。ロング・ショート対称。
	settleMaker := func(sym, why string) (float64, float64, bool) {
		mp, ok := makerPendings[sym]
		if !ok {
			return 0, 0, false
		}
		delete(makerPendings, sym)
		delete(makerLastPoll, sym)
		if !live {
			return 0, 0, false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := executor.CancelOrder(ctx, mp.OrderID); err != nil {
			cli.logger.Info("vt-live: maker cancel %s %s (%v); checking fills", sym, mp.OrderID, err)
		}
		qty, avg := executor.QueryFills(ctx, sym, mp.OrderID)
		if qty <= 0 {
			cli.logger.Info("vt-live: maker miss %s %s (%s)", sym, mp.Sig.Pattern, why)
			saveAll()
			return 0, 0, false
		}
		if avg <= 0 {
			avg = mp.Limit
		}
		fillAction := "BUY"
		if mp.Sig.Type == vt.Sell {
			fillAction = "SELL"
		}
		_ = tradeLog.Append(trading.TradeLogEntry{Timestamp: nowISO(), Action: fillAction,
			Symbol: sym, Price: avg, Amount: qty,
			Reason: mp.Sig.Reason + "/maker", OrderID: mp.OrderID, Status: "filled"})
		opens[sym] = &vt.OpenPos{Side: mp.Sig.Type, EntryPrice: avg,
			EntryTime: mp.Placed, Volume: qty,
			EntryRegime: mp.Sig.Regime, EntryPattern: mp.Sig.Pattern,
			EntryRSI: mp.Sig.RSI, EntryPB: mp.Sig.PercentB,
			EntryHist: mp.Sig.MACDHist, EntryConf: mp.Sig.Confidence}
		saveAll()
		cli.logger.Info("vt-live: maker filled %s %s qty=%v price=%v (%s)",
			sym, mp.Sig.Type, qty, avg, why)
		return qty, avg, true
	}

	// openNew は新規建玉を開く（dry-run では意図記録のみ）。
	openNew := func(cand vtLiveCandidate) {
		sym := cand.symbol
		// Maker の逆選択抑制：吸収を確認できた押し目/戻りだけを指値待機にし、
		// 未確認なら成行へ退化する（シグナル自体は捨てない）。
		useMaker := makerEntries && cand.sig.Type == vt.Buy
		if useMaker && makerRequireAbsorb {
			if eng := engines[sym]; eng == nil || !eng.AbsorbConfirmed(sym, 1) {
				useMaker = false
				cli.logger.Info("vt-live: maker condition unmet %s (%s); using taker", sym, cand.sig.Pattern)
			}
		}
		if !live {
			// Maker dry-run：指値待機をシミュレーションする（tick側で約定判定）。
			// 建玉は開かず、待機だけ記録する。
			if useMaker {
				lim := makerLimitPrice(cand.price, cand.spread, cand.sig.Type)
				dryAppend(map[string]any{"ts": cand.sig.Timestamp, "symbol": sym, "action": "open-maker",
					"side": string(cand.sig.Type), "limit": lim, "qty": cand.qty,
					"pattern": cand.sig.Pattern, "regime": cand.sig.Regime,
					"confidence": cand.sig.Confidence, "timeout_ms": makerTimeoutMs})
				cp := *cand.sig
				makerPendings[sym] = &makerPending{OrderID: "dryrun", Limit: lim, Qty: cand.qty, Sig: &cp, Placed: cand.sig.Timestamp}
				cli.logger.Info("vt-live DRYRUN maker-pending %s %s limit=%v qty=%v pattern=%s",
					sym, cand.sig.Type, lim, cand.qty, cand.sig.Pattern)
				saveAll()
				return
			}
			dryAppend(map[string]any{"ts": cand.sig.Timestamp, "symbol": sym, "action": "open",
				"side": string(cand.sig.Type), "price": cand.price, "qty": cand.qty,
				"pattern": cand.sig.Pattern, "regime": cand.sig.Regime,
				"confidence": cand.sig.Confidence, "mm_state": cand.sig.MMState,
				"score": vtEfficiency(cand.sig.Confidence, cand.sig.PercentB, feeBps)})
			cli.logger.Info("vt-live DRYRUN open %s %s price=%v qty=%v pattern=%s",
				sym, cand.sig.Type, cand.price, cand.qty, cand.sig.Pattern)
			opens[sym] = &vt.OpenPos{Side: cand.sig.Type, EntryPrice: cand.price,
				EntryTime: cand.sig.Timestamp, Volume: cand.qty,
				EntryRegime: cand.sig.Regime, EntryPattern: cand.sig.Pattern,
				EntryRSI: cand.sig.RSI, EntryPB: cand.sig.PercentB,
				EntryHist: cand.sig.MACDHist, EntryConf: cand.sig.Confidence}
			return
		}
		var side domain.TradeSide = domain.TradeBuy
		if cand.sig.Type == vt.Sell {
			if !margin {
				cli.logger.Error("vt-live: spot cannot short %s; skipping", sym)
				return
			}
			side = domain.TradeSell
		}
		var res *trading.OrderResult
		var err error
		if side == domain.TradeSell {
			// 証拠金ショートの建玉は MarginMarketOrder で開く。
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			res, err = executor.MarginMarketOrder(ctx, sym, side, cand.qty)
			cancel()
		} else if useMaker {
			// Maker執行：FAS指値を置いて待つ。約定も取消もtick側で始末する。
			// 既存待機があれば先に始末する（約定済みならその建玉を優先する）。
			if _, ok := makerPendings[sym]; ok {
				if _, _, filled := settleMaker(sym, "replace"); filled {
					return
				}
			}
			lim := makerLimitPrice(cand.price, cand.spread, cand.sig.Type)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			res, err = executor.LimitOrder(ctx, sym, side, cand.qty, lim, "FAS")
			cancel()
			if err != nil {
				cli.logger.Error("vt-live: maker limit failed %s (%v)", sym, err)
				_ = tradeLog.Append(trading.TradeLogEntry{Timestamp: nowISO(), Action: "BUY",
					Symbol: sym, Price: lim, Amount: cand.qty,
					Reason: cand.sig.Reason + "/maker", OrderID: "", Status: "error"})
				return
			}
			// 即時充足の確認：約定があれば建玉化し、残りは取り消す。
			// なければ全量待機する。
			ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
			fq, favg := executor.QueryFills(ctx2, sym, res.OrderID)
			cancel2()
			cp := *cand.sig
			if fq > 0 {
				if favg <= 0 {
					favg = lim
				}
				_ = executor.CancelOrder(context.Background(), res.OrderID)
				_ = tradeLog.Append(trading.TradeLogEntry{Timestamp: nowISO(), Action: "BUY",
					Symbol: sym, Price: favg, Amount: fq,
					Reason: cand.sig.Reason + "/maker", OrderID: res.OrderID, Status: "filled"})
				opens[sym] = &vt.OpenPos{Side: cand.sig.Type, EntryPrice: favg,
					EntryTime: cand.sig.Timestamp, Volume: fq,
					EntryRegime: cand.sig.Regime, EntryPattern: cand.sig.Pattern,
					EntryRSI: cand.sig.RSI, EntryPB: cand.sig.PercentB,
					EntryHist: cand.sig.MACDHist, EntryConf: cand.sig.Confidence}
				saveAll()
				cli.logger.Info("vt-live: maker instant fill %s %s qty=%v price=%v order=%s",
					sym, cand.sig.Type, fq, favg, res.OrderID)
				return
			}
			makerPendings[sym] = &makerPending{OrderID: res.OrderID, Limit: lim, Qty: cand.qty, Sig: &cp, Placed: time.Now().UnixMilli()}
			saveAll()
			cli.logger.Info("vt-live: maker resting %s %s limit=%v qty=%v order=%s",
				sym, cand.sig.Type, lim, cand.qty, res.OrderID)
			return
		} else {
			res, err = placeOrder(sym, side, cand.qty, cand.price, cand.sig.Reason)
		}
		if err != nil {
			cli.logger.Error("vt-live: open order failed %s (%v)", sym, err)
			action := "BUY"
			if side == domain.TradeSell {
				action = "SELL"
			}
			_ = tradeLog.Append(trading.TradeLogEntry{Timestamp: nowISO(), Action: action,
				Symbol: sym, Price: cand.price, Amount: cand.qty,
				Reason: cand.sig.Reason, OrderID: "", Status: "error"})
			return
		}
		fill := res.Price
		if fill <= 0 {
			fill = cand.price
		}
		// 建玉数量は実発注量（刻み丸め後）で保持する。要求量のままだと
		// 刻み未満の端数・浮動小数点ダストが残高と乖離し、決済時の
		// 資金ガードに引っかかって売れなくなる（2026-09-26 SUI 実績）。
		vol := res.Quantity
		if vol <= 0 {
			vol = cand.qty
		}
		fillAction := "BUY"
		if side == domain.TradeSell {
			fillAction = "SELL"
		}
		_ = tradeLog.Append(trading.TradeLogEntry{Timestamp: nowISO(), Action: fillAction,
			Symbol: sym, Price: fill, Amount: vol,
			Reason: cand.sig.Reason, OrderID: res.OrderID, Status: "filled"})
		opens[sym] = &vt.OpenPos{Side: cand.sig.Type, EntryPrice: fill,
			EntryTime: cand.sig.Timestamp, Volume: vol,
			EntryRegime: cand.sig.Regime, EntryPattern: cand.sig.Pattern,
			EntryRSI: cand.sig.RSI, EntryPB: cand.sig.PercentB,
			EntryHist: cand.sig.MACDHist, EntryConf: cand.sig.Confidence}
		cli.logger.Info("vt-live: opened %s %s fill=%v qty=%v order=%s",
			sym, cand.sig.Type, fill, cand.qty, res.OrderID)
		// スリッページ測定（警告のみ・動作不変）。入口の参照価格はシグナル価格。
		if slip := slipBps(fill, cand.price, cand.sig.Type); slip >= slipAlertBps {
			cli.logger.Warn("vt-live: slippage on open %s %s ref=%v fill=%v (%.1fbps)",
				sym, cand.sig.Type, cand.price, fill, slip)
		}
	}

	ticks := make(chan vtTick, 1024)
	// worker は転送のみ。判定・状態は coordinator が一元管理する。
	// ハートビートは60秒毎に更新する（watchdog の生存監視用）。
	// lastBalanceCheck は残高の最終確認時刻（ms）。heartbeat で可視化する。
	var lastBalanceCheck int64
	// lastTick は銘柄別の最終 tick 受信時刻。停滞検出に使う。
	lastTick := map[string]time.Time{}
	lastBeat := time.Now()
	// deny再読込の監視状態（P4）。mtime変化で全エンジンにApplyDenyする。
	denyMod := map[string]time.Time{}
	for _, p := range denyWatch {
		if st, err := os.Stat(p); err == nil {
			denyMod[p] = st.ModTime()
		}
	}
	lastDenyCheck := time.Now()
	reloadDeny := func() {
		if time.Since(lastDenyCheck) < 60*time.Second {
			return
		}
		lastDenyCheck = time.Now()
		changed := false
		for _, p := range denyWatch {
			st, err := os.Stat(p)
			if err != nil {
				continue
			}
			if mt, ok := denyMod[p]; !ok || st.ModTime().After(mt) {
				denyMod[p] = st.ModTime()
				changed = true
			}
		}
		if !changed {
			return
		}
		var ld, sd []string
		for _, p := range denyWatch {
			if a, b, err := loadPatternDeny(p); err == nil {
				ld = unionPatterns(ld, a)
				sd = unionPatterns(sd, b)
			}
		}
		for _, eng := range engines {
			if eng != nil {
				eng.ApplyDeny(ld, sd)
			}
		}
		cli.logger.Info("vt-live: deny reloaded long=%d short=%d", len(ld), len(sd))
		// 実証セル再読込：週次・日次の和集合で置き換える。
		var cells []vt.AllowCell
		for _, p := range denyWatch {
			if c, err := loadAllowCells(p); err == nil && len(c) > 0 {
				cells = unionAllowCells(cells, c)
			}
		}
		for _, eng := range engines {
			if eng != nil {
				eng.ApplyAllow(cells)
			}
		}
		if len(cells) > 0 {
			cli.logger.Info("vt-live: allow cells reloaded=%d", len(cells))
		}
		// tune再読込：確信度ゲートと安全ノブを更新する。
		if tunePath != "" {
			nt := loadTune(tunePath)
			if nt.MinConfidence > 0 && nt.MinConfidence != minConf {
				cli.logger.Info("vt-live: tune conf %.2f -> %.2f", minConf, nt.MinConfidence)
				minConf = nt.MinConfidence
			}
			for _, eng := range engines {
				if eng != nil {
					eng.ApplyTune(tuneToParams(nt))
				}
			}
		}
	}
	beat := func() {
		if time.Since(lastBeat) < 60*time.Second {
			return
		}
		lastBeat = time.Now()
		reloadDeny()
		opensView := map[string]any{}
		for sym, o := range opens {
			opensView[sym] = map[string]any{"side": string(o.Side), "entry": o.EntryPrice}
		}
		tickAge := map[string]int64{}
		for _, sym := range symbols {
			if t, ok := lastTick[sym]; ok {
				tickAge[sym] = int64(time.Since(t).Seconds())
			} else {
				tickAge[sym] = -1
			}
		}
		data, err := json.Marshal(map[string]any{"ts": time.Now().UnixMilli(),
			"mode": mode, "opens": opensView, "daily_pnl": daily[vtTodayJST()],
			"balance_jpy": buyBalance, "margin_jpy": sellBalance, "balance_checked_ts": lastBalanceCheck,
			"tick_age": tickAge})
		if err != nil {
			return
		}
		_ = os.WriteFile(dir+"/vt_live_heartbeat.json", append(data, '\n'), 0o644)
	}
	forwardSnap := func(sym string, ch <-chan capture.Snapshot) {
		for snap := range ch {
			var bidVol, askVol float64
			for _, o := range snap.Bids {
				bidVol += o.Volume
			}
			for _, o := range snap.Asks {
				askVol += o.Volume
			}
			// 上位3段の板厚（合計量では希薄化する上位偏在用）。取れなければ不明扱い。
			wall, wallOK := snapWallImb(snap)
			// 壁ライフサイクル追跡用に上位3段数量も送る（P1データ層）。
			wallBid3, wallAsk3, wallSizeOK := snapWallSizes(snap)
			select {
			case ticks <- vtTick{sym: sym, ts: snap.Timestamp, mid: snap.MidPrice, bidVol: bidVol, askVol: askVol, spreadRatio: snapSpreadRatio(snap), wallImb: wall, wallOK: wallOK, wallBid3: wallBid3, wallAsk3: wallAsk3, wallSizeOK: wallSizeOK}:
			case <-stop:
				return
			}
		}
	}
	// GMO時は1接続で全銘柄を購読する（同一IPで1秒1リクエスト制限のため
	// 17並列接続ではERR-5003で枯渇する）。他ソースは従来の銘柄別接続。
	if captureSource(kv) == "gmo" {
		gchans, err := capture.StreamGMOBooks(symbols, 5, time.Duration(intervalSec*float64(time.Second)), stop)
		if err != nil {
			return fmt.Errorf("vt-live: gmo books: %w", err)
		}
		for sym, ch := range gchans {
			sym, ch := sym, ch
			go func() { forwardSnap(sym, ch) }()
		}
		cli.logger.Info("vt-live: gmo shared book stream (%d symbols)", len(gchans))
	} else {
		for _, sym := range symbols {
			sym := sym
			go func() {
				streamOpts := capture.Options{
					Symbol: sym, Levels: 5,
					SampleEvery: time.Duration(intervalSec * float64(time.Second)),
					Source:      captureSource(kv),
				}
				ch, err := capture.Stream(streamOpts, stop)
				if err != nil {
					cli.logger.Error("vt-live: stream %s: %v", sym, err)
					return
				}
				forwardSnap(sym, ch)
			}()
		}
	}

	// 約定テープ供給：全銘柄の公開約定を取得し、各エンジンの
	// CVD・吸収検知に使う（paper と同一ロジックの live 版）。
	// --tape-poll-sec（既定10秒。GMO公開API負荷に配慮し5秒未満は切り上げ）。
	// 発注判断をブロックしない（失敗時は黙って次回に回す）。
	// 肯定投票（flowAgree のテープ側）が本番で死なないための配線。
	if tapeFetcher := vtTapeFetcher(captureSource(kv)); tapeFetcher != nil {
		tapePollSec := parseFloatKV(kv, "tape-poll-sec", 10)
		if tapePollSec < 5 {
			tapePollSec = 5
		}
		cli.logger.Info("vt-live: tape feed enabled (%.0fs poll, %d symbols)", tapePollSec, len(symbols))
		go func() {
			seen := map[string]bool{}
			var order []string
			poll := func() {
				for _, sym := range symbols {
					execs, err := tapeFetcher.FetchExecutions(sym, 100)
					if err != nil {
						continue
					}
					eng := engines[sym]
					if eng == nil {
						continue
					}
					for _, e := range execs {
						if e.Timestamp <= 0 || e.Price <= 0 || e.Qty <= 0 {
							continue
						}
						key := fmt.Sprintf("%s|%d|%.8f|%.8f|%s", sym, e.Timestamp, e.Price, e.Qty, e.Side)
						if seen[key] {
							continue
						}
						seen[key] = true
						order = append(order, key)
						if len(order) > 4096 {
							delete(seen, order[0])
							order = order[1:]
						}
						eng.AddTape(sym, e.Timestamp, e.Price, e.Qty, e.Side == domain.TradeBuy)
					}
				}
			}
			poll()
			t := time.NewTicker(time.Duration(tapePollSec * float64(time.Second)))
			defer t.Stop()
			for {
				select {
				case <-stop:
					return
				case <-t.C:
					poll()
				}
			}
		}()
	}

	var pending []vtLiveCandidate
	var gridPending []vtLiveCandidate
	var pickTimer <-chan time.Time
	armTimer := func() {
		if pickTimer == nil {
			pickTimer = time.After(time.Duration(pickWindowSec) * time.Second)
		}
	}
	// applyClose は決済成功後の後処理（日次損益・cooldown・建玉削除）を担う。
	// evaluateExit と manageMakerExit で共用する（二重計上防止）。
	applyClose := func(sym string, open *vt.OpenPos, price float64, ts int64) {
		if orphanCleared[sym] {
			delete(orphanCleared, sym)
			lastExit[sym] = ts
			delete(opens, sym)
			saveAll()
			cli.logger.Info("vt-live: orphan cleared %s %s (no pnl)", sym, open.Side)
			return
		}
		pnl := (price - open.EntryPrice) * open.Volume
		if open.Side == vt.Sell {
			pnl = (open.EntryPrice - price) * open.Volume
		}
		today := vtTodayJST()
		daily[today] += pnl
		lastExit[sym] = ts
		delete(opens, sym)
		saveAll()
	}

	// otherExitReason は利確以外の出口理由を返す（Maker出口待機中の強制決済判定）。
	otherExitReason := func(sym string, open *vt.OpenPos, mid float64, ts int64, sig *vt.Signal) string {
		eng := engines[sym]
		if eng == nil {
			return ""
		}
		if vt.IsGridPattern(open.EntryPattern) {
			if ok, r := eng.GridExit(sym, *open, mid, ts); ok && r != "take_profit" && r != "grid_tp" {
				return r
			}
		}
		if ok, r := eng.EvalExit(sym, *open, mid, ts); ok && r != "take_profit" {
			return r
		}
		if sig != nil && ((open.Side == vt.Buy && sig.Type == vt.Sell) ||
			(open.Side == vt.Sell && sig.Type == vt.Buy)) {
			return "opposite_" + sig.Pattern
		}
		return ""
	}

	// startMakerExit は利確の指値（post-only=FAS）を置く。失敗時は何もせず、
	// 次tickの通常（成行）決済に委ねる。dry-run は待機のみ記録する。
	startMakerExit := func(sym string, open *vt.OpenPos, ts int64) {
		lim := makerTPLimit(open.EntryPrice, baseCfg.TPRatio, open.Side)
		if lim <= 0 || open.Volume <= 0 {
			return
		}
		if !live {
			dryAppend(map[string]any{"ts": ts, "symbol": sym, "action": "exit-maker",
				"limit": lim, "qty": open.Volume, "reason": "take_profit"})
			makerExits[sym] = &makerExit{OrderID: "dryrun", Limit: lim, Qty: open.Volume, Placed: ts, Reason: "take_profit"}
			saveAll()
			cli.logger.Info("vt-live DRYRUN maker-exit resting %s limit=%v qty=%v", sym, lim, open.Volume)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		res, err := executor.LimitOrder(ctx, sym, domain.TradeSell, open.Volume, lim, "FAS")
		if err != nil {
			cli.logger.Info("vt-live: maker-exit limit failed %s (%v); will use taker", sym, err)
			return
		}
		// 即時充足（異常時）：そのまま約定扱いにする。
		q, avg := executor.QueryFills(context.Background(), sym, res.OrderID)
		if q >= open.Volume && q > 0 {
			_ = executor.CancelOrder(context.Background(), res.OrderID)
			px := avg
			if px <= 0 {
				px = lim
			}
			if closeOpen(sym, open, px, "take_profit/maker", ts) {
				applyClose(sym, open, px, ts)
			}
			return
		}
		makerExits[sym] = &makerExit{OrderID: res.OrderID, Limit: lim, Qty: open.Volume,
			Placed: time.Now().UnixMilli(), Reason: "take_profit"}
		saveAll()
		cli.logger.Info("vt-live: maker-exit resting %s limit=%v qty=%v order=%s", sym, lim, open.Volume, res.OrderID)
	}

	// manageMakerExit は利確指値の約定検出と、強制・期限での成行退避を担う。
	// 戻り値は決済完了（または建玉消滅）かどうか。
	manageMakerExit := func(sym string, open *vt.OpenPos, me *makerExit, mid float64, ts int64, sig *vt.Signal) bool {
		filled, fillPx := false, me.Limit
		if !live {
			filled = makerCrossed("Sell", mid, me.Limit, makerCrossBps)
		} else if time.Since(makerExitPoll[sym]) > time.Duration(makerPollSec*float64(time.Second)) {
			makerExitPoll[sym] = time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			q, avg := executor.QueryFills(ctx, sym, me.OrderID)
			cancel()
			if q >= open.Volume && q > 0 {
				filled = true
				if avg > 0 {
					fillPx = avg
				}
			}
		}
		if filled {
			delete(makerExits, sym)
			delete(makerExitPoll, sym)
			if closeOpen(sym, open, fillPx, me.Reason+"/maker", ts) {
				applyClose(sym, open, fillPx, ts)
			}
			return true
		}
		forced := otherExitReason(sym, open, mid, ts, sig)
		if forced == "" && ts <= me.Placed+makerExitTimeoutMs {
			return false
		}
		// 強制・期限：まず指値を取り消してから成行決済（二重売り防止）。
		delete(makerExits, sym)
		delete(makerExitPoll, sym)
		if live {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_ = executor.CancelOrder(ctx, me.OrderID)
			q, _ := executor.QueryFills(ctx, sym, me.OrderID)
			cancel()
			if q >= open.Volume && open.Volume > 0 {
				if closeOpen(sym, open, me.Limit, me.Reason+"/maker", ts) {
					applyClose(sym, open, me.Limit, ts)
				}
				return true
			}
			if q > 0 {
				open.Volume -= q
			}
		}
		if open.Volume <= 1e-12 {
			open.Volume = 0
			delete(opens, sym)
			lastExit[sym] = ts
			saveAll()
			return true
		}
		r := forced
		if r == "" {
			r = me.Reason + "/timeout"
		}
		if closeOpen(sym, open, mid, r, ts) {
			applyClose(sym, open, mid, ts)
		}
		return true
	}

	// evaluateExit は建玉の出口を判定・執行する。sig=nil のときは
	// 反対シグナルを見ない（sweep 用）。戻り値は決済完了かどうか。
	evaluateExit := func(sym string, open *vt.OpenPos, mid float64, tsMs int64, sig *vt.Signal) bool {
		eng := engines[sym]
		if eng == nil {
			return false
		}
		exitReason := ""
		// グリッド建玉は固有出口（利確・無効化）を先に判定する。
		// 決まらなければ下の trend 安全網（SL・時間・トレイル・反転・板）に委ねる。
		if vt.IsGridPattern(open.EntryPattern) {
			if ok, reason := eng.GridExit(sym, *open, mid, tsMs); ok {
				exitReason = reason
			}
		}
		if exitReason == "" {
			if ok, reason := eng.EvalExit(sym, *open, mid, tsMs); ok {
				exitReason = reason
			} else if sig != nil && ((open.Side == vt.Buy && sig.Type == vt.Sell) ||
				(open.Side == vt.Sell && sig.Type == vt.Buy)) {
				exitReason = "opposite_" + sig.Pattern
			}
		}
		if exitReason == "" {
			return false
		}
		// Maker出口（利確を指値待機）：既定 OFF。現物ロングかつ非グリッドのみ。
		// 待機中は manageMakerExit が約定/強制を捌く。TP 以外の出口は即成行。
		if exitReason == "take_profit" && makerExitTP && open.Side == vt.Buy &&
			!vt.IsGridPattern(open.EntryPattern) {
			if _, waiting := makerExits[sym]; !waiting {
				startMakerExit(sym, open, tsMs)
			}
			return false
		}
		if closeOpen(sym, open, mid, exitReason, tsMs) {
			applyClose(sym, open, mid, tsMs)
			return true
		}
		return false
	}
	// exitSweep は板ストリーム停滞時の安全網：建玉銘柄の出口を
	// REST 現在値で定期再評価する。stream 健康時は tick 側が先に捌くため
	// 何もしない（単一 goroutine のため二重決済は起きない）。
	exitSweepSec := parseFloatKV(kv, "exit-sweep-sec", 60)
	tickStaleSec := parseFloatKV(kv, "tick-stale-sec", 120)
	var exitTimer <-chan time.Time
	if exitSweepSec > 0 {
		ticker := time.NewTicker(time.Duration(exitSweepSec) * time.Second)
		defer ticker.Stop()
		exitTimer = ticker.C
	}
	// 残高の定期再取得：入金・出金・損益で変わる実残高に追随する。
	// 失敗時は現行値を維持する（ゼロ化しない）。建玉中の数量は変えない。
	var balanceTimer <-chan time.Time
	if live && balanceRefreshSec > 0 {
		ticker := time.NewTicker(time.Duration(balanceRefreshSec) * time.Second)
		defer ticker.Stop()
		balanceTimer = ticker.C
	}

	for {
		select {
		case <-stop:
			saveAll()
			return printJSON(map[string]any{"status": "ok", "engine": "vt-live",
				"mode": mode, "opens": len(opens), "daily_pnl": daily[vtTodayJST()]})
		case <-balanceTimer:
			lastBalanceCheck = time.Now().UnixMilli()
			// 証拠金の遅延回復：起動時にルール取得失敗（メンテ等）で無効化
			// されていても、定期更新で再取得し、成功したら有効化する。
			// 再起動なしで窓明け後にフルスコープへ自動復帰するため。
			// 既存建玉は全て Buy（Sell は margin 有効時しか建たない）なので
			// 途中での有効化は安全。
			if !margin {
				func() {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					if err := executor.LoadMarginRules(ctx); err != nil {
						cli.logger.Info("vt-live: margin rules retry failed (%v); staying disabled", err)
						return
					}
					margin = true
					cli.logger.Info("vt-live: margin recovered (rules loaded on retry)")
				}()
			}
			spot, marg, okSpot, okMarg := fetchBalances()
			if okSpot || okMarg {
				if (okSpot && balanceChanged(buyBalance, spot)) || (okMarg && balanceChanged(sellBalance, marg)) {
					cli.logger.Info("vt-live: balances updated spot=%.0f margin=%.0f", spot, marg)
					applyBalances(spot, marg, okSpot, okMarg)
				}
			} else {
				cli.logger.Info("vt-live: balance refresh failed; keeping spot=%.0f margin=%.0f", buyBalance, sellBalance)
			}
		case <-exitTimer:
			// 窓中は発注不可のため sweep も止める（失敗スパム防止）。
			if inMaintWindowT(time.Now()) {
				continue
			}
			nowMs := time.Now().UnixMilli()
			for sym, open := range opens {
				if !tickStale(lastTick[sym], time.Now(), tickStaleSec) {
					continue
				}
				mid, err := executor.SpotMid(sym)
				if err != nil {
					cli.logger.Info("vt-live: sweep price failed %s (%v)", sym, err)
					continue
				}
				if me, waiting := makerExits[sym]; waiting {
					if manageMakerExit(sym, open, me, mid, nowMs, nil) {
						cli.logger.Info("vt-live: sweep maker-exit %s", sym)
					}
					continue
				}
				if evaluateExit(sym, open, mid, nowMs, nil) {
					cli.logger.Info("vt-live: sweep exit %s", sym)
				}
			}
		case tk := <-ticks:
			beat()
			lastTick[tk.sym] = time.Now()
			eng := engines[tk.sym]
			if eng == nil {
				continue
			}
			if tk.wallOK {
				eng.AddWall(tk.sym, tk.wallImb)
			}
			if tk.wallSizeOK {
				eng.TrackWall(tk.sym, tk.ts, tk.wallBid3, tk.wallAsk3, tk.mid)
			}
			sig, bar := eng.Observe(tk.sym, tk.ts, tk.mid, tk.bidVol, tk.askVol)
			// メンテ帯：窓前は当該銘柄を即時手仕舞い（他銘柄は自tickで順次）、
			// 新規入口は窓前・窓中共に停止する。窓中は保持のみ。
			// 足の蓄積（Observe）は止めない（窓明け後の入口を殺さない）。
			if maintBlocked(time.Now(), maintPreMin) {
				if open, ok := opens[tk.sym]; ok && inMaintPreWindow(time.Now(), maintPreMin) {
					// 利確指値が残っていれば先に取り消す（成行との二重売り防止）。
					if me, waiting := makerExits[tk.sym]; waiting {
						if live {
							ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
							_ = executor.CancelOrder(ctx, me.OrderID)
							_, _ = executor.QueryFills(ctx, tk.sym, me.OrderID)
							cancel()
						}
						delete(makerExits, tk.sym)
						delete(makerExitPoll, tk.sym)
					}
					if closeOpen(tk.sym, open, tk.mid, "maintenance", tk.ts) {
						if orphanCleared[tk.sym] {
							delete(orphanCleared, tk.sym)
							lastExit[tk.sym] = tk.ts
							delete(opens, tk.sym)
							saveAll()
							cli.logger.Info("vt-live: orphan cleared %s %s on maintenance (no pnl)", tk.sym, open.Side)
						} else {
							pnl := (tk.mid - open.EntryPrice) * open.Volume
							if open.Side == vt.Sell {
								pnl = (open.EntryPrice - tk.mid) * open.Volume
							}
							today := vtTodayJST()
							daily[today] += pnl
							lastExit[tk.sym] = tk.ts
							delete(opens, tk.sym)
							saveAll()
							cli.logger.Info("vt-live: maintenance exit %s %s", tk.sym, open.Side)
						}
					}
				}
				pending = nil
				gridPending = nil
				continue
			}
			// 指値待機の管理：充足ポーリング・タイムアウト・反対合図で始末する。
			// 建玉とは排他（待機中は建玉なし）のため、始末後は通常フローに続ける。
			if mp, ok := makerPendings[tk.sym]; ok {
				if !live {
					// dry-run シミュレーション：タッチ（または crossBps 貫通）で約定、
					// 期限切れでmiss。crossBps>0 で逆選択を織り込む。
					touched := makerCrossed(string(mp.Sig.Type), tk.mid, mp.Limit, makerCrossBps)
					if touched {
						dryAppend(map[string]any{"ts": tk.ts, "symbol": tk.sym, "action": "open",
							"side": string(mp.Sig.Type), "price": mp.Limit, "qty": mp.Qty,
							"pattern": mp.Sig.Pattern, "regime": mp.Sig.Regime,
							"confidence": mp.Sig.Confidence, "maker": true})
						opens[tk.sym] = &vt.OpenPos{Side: mp.Sig.Type, EntryPrice: mp.Limit,
							EntryTime: tk.ts, Volume: mp.Qty,
							EntryRegime: mp.Sig.Regime, EntryPattern: mp.Sig.Pattern,
							EntryRSI: mp.Sig.RSI, EntryPB: mp.Sig.PercentB,
							EntryHist: mp.Sig.MACDHist, EntryConf: mp.Sig.Confidence}
						delete(makerPendings, tk.sym)
						saveAll()
						cli.logger.Info("vt-live DRYRUN maker filled %s %s price=%v",
							tk.sym, mp.Sig.Type, mp.Limit)
					} else if tk.ts > mp.Placed+makerTimeoutMs {
						delete(makerPendings, tk.sym)
						saveAll()
						cli.logger.Info("vt-live DRYRUN maker miss %s %s", tk.sym, mp.Sig.Pattern)
					}
				} else if sig != nil && ((mp.Sig.Type == vt.Buy && sig.Type == vt.Sell) ||
					(mp.Sig.Type == vt.Sell && sig.Type == vt.Buy)) {
					settleMaker(tk.sym, "opposite")
				} else if tk.ts > mp.Placed+makerTimeoutMs {
					settleMaker(tk.sym, "timeout")
				} else if time.Since(makerLastPoll[tk.sym]) > time.Duration(makerPollSec*float64(time.Second)) {
					makerLastPoll[tk.sym] = time.Now()
					ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
					actives, err := executor.FetchActiveOrders(ctx, tk.sym)
					cancel()
					if err == nil {
						found := false
						for _, a := range actives {
							if a.OrderID == mp.OrderID {
								found = true
								break
							}
						}
						if !found {
							settleMaker(tk.sym, "poll-filled")
						}
					}
				}
			}
			// 建玉あり → 出口は毎tick判定する（SL・時間切れの遅延防止）。
			// 従来は確定足が閉じたtickでしか判定せず、足周期（5分）分だけ
			// SL/トレイル/時間切れが遅延した。新規足が閉じたときだけ sig を
			// 渡し、反対シグナル離脱も見る。足未確定のtickでは sig=nil で
			// 価格系（SL/TP/時間/トレイル・指標）のみ判定する。
			if open, ok := opens[tk.sym]; ok {
				if me, waiting := makerExits[tk.sym]; waiting {
					manageMakerExit(tk.sym, open, me, tk.mid, tk.ts, sig)
					continue
				}
				evaluateExit(tk.sym, open, tk.mid, tk.ts, sig)
				continue
			}
			// 建玉なし → 入口は確定足でのみ判定する。
			if bar == nil || sig == nil {
				continue
			}
			// 建玉なし → 入口候補。Buy は現物、Sell は証拠金（--margin 時のみ）。
			// 除外銘柄は拾わない。証拠金シンボル無し銘柄の Sell も拾わない。
			// ゲート（確信度・飛び板）は trend 候補にだけ適用する。
			// Hold には適用しない（適用するとグリッド到達不能の欠陥になる）。
			if sig.Type == vt.Buy || (margin && sig.Type == vt.Sell) {
				// 入口ゲート：低確信・売られ過ぎショートを見送る（出口は対象外）。
				if ok, why := entryAllowed(sig, minConf, shortRSIFloor); !ok {
					cli.logger.Info("vt-live: skip gated %s %s (%s)", tk.sym, sig.Pattern, why)
					continue
				}
				// 飛び板注意：スプレッド異常拡大ではロング・ショート対称に見送る。
				// 板が飛んでいる状態での新規は滑り・誤射の元になるため。
				if tobiitaBlocked(tk.spreadRatio) {
					cli.logger.Info("vt-live: skip tobiita %s %s (spread=%.5f)", tk.sym, sig.Pattern, tk.spreadRatio)
					continue
				}
				// コスト・ゲート：狙える値動き(ATR)が往復コスト×k未満なら見送る。
				if ok, why := costGateAllowed(eng.ATRRatio(tk.sym), tk.spreadRatio, gateEntryFee, gateExitFee, costGateK); !ok {
					cli.logger.Info("vt-live: skip cost-gated %s %s (%s atr=%.5f)", tk.sym, sig.Pattern, why, eng.ATRRatio(tk.sym))
					continue
				}
				// 回転抑制：決済直後の同一銘柄への即再入を止める。
				if cooldownBlocked(tk.ts, lastExit[tk.sym], entryCooldownSec) {
					cli.logger.Info("vt-live: skip cooldown %s %s", tk.sym, sig.Pattern)
					continue
				}
				if blocked[tk.sym] {
					cli.logger.Info("vt-live: skip blocked %s %s", tk.sym, sig.Pattern)
					continue
				}
				if sig.Type == vt.Sell && !executor.SupportsMargin(tk.sym) {
					cli.logger.Info("vt-live: skip non-margin %s %s", tk.sym, sig.Pattern)
					continue
				}
				var qty float64
				if sig.Type == vt.Sell {
					var ok bool
					qty, ok = executor.MarginSizeFor(tk.sym, sig.Price, sig.Confidence, sellBalance, riskFraction, tk.spreadRatio, marginFallbackMult)
					if !ok {
						continue
					}
				} else {
					qty = vtPaperQty(tk.sym, sig.Price, sig.Confidence, buyBalance, riskFraction, tk.spreadRatio)
					if qty <= 0 {
						continue
					}
				}
				// ボラ逆比例の等リスク数量（target未設定なら倍率1）。
				if volTarget > 0 && qty > 0 {
					qty *= volScale(eng.ATRRatio(tk.sym), volTarget)
				}
				pending = append(pending, vtLiveCandidate{symbol: tk.sym, sig: sig, price: sig.Price, qty: qty, spread: tk.spreadRatio})
				armTimer()
				continue
			}
			// trend が Hold のときだけグリッド（二刀流のレンジ担当）を評価する。
			// regime==range でのみ発火するため trend との二重発火はない。
			// 現物ロング専用・小口（grid-risk-fraction）。
			if gridEnabled {
				gsig := eng.GridEntry(tk.sym, tk.ts, tk.spreadRatio)
				if gsig.Type != vt.Buy {
					continue
				}
				if ok, why := entryAllowed(gsig, minConf, shortRSIFloor); !ok {
					cli.logger.Info("vt-live: skip grid-gated %s %s (%s)", tk.sym, gsig.Pattern, why)
					continue
				}
				if tobiitaBlocked(tk.spreadRatio) {
					cli.logger.Info("vt-live: skip grid-tobiita %s (spread=%.5f)", tk.sym, tk.spreadRatio)
					continue
				}
				if ok, why := costGateAllowed(eng.ATRRatio(tk.sym), tk.spreadRatio, gateEntryFee, gateExitFee, costGateK); !ok {
					cli.logger.Info("vt-live: skip grid-cost-gated %s %s (%s atr=%.5f)", tk.sym, gsig.Pattern, why, eng.ATRRatio(tk.sym))
					continue
				}
				if cooldownBlocked(tk.ts, lastExit[tk.sym], entryCooldownSec) {
					cli.logger.Info("vt-live: skip grid-cooldown %s %s", tk.sym, gsig.Pattern)
					continue
				}
				if blocked[tk.sym] {
					cli.logger.Info("vt-live: skip grid-blocked %s %s", tk.sym, gsig.Pattern)
					continue
				}
				gqty := vtPaperQty(tk.sym, gsig.Price, gsig.Confidence, buyBalance, gridRiskFraction, tk.spreadRatio)
				if gqty <= 0 {
					continue
				}
				if volTarget > 0 {
					gqty *= volScale(eng.ATRRatio(tk.sym), volTarget)
				}
				gridPending = append(gridPending, vtLiveCandidate{symbol: tk.sym, sig: gsig, price: gsig.Price, qty: gqty, spread: tk.spreadRatio})
				armTimer()
			}
		case <-pickTimer:
			pickTimer = nil
			// メンテ帯に拾った候補は捨てる（窓前・窓中の新規建玉を禁止）。
			if maintBlocked(time.Now(), maintPreMin) {
				pending = nil
				gridPending = nil
				continue
			}
			today := vtTodayJST()
			// 日跨ぎで除外リストを更新する（前日の paper 実績を反映）。
			if today != blockDay {
				blockDay = today
				blocked = refreshBlocklist(dir, blockDays, blockMinPairs, parseBlockSymbols(blockSymRaw), parseBlockSymbols(allowSymRaw))
				cli.logger.Info("vt-live: blocklist refreshed=%v", sortedKeys(blocked))
			}
			openSyms := map[string]bool{}
			for s := range opens {
				openSyms[s] = true
			}
			if len(pending) != 0 {
				picks, reason := pickTopKCorr(pending, feeBps, openSyms, maxPos, len(opens), daily[today], maxDaily, corrIsolation)
				if len(picks) == 0 {
					cli.logger.Info("vt-live: pick rejected %d candidates (%s)", len(pending), reason)
				}
				for _, p := range picks {
					cli.logger.Info("vt-live: pick %s %s (score selection over %d candidates)",
						p.symbol, p.sig.Pattern, len(pending))
					openNew(p)
					openSyms[p.symbol] = true
				}
				pending = nil
			}
			// グリッド選抜：trend と同一の効率・日次上限で選び、共有枠と
			// グリッド枠の両方を守る。PFはパターン名 grid_ で分離できる。
			if gridEnabled && len(gridPending) != 0 {
				gridOpens := 0
				for _, o := range opens {
					if vt.IsGridPattern(o.EntryPattern) {
						gridOpens++
					}
				}
				if gridMaxPos > 0 && gridOpens >= gridMaxPos {
					cli.logger.Info("vt-live: grid pick rejected %d candidates (grid max positions)", len(gridPending))
				} else {
					gpicks, greason := pickTopKCorr(gridPending, feeBps, openSyms, maxPos, len(opens), daily[today], maxDaily, corrIsolation)
					if gridMaxPos > 0 && len(gpicks) > gridMaxPos-gridOpens {
						gpicks = gpicks[:gridMaxPos-gridOpens]
					}
					if len(gpicks) == 0 {
						cli.logger.Info("vt-live: grid pick rejected %d candidates (%s)", len(gridPending), greason)
					}
					for _, p := range gpicks {
						cli.logger.Info("vt-live: grid pick %s %s (score selection over %d candidates)",
							p.symbol, p.sig.Pattern, len(gridPending))
						openNew(p)
					}
				}
				gridPending = nil
			}
			saveAll()
		}
	}
}
