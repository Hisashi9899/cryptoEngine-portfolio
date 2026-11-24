package vt

import "strings"

// engine.go は V.Tエンジン（テクニカル分析型）を提供する。
//
// v2（5秒板スキャル）とは別物で、確定足の形を読む。
//-polices:
//   - 上位方向（MA位置＋傾き）に逆らわない
//   - 上位足の確定を待つ。初動の1時間未確定ブレイクは追わない
//     （ヘッドフェイク回避。株の達人 順張り doctrine：収縮→拡大→帯歩き）。
//     レンジ下抜けエクスパンションでの5分順張りロングは手数料負けのため見送る
//   - 方向のきっかけは古典パターン＋RSI/MACD のいずれか
//   - 出口は反対シグナル／TP・SL比率／最大保有のいずれか早いもの
//   - 発注しない。判定と記録のみ（執行は将来の検証通過後）

// Side は売買方向。
type Side string

const (
	Buy  Side = "buy"
	Sell Side = "sell"
	Hold Side = "hold"
)

// Config は V.Tエンジンの設定。
type Config struct {
	BarsSec    int64 // 足秒（60/300/900/3600：1分・5分・15分・1時間）
	MAFast     int
	MASlow     int
	BBPeriod   int
	BBK        float64
	RSIPeriod  int
	MACDFast   int
	MACDSlow   int
	MACDSig    int
	TPRatio    float64 // 利確比率（額面比）
	SLRatio    float64 // 損切比率（額面比）
	MaxHoldSec float64
	// TrendTFSec は方向判定に使う上位足の秒数（0＝入口足と同じ）。
	// 例：BarsSec=300・TrendTFSec=3600 で「1時間の方向に従い5分足で撃つ」。
	// 上位足の MA 方向に逆らう入口は出さない。
	TrendTFSec int64
	// FastTFSec は下位足の秒数（0＝無効）。入口確定前に下位足の同意を要求する。
	// 例：BarsSec=300・FastTFSec=60 で「5分足の形＋1分足の追認」で撃つ。
	// 下位足が強く逆行なら見送り、順行なら確信度を微加算する。
	// 不足・レンジ・不明時は中立（fail-open）。
	FastTFSec int64
	// TrailActivateR はトレイル発動益率（0＝無効）。
	// 建玉がこの益率を超えたら高値（売りは安値）を追い始める。
	TrailActivateR float64
	// TrailBackR は高値からの押し戻し許容率（0＝無効）。
	// 発動後にこの率だけ逆行したら利確離脱する（trailing_stop）。
	TrailBackR float64
	// TrailActivateATR/TrailBackATR/SLATR は ATR 倍率の安全網（0＝無効）。
	// 有効時は固定比率と ATR×倍率÷基準価格の大きい方を閾値にする。
	// 凪では固定値、荒れ場では ATR 側が勝ち、ノイズ狩りを防ぐ。
	// 既定は全て 0（従来の固定値動作を完全維持。調整は実測後に行う）。
	TrailActivateATR float64
	TrailBackATR     float64
	SLATR            float64
	// ImbMinHoldSec は板系離脱（imbalance_flip/absorption）の最低保有秒数
	// （0＝無効）。建玉直後の足未確定の板ノイズで切る往復（5秒・30秒離脱の
	// 実績）を防ぐ。SL・指標系・反転確定・時間切れには影響しない。
	// テクニカル定義は不変で、発動タイミングだけを縛るルール層。
	ImbMinHoldSec float64
	// ExitConfirmVotes は弱い出口（rsi_weak・imbalance_flip・absorption・
	// ma_break）に要求する確証票数（0＝無効＝従来動作）。
	// 票は単純多数決で、内訳はトレンド逆行・MACD減速・強い反対足・板逆行の
	// 最大4票。入口の多重確認を出口にも適用し、単一要因の誤射を防ぐ。
	// SL・時間切れ・反転2足確定・トレンド反転・含み益系・フィルタ済み反対
	// シグナルは対象外（無条件のまま）。
	ExitConfirmVotes int
	// ShortAllow はショート入口に許可するパターン列（空＝全許可）。
	// paper 検証の勝ち組＋天井圏の構造系。実証済みの負け組
	// （bearish_engulfing/rsi_fade/evening_star/macd_cross_down系/
	// div系/ma_cross_down/bb_bounce_down/pullback_to_bbmid_down）は除外。
	// 反対シグナル離脱も許可集合のみで判定される。
	ShortAllow []string
	// LongDeny/ShortDeny は paper 実績で負け越しが確定したパターンの
	// 追加除外集合（空＝除外なし）。vt-pattern-allow が週次で生成する。
	// ShortAllow（許可式）と併用時は両方が適用される（deny 優先）。
	// 出口の未加工反転判定（reversalExitPattern）は deny の影響を
	// 受けない。入口の厳選と出口の確実性を分離するため。
	LongDeny  []string
	ShortDeny []string
	// AllowCells は実証セル（side・pattern・regime）の許可集合。
	// 空＝無制限（従来動作）。非空時はセル外の新規を不発火にする。
	// vt-pattern-allow が手数料込みPFで生成する。deny より後に判定し、
	// 両方が適用される（deny 優先・allow は絞り込み）。
	AllowCells []AllowCell
	// SqueezeGate はレンジ帯での非ブレイク入口の停止（既定 true）。
	// レンジ時はブレイク系のみ発火させ、揉み合いでの往復出血を断つ。
	// 出口・反転判定には影響しない。false で無効化できる。
	SqueezeGate bool
	// WallFirmMin は板厚認定に必要な平均傾きの大きさ（既定 0.5）。
	// 夜間チューナーが安全範囲で動かす。0以下は 0.5 扱い。
	WallFirmMin float64
	// ExpCooldownBars は拡大失敗後に同方向の拡大を止めるバー数（既定 48）。
	// 0以下は 48 扱い。
	ExpCooldownBars int
	// ExpProgressMin は拡大の順行と認める価格進捗（既定 0.003）。
	// 0以下は 0.003 扱い。
	ExpProgressMin float64
	// SpoofHaltMs はspoof検出後の新規停止期間ms（既定 3600000＝1時間）。
	// 0以下は 3600000 扱い。
	SpoofHaltMs int64
	// GridCostRate はグリッド稼働に要求する片道コスト率（既定 0 は
	// gridCostRate 定数＝0.0015）。Maker rebate 前提で下げる場合のみ設定する。
	// 0 以下は定数扱い（従来動作を完全維持）。
	GridCostRate float64
	// ExitDeny は「弱い出口（rsi_weak・imbalance_flip・absorption・signal・
	// trend_flip 等）」の理由名を禁止する集合（空＝全許可＝従来動作）。
	// stop_loss / take_profit / max_hold / 反転確定系は安全のため常に許可する
	// （禁止できるのは利益を切りすぎるソフト出口のみ）。paper実績で
	// 「勝率0・損失のみ」と確認できた出口を切るために使う。
	ExitDeny []string
}

// DefaultConfig は推奨初期値を返す（5分足スイング＋1時間足の方向 gate）。
// 出口は反対シグナルが基本：上昇予測で買ったら下降予測が出るまで持つ。
// セーフティーネットとして SL のみ既定で有効（2%＝通常の小幅な
// 値動きでは触れない水準。明らかな損失拡大だけを切る）。TP・時間切れは
// 既定で無効（0）。利益は伸び切り（反対シグナル）で決済する設計。
func DefaultConfig() Config {
	return Config{
		BarsSec: 300, MAFast: 5, MASlow: 20,
		BBPeriod: 20, BBK: 2.0, RSIPeriod: 14,
		MACDFast: 12, MACDSlow: 26, MACDSig: 9,
		TPRatio: 0, SLRatio: 0.02, MaxHoldSec: 0,
		TrendTFSec: 3600, SqueezeGate: true,
		FastTFSec: 0, TrailActivateR: 0.015, TrailBackR: 0.008,
		WallFirmMin: WallFirmMinDefault, ExpCooldownBars: expCooldownBarsDefault,
		ExpProgressMin: expProgressMinDefault, SpoofHaltMs: spoofHaltMsDefault,
		ShortAllow: []string{
			"rci_turn_down", "bb_ride_momentum_down", "three_crows",
			"bb_breakdown", "pullback_to_ma_down", "overheat_macd", "rci_layer_down",
			"triple_top", "double_top", "spike_top_black", "spike_ma_break",
			"high_black", "lower_high_break", "long_dc", "dc_pullback",
			"dma_resist", "vol_surge_down", "rci_sar_down",
		},
	}
}

// Signal は1判定の出力。
// BarO/H/L/V は入口確定足の形状（事後検証用。live足と収集足の乖離の切り分けに使う）。
// MMState は入口時点の板厚・テープ体制（accumulate/distribute/balanced）。
// PFの体制別分離キー。Hold では空。
type Signal struct {
	Symbol     string
	Type       Side
	Price      float64
	Reason     string
	Timestamp  int64
	Confidence float64
	Pattern    string
	Regime     string
	RSI        float64
	PercentB   float64
	MACDHist   float64
	BarO       float64
	BarH       float64
	BarL       float64
	BarV       float64
	MMState    string
}

// trendTieBand は MA 乖離がこの比率未満ならレンジとみなす。
// 微差でのレジーム反転（往復ビンタの種）を防ぐ。
const trendTieBand = 0.0005

// patternMinATR はパターン足の実体が ATR の何倍以上必要か。
// ノイズ足の包み・ヒゲを無視するための有意性フィルタ。
const patternMinATR = 0.4

// minBodyRatio はパターン足の実体が価格の何割以上必要か（絶対下限）。
// ATR比だけでは凪でATRが潰れた際に塵足（LINK04:15型の実体0.5/2301など）が
// 通るため、価格比でも縛る。ロング・ショート対称（sigBody共有で両side適用）。
const minBodyRatio = 0.0005

// volumeActivityMin は活発さゲート：当足出来高が中央値の何倍以上必要か。
// 死んだ板での発火を防ぐ。出来高は足内の合計。
const volumeActivityMin = 0.5

// squeezeLookback はスクイーズ判定の窓（足数）。
const squeezeLookback = 20

// RCI 期間（株の達人式：短期9・中期26・長期52）。
const (
	rciFastPeriod  = 9
	rciSlowPeriod  = 26
	rciTrendPeriod = 52
)

// divOrder/divLookback はダイバージェンス検出のスイング次数・窓。
const (
	divOrder    = 3
	divLookback = 40
)

// OpenPos は paper が保持する建玉。入口時の regime・指標を保持し、
// 出口記録に引き継ぐ（出口時点の hold シグナルには regime が無いため）。
type OpenPos struct {
	Side         Side
	EntryPrice   float64
	EntryTime    int64
	Volume       float64
	EntryRegime  string
	EntryPattern string
	EntryRSI     float64
	EntryPB      float64
	EntryHist    float64
	EntryConf    float64
}

// Engine は銘柄別の足束ね＋判定器。
type Engine struct {
	cfg      Config
	builders map[string]*Builder
	trendBu  map[string]*Builder
	// fastBu は銘柄別の下位足 Builder（タイミング合流用）。
	fastBu map[string]*Builder
	// trail は銘柄別のトレイル高値・安値（建玉の EntryTime で世代管理）。
	trail map[string]*trailState
	// shortAllow はショート許可集合（空＝全許可）。
	shortAllow map[string]bool
	// longDeny/shortDeny は実績負け越しパターンの追加除外集合。
	longDeny  map[string]bool
	shortDeny map[string]bool
	// allowCells は実証セルの許可集合（空＝無制限）。
	allowCells map[string]bool
	// revExit は銘柄別の反転確定マーク（建玉の EntryTime で世代管理）。
	// 単発の反転では降ろさず、連続2足でのみ離脱する（往復ビンタ防止）。
	revExit map[string]*revMark
	// wickHi/wickLo は上ヒゲ・下ヒゲ拒否の最新足開始時刻（ms）。
	// 天井圏の売り拒否・底値圏の買い拒否を上位・入口の両足で見る。
	// 6時間で失効する。ロング・ショート対称の初動・継続判定に使う。
	wickHi map[string]int64
	wickLo map[string]int64
	// imbs は銘柄別の板不均衡（(bid-ask)/(bid+ask)）履歴。売り圧の
	// 監視（入口の裏取り・損切り前の離脱）に使う。最新20件保持。
	imbs map[string][]float64
	// walls は銘柄別の近接板厚imb（上位3段の(bid-ask)/(bid+ask)）履歴。
	// 合計量では希薄化する上位偏在（ETH13:00型の売り壁）を捉える。
	// live/paper/replay が毎tick供給する。最新20件保持。不明時は空のまま
	// fail-closed（確信が必要な入口投票では不発火、裏取り veto には使わない）。
	walls map[string][]float64
	// wflows は銘柄別の板厚変化率（直前tick比のimb差分）履歴。最新20件保持。
	// 壁の加速方向（食い尽くし・積み増し）を捉える。含み損玉の早期離脱に使う。
	// 記録のみではなく判定にも使うが、発火方向は損切り前倒しに限定する。
	wflows map[string][]float64
	// wallLv は銘柄別の上位3段数量履歴（買い合計・売り合計・mid・時刻）。
	// 壁のライフサイクル（出現・消失・食い尽くし）追跡用。最新180件保持
	// （約3分。spoofの出現→消失スパンを覆う）。P1データ層。
	wallLv map[string][]WallLevel
	// tape は銘柄別の約定テープ履歴。CVD・吸収検知に使う。最新200件保持。
	tape map[string][]TapeTick
	// expFire は銘柄別の拡大系パターン最終発火マーク。
	// 収縮→拡大が騙しに終わった直後の連続発火を止める
	// （10/02-10/04の帯歩き・ブレイク天井掴み対策）。バー数基準で判定し、
	// 0.3%以上の順行があれば解除する（本物の拡大は進むため）。
	expFire map[string]expMark
	// spoofHalt は銘柄別のspoof停止マーク（tactics.go）。
	spoofHalt map[string]spoofMark
}

// expMark は拡大系パターンの最終発火記録。
type expMark struct {
	bars int
	px   float64
	dir  int // +1 買い拡大 / -1 売り拡大
}

// ImbWindow はフロー判定に使う直近件数。
const ImbWindow = 10

// ImbPersistMin は方向認定に必要な符号持続率。
const ImbPersistMin = 0.6

// ImbFlipN は反対並びが何件続けば離脱するか。
const ImbFlipN = 5

// WallWindow は板厚投票に使う直近件数。
const WallWindow = 5

// WallFirmMinDefault は板厚認定に必要な平均傾きの大きさの既定値。
// 合計量の imbFlipMinMean(0.5) と対称。ATOM14:40型の弱傾き（0.2級）を落とし、
// LINK09:49型の上位偏在（0.7級）を通す。
const WallFirmMinDefault = 0.5

// imbFlipMinMean は離脱に必要な平均板傾きの大きさ。
// 符号の連続だけでなく確信的な圧力を要求する。弱い偏り（-0.1級）での
// 往復離脱を防ぐ。約半分以上の偏りが続いた状態に相当する。
const imbFlipMinMean = 0.5

// expCooldownBarsDefault は拡大失敗後に同方向の拡大を止めるバー数の既定値
// （5分足で約4時間）。
const expCooldownBarsDefault = 48

// expProgressMinDefault は拡大の順行と認める価格進捗の既定値（0.3%）。これ以上進めば
// 前回は騙しではなく本物の拡大として解除し、マークを更新する。
const expProgressMinDefault = 0.003

// expansionPatterns は拡大系（天井・底掴み注意）パターンの集合。
// クールダウンの対象。ride/breakout/new_high の買い売り対称。
var expansionPatterns = map[string]bool{
	"bb_breakout_up": true, "bb_breakdown": true,
	"bb_ride_momentum": true, "bb_ride_momentum_down": true,
	"new_high": true,
}

// AllowCell は実証済みの入口セル（方向・パターン・体制）。
type AllowCell struct {
	Side    string
	Pattern string
	Regime  string
}

// allowKey はセル照合キーを返す。
func allowKey(side, pattern, regime string) string {
	return side + "\x00" + pattern + "\x00" + regime
}

// NewEngine は Engine を生成する。
func NewEngine(cfg Config) *Engine {
	if cfg.BarsSec <= 0 {
		cfg.BarsSec = 300
	}
	allow := map[string]bool{}
	for _, p := range cfg.ShortAllow {
		if p = strings.TrimSpace(p); p != "" {
			allow[p] = true
		}
	}
	mkDeny := func(xs []string) map[string]bool {
		m := map[string]bool{}
		for _, p := range xs {
			if p = strings.TrimSpace(p); p != "" {
				m[p] = true
			}
		}
		return m
	}
	mkAllow := func(xs []AllowCell) map[string]bool {
		m := map[string]bool{}
		for _, c := range xs {
			side := strings.ToLower(strings.TrimSpace(c.Side))
			pat := strings.TrimSpace(c.Pattern)
			reg := strings.TrimSpace(c.Regime)
			if (side != "buy" && side != "sell") || pat == "" || reg == "" {
				continue
			}
			m[allowKey(side, pat, reg)] = true
		}
		return m
	}
	return &Engine{cfg: cfg, builders: map[string]*Builder{}, trendBu: map[string]*Builder{}, fastBu: map[string]*Builder{}, imbs: map[string][]float64{}, walls: map[string][]float64{}, wflows: map[string][]float64{}, tape: map[string][]TapeTick{}, wallLv: map[string][]WallLevel{}, trail: map[string]*trailState{}, shortAllow: allow, longDeny: mkDeny(cfg.LongDeny), shortDeny: mkDeny(cfg.ShortDeny), allowCells: mkAllow(cfg.AllowCells), revExit: map[string]*revMark{}, wickHi: map[string]int64{}, wickLo: map[string]int64{}, expFire: map[string]expMark{}, spoofHalt: map[string]spoofMark{}}
}

// builderFor は銘柄別の Builder を返す。
func (e *Engine) builderFor(symbol string) *Builder {
	b := e.builders[symbol]
	if b == nil {
		b = NewBuilder(e.cfg.BarsSec, 300)
		e.builders[symbol] = b
	}
	return b
}

// SplitPatterns はカンマ列をパターン集合に変換する（空要素は落とす）。
// 空文字→空集合（非nil。allow-all の意味で使うため nil と区別する）。
func SplitPatterns(raw string) []string {
	out := []string{}
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// trailState はトレイル用の高値・安値・%B両極（EntryTime で建玉世代を識別）。
type trailState struct {
	entryTime int64
	high      float64
	low       float64
	maxPb     float64
	minPb     float64
}

// revMark は反転確定の印（連続2足の反転で離脱する）。
// armed は前足で反転が出たこと、barStart はその足の開始時刻。
// 別足で再び反転が出たら確定。途切れたら armed を落とす。
type revMark struct {
	entryTime int64
	armed     bool
	barStart  int64
}

// breakoutExempt は板・テープ裏取りを免除するブレイク系パターン。
// 休み売りを食って伸びるのが定義のため、逆フローでも撃つ。
var breakoutExempt = map[string]bool{
	"bb_breakout_up": true, "bb_ride_momentum": true, "new_high": true,
	"bb_breakdown": true, "bb_ride_momentum_down": true,
}

// squeezeBreakout はレンジ帯（スクイーズ）で発火を許可するパターン集合。
// レンジ時は水準ブレイク系のみ撃ち、揉み合いでの往復出血を断つ。
// 抜けた後の継続（ride）・新高値・天井底構造の neck ブレイクを含む。
var squeezeBreakout = map[string]bool{
	"bb_breakout_up": true, "bb_breakdown": true,
	"bb_ride_momentum": true, "bb_ride_momentum_down": true,
	"new_high":   true,
	"double_top": true, "double_bottom": true,
	"triple_top": true, "inv_head_shoulder": true,
}

// squeezeGateLookback はスクイーズ判定の窓（足数）。
const squeezeGateLookback = 60

// squeezeGateRank はスクイーズ認定の分位（これ未満でレンジ帯）。
const squeezeGateRank = 0.25

// squeezeGateFloor は絶対帯域の下限（これ未満は無条件でレンジ帯）。
// 帯域は mid 比率のため銘柄非依存。死んだ板の微動を拾う。
const squeezeGateFloor = 0.005

// squeezeGateDrift はレンジ帯の正味変位の上限（60足で2%未満）。
// 上昇一途では帯域が価格上昇で機械的に圧縮されるため、分位だけでは
// トレンドを絞りと誤認する。正味に動いていたらレンジではない。
const squeezeGateDrift = 0.02

// chopGateSep は方向なし判定のMA乖離しきい値（ATR比）。
// 短期線と長期線の差が ATR の半分未満なら方向なしとみなす。
// 帯域幅では区別できない「広い方向なし揉み合い」を捉える。
// 方向の持続性そのものを見るため、トレンド初期の拡大とも区別できる。
const chopGateSep = 0.5

// jumpGateMult は飛び足判定の倍率（ATR比）。
// 直近3足のうち2足以上が ATR のこの倍率を超える実値動きで、
// かつ3足の正味変位が ATR 未満なら、値飛びの往復揉み合いとみなす。
// ロウソク足がつぶれた相場（ヒゲの往復・板飛び）での新規を止める。
// 一方向への連続拡大（正味変位が大きい）は対象外とする。
const jumpGateMult = 2.5

// squeezedEntry は入口足がレンジ帯かを返す。
// 直近60足の正味変位が小さく、かつ次のいずれかのとき真：
//  1. 現在帯域が直近60窓の下位25%以内（コイル）、
//  2. 絶対帯域が極小（死んだ板）、
//  3. MA乖離が ATR の半分未満（広い方向なし揉み合い）。
//
// 足不足（80本未満）は不明扱いで偽（fail-open。冷間始動を殺さない）。
func (e *Engine) squeezedEntry(symbol string) bool {
	return e.squeezedSeries(e.builderFor(symbol).Bars())
}

// squeezedSeries は足列がレンジ帯かを返す純粋部。
// グリッド入口では確定足自身の値動きで帯が膨らむため、確定足を除いた
// 列（コイル状態）で判定する。両side対称の定義として共用する。
func (e *Engine) squeezedSeries(bars []Bar) bool {
	if len(bars) < squeezeGateLookback+e.cfg.BBPeriod {
		return false
	}
	closes := Closes(bars)
	base := closes[len(closes)-squeezeGateLookback]
	last := closes[len(closes)-1]
	if base <= 0 {
		return false
	}
	drift := (last - base) / base
	if drift < 0 {
		drift = -drift
	}
	if drift >= squeezeGateDrift {
		return false
	}
	// 値飛びの往復揉み合い：直近3足のうち2足以上が ATR の jumpGateMult 倍を
	// 超える実値動きで、かつ3足の正味変位が ATR 未満。つぶれた足での
	// 新規を止める。一方向への連続拡大は正味変位が大きく対象外となる。
	atrJ := ATR(bars, 14)
	if atrJ > 0 && len(bars) >= 3 {
		jumps := 0
		for _, b := range bars[len(bars)-3:] {
			if Range(b) >= jumpGateMult*atrJ {
				jumps++
			}
		}
		if jumps >= 2 {
			net := closes[len(closes)-1] - closes[len(closes)-3]
			if net < 0 {
				net = -net
			}
			if net < atrJ {
				return true
			}
		}
	}
	// 方向なし揉み合い：MA乖離が ATR の半分未満。
	maF := SMA(closes, e.cfg.MAFast)
	maS := SMA(closes, e.cfg.MASlow)
	atr := ATR(bars, 14)
	if maF > 0 && maS > 0 && atr > 0 {
		sep := maF - maS
		if sep < 0 {
			sep = -sep
		}
		if sep < chopGateSep*atr {
			return true
		}
	}
	cur := Bandwidth(closes, e.cfg.BBPeriod, e.cfg.BBK)
	if cur <= 0 {
		return false
	}
	if cur < squeezeGateFloor {
		return true
	}
	lo := 0
	for j := 0; j < squeezeGateLookback; j++ {
		end := len(closes) - j
		if end < e.cfg.BBPeriod {
			break
		}
		if Bandwidth(closes[:end], e.cfg.BBPeriod, e.cfg.BBK) <= cur {
			lo++
		}
	}
	return float64(lo)/float64(squeezeGateLookback) < squeezeGateRank
}

// trendFor は銘柄別の上位足 Builder を返す。
// TrendTFSec 未設定・入口足以下の場合は入口足をそのまま使う。
func (e *Engine) trendFor(symbol string) *Builder {
	if e.cfg.TrendTFSec <= 0 || e.cfg.TrendTFSec <= e.cfg.BarsSec {
		return e.builderFor(symbol)
	}
	b := e.trendBu[symbol]
	if b == nil {
		b = NewBuilder(e.cfg.TrendTFSec, 300)
		e.trendBu[symbol] = b
	}
	return b
}

// fastFor は銘柄別の下位足 Builder を返す。
// FastTFSec 未設定・入口足以上の場合は nil（合流なし）。
func (e *Engine) fastFor(symbol string) *Builder {
	if e.cfg.FastTFSec <= 0 || e.cfg.FastTFSec >= e.cfg.BarsSec {
		return nil
	}
	b := e.fastBu[symbol]
	if b == nil {
		b = NewBuilder(e.cfg.FastTFSec, 300)
		e.fastBu[symbol] = b
	}
	return b
}

// fastTrend は下位足の方向を返す（不足時は ""＝中立・fail-open）。
func (e *Engine) fastTrend(symbol string) string {
	fb := e.fastFor(symbol)
	if fb == nil {
		return ""
	}
	return trendOf(Closes(fb.Bars()), e.cfg.MAFast, e.cfg.MASlow)
}

// trendOf は終値列の MA 方向を返す（tie-band＋傾き付き）。
// 位置（MAfast と MAslow の上下）に加え、fast MA の傾き（現足と前足の差）
// が逆行している場合はレンジ扱いにし、往復ビンタを防ぐ。傾き不明時（足不足）
// は位置のみで判定する（fail-open。冷間始動を殺さない）。
func trendOf(closes []float64, maFast, maSlow int) string {
	maF := SMA(closes, maFast)
	maS := SMA(closes, maSlow)
	if maF <= 0 || maS <= 0 {
		return ""
	}
	slope := 0.0
	if len(closes) > maFast {
		if prev := SMA(closes[:len(closes)-1], maFast); prev > 0 {
			slope = maF - prev
		}
	}
	rel := maF - maS
	if rel < 0 {
		rel = -rel
	}
	if rel <= trendTieBand*maS {
		return "range"
	}
	if maF > maS {
		if slope < 0 {
			return "range"
		}
		return "trend_up"
	}
	if slope > 0 {
		return "range"
	}
	return "trend_down"
}

// Observe はティックを投入し、確定足ができたら判定する。
// bidVol/askVol から板不均衡を記録する（売り圧監視用）。
// 建玉なし時の入口判定を行い、シグナル（hold 含む）と確定足を返す。
func (e *Engine) Observe(symbol string, tsMs int64, mid, bidVol, askVol float64) (*Signal, *Bar) {
	if total := bidVol + askVol; total > 0 {
		imb := (bidVol - askVol) / total
		hist := append(e.imbs[symbol], imb)
		if len(hist) > 20 {
			hist = hist[len(hist)-20:]
		}
		e.imbs[symbol] = hist
	}
	bar := e.builderFor(symbol).Add(tsMs, mid, bidVol+askVol)
	// 上位足も同じティックで進める（TrendTFSec 未設定時は入口足と同一）。
	e.trendFor(symbol).Add(tsMs, mid, bidVol+askVol)
	// 下位足も同じティックで進める（FastTFSec 未設定・入口足以上は作らない）。
	if fb := e.fastFor(symbol); fb != nil {
		fb.Add(tsMs, mid, bidVol+askVol)
	}
	if bar == nil {
		return nil, nil
	}
	return e.EvalEntry(symbol, bar.Start+e.cfg.BarsSec*1000), bar
}

// flowDir は直近の不均衡の多数派符号を返す（+1/-1、不明は 0）。
func (e *Engine) flowDir(symbol string) int {
	hist := e.imbs[symbol]
	n := len(hist)
	if n > ImbWindow {
		hist = hist[n-ImbWindow:]
		n = ImbWindow
	}
	if n < 6 {
		return 0
	}
	pos := 0
	for _, v := range hist {
		if v >= 0 {
			pos++
		}
	}
	if float64(pos)/float64(n) >= ImbPersistMin {
		return 1
	}
	if float64(n-pos)/float64(n) >= ImbPersistMin {
		return -1
	}
	return 0
}

// WallLevel はある時刻の上位3段数量スナップショット。
type WallLevel struct {
	TS   int64
	Bid3 float64
	Ask3 float64
	Mid  float64
}

// wallLevelKeep は壁数量履歴の保持件数。
const wallLevelKeep = 180

// TrackWall は上位3段数量を履歴に積む。最新180件保持。
// live/paper/replay が板スナップショット毎に呼ぶ（P1データ層）。
// 不正値（数量負・mid非正）は捨てる。
func (e *Engine) TrackWall(symbol string, tsMs int64, bid3, ask3, mid float64) {
	if bid3 < 0 || ask3 < 0 || mid <= 0 || bid3+ask3 <= 0 {
		return
	}
	hist := append(e.wallLv[symbol], WallLevel{TS: tsMs, Bid3: bid3, Ask3: ask3, Mid: mid})
	if len(hist) > wallLevelKeep {
		hist = hist[len(hist)-wallLevelKeep:]
	}
	e.wallLv[symbol] = hist
}

// AddWall は近接板厚imbを履歴に追加する。最新20件保持。
// live/paper/replay が板スナップショットの上位3段から算出して毎tick呼ぶ。
// 同時に直前比の変化率を wflows に積む（壁の加速検知用）。
func (e *Engine) AddWall(symbol string, imb float64) {
	hist := e.walls[symbol]
	if n := len(hist); n > 0 {
		d := imb - hist[n-1]
		fl := append(e.wflows[symbol], d)
		if len(fl) > 20 {
			fl = fl[len(fl)-20:]
		}
		e.wflows[symbol] = fl
	}
	hist = append(hist, imb)
	if len(hist) > 20 {
		hist = hist[len(hist)-20:]
	}
	e.walls[symbol] = hist
}

// wallAgree は近接板厚が want 方向（+1買い/-1売り）に確信的に傾いているかを返す。
// 直近窓の全件が同符号かつ平均の大きさが WallFirmMin 以上を要求する。
// 不明・不足時は偽（fail-closed。確信が必要な入口投票でのみ使い、
// 裏取り veto には使わないため冷間始動を殺さない）。
func (e *Engine) wallAgree(symbol string, want int) bool {
	hist := e.walls[symbol]
	n := len(hist)
	if n > WallWindow {
		hist = hist[n-WallWindow:]
		n = WallWindow
	}
	if n < WallWindow {
		return false
	}
	sum := 0.0
	for _, v := range hist {
		if want > 0 && v <= 0 {
			return false
		}
		if want < 0 && v >= 0 {
			return false
		}
		sum += v
	}
	mean := sum / float64(n)
	if want > 0 {
		return mean >= e.wallFirmMin()
	}
	return mean <= -e.wallFirmMin()
}

// wallFirmMin は板厚認定しきい値を返す（設定値、0以下は既定値）。
func (e *Engine) wallFirmMin() float64 {
	if e.cfg.WallFirmMin > 0 {
		return e.cfg.WallFirmMin
	}
	return WallFirmMinDefault
}

// expCooldown は拡大クールダウンのバー数を返す（設定値、0以下は既定値）。
func (e *Engine) expCooldown() int {
	if e.cfg.ExpCooldownBars > 0 {
		return e.cfg.ExpCooldownBars
	}
	return expCooldownBarsDefault
}

// expProgress は拡大順行の進捗率を返す（設定値、0以下は既定値）。
func (e *Engine) expProgress() float64 {
	if e.cfg.ExpProgressMin > 0 {
		return e.cfg.ExpProgressMin
	}
	return expProgressMinDefault
}

// spoofHaltWindow はspoof停止期間msを返す（設定値、0以下は既定値）。
func (e *Engine) spoofHaltWindow() int64 {
	if e.cfg.SpoofHaltMs > 0 {
		return e.cfg.SpoofHaltMs
	}
	return spoofHaltMsDefault
}

// flowAgree は板厚・テープのいずれかで確信的な順行が取れているかを返す。
// 反転形の肯定投票用。mmState==want と同値。ただし両方ともデータ不在の
// 冷間時は真（fail-open。冷間始動・短い足列の単体テストを殺さない）。
// ウォームアップで板厚は先読みされるため、実稼働で不在になるのは
// CSV不在の初回約25秒のみ。データがあるのに弱い時は偽。両side対称。
func (e *Engine) flowAgree(symbol string, want int) bool {
	if _, ok := e.CVDRatio(symbol); len(e.walls[symbol]) == 0 && !ok {
		return true
	}
	return e.mmState(symbol) == want
}

// mmState は板厚・テープからMM注文圧体制を分類する。
// +1 accumulate（買い圧体制）、-1 distribute（売り圧）、0 均衡・不明。
// 壁確信またはテープ強確信のいずれか一方向だけで認定する。両方向の確信が
// 同時に立つことはなく（同一窓・同一CVDの符号は一意）、不明・弱確信・
// 方向衝突時は 0 を返す。入口の肯定投票と distribute 禁止はこの一本に
// 集約する。両side対称・PF分離の記録キー。
func (e *Engine) mmState(symbol string) int {
	walls := e.walls[symbol]
	tapeCold := true
	buyTape, sellTape := false, false
	if ratio, ok := e.CVDRatio(symbol); ok {
		tapeCold = false
		if ratio >= TapeAlignMin {
			buyTape = true
		} else if ratio <= -TapeAlignMin {
			sellTape = true
		}
	}
	if len(walls) == 0 && tapeCold {
		return 0
	}
	buyWall := e.wallAgree(symbol, 1)
	sellWall := e.wallAgree(symbol, -1)
	if (buyWall || buyTape) && !(sellWall || sellTape) {
		return 1
	}
	if (sellWall || sellTape) && !(buyWall || buyTape) {
		return -1
	}
	return 0
}

// confirmedTopBypass は除外中の天井3種をヒゲ＋板確信で迂回してよいかを返す。
// 対象は evening_star / bearish_engulfing / macd_cross_down(_high)。
// ロング側に許可制はないためショート専用。両side対称の思想のもと、
// 買い側の除外（LongDeny）は週次較正に委ね、ここでは触らない。
func (e *Engine) confirmedTopBypass(symbol string, tsMs int64, pattern string) bool {
	switch pattern {
	case "evening_star", "bearish_engulfing", "macd_cross_down", "macd_cross_down_high":
	default:
		return false
	}
	return e.wickFreshHi(symbol, tsMs) && e.flowAgree(symbol, -1)
}

// ApplyDeny はdeny集合を置き換える（日次キルスイッチの再読込用）。
// 再起動なしで負け越しパターンの停止を反映する。nilは空扱い。
func (e *Engine) ApplyDeny(longDeny, shortDeny []string) {
	mk := func(xs []string) map[string]bool {
		m := map[string]bool{}
		for _, p := range xs {
			if p = strings.TrimSpace(p); p != "" {
				m[p] = true
			}
		}
		return m
	}
	e.longDeny = mk(longDeny)
	e.shortDeny = mk(shortDeny)
}

// ApplyAllow は実証セル集合を置き換える（再読込用）。
// 空・nilは無制限に戻す。壊れたセル（方向・空名）は落とす。
func (e *Engine) ApplyAllow(cells []AllowCell) {
	m := map[string]bool{}
	for _, c := range cells {
		side := strings.ToLower(strings.TrimSpace(c.Side))
		pat := strings.TrimSpace(c.Pattern)
		reg := strings.TrimSpace(c.Regime)
		if (side != "buy" && side != "sell") || pat == "" || reg == "" {
			continue
		}
		m[allowKey(side, pat, reg)] = true
	}
	e.allowCells = m
}

// TuneParams は夜間チューナーが動かす安全ノブの集合。
// 全て範囲付きで、範囲外の値は無視する（壊れた調整を適用しない）。
type TuneParams struct {
	MinConfidence    float64
	ExitConfirmVotes int
	WallFirmMin      float64
	ExpCooldownBars  int
	ExpProgressMin   float64
	SpoofHaltMs      int64
}

// ApplyTune は安全ノブを置き換える（夜間チューナーの再読込用）。
// 範囲外の値は無視する。数値ノブの0値（未設定）は既存値を維持する。
// ExitConfirmVotes のみ 0 が有効値（無効化）のため明示的に適用する。
func (e *Engine) ApplyTune(t TuneParams) {
	if t.ExitConfirmVotes >= 0 && t.ExitConfirmVotes <= 4 {
		e.cfg.ExitConfirmVotes = t.ExitConfirmVotes
	}
	if t.WallFirmMin >= 0.2 && t.WallFirmMin <= 0.8 {
		e.cfg.WallFirmMin = t.WallFirmMin
	}
	if t.ExpCooldownBars >= 12 && t.ExpCooldownBars <= 144 {
		e.cfg.ExpCooldownBars = t.ExpCooldownBars
	}
	if t.ExpProgressMin >= 0.001 && t.ExpProgressMin <= 0.01 {
		e.cfg.ExpProgressMin = t.ExpProgressMin
	}
	if t.SpoofHaltMs >= 900000 && t.SpoofHaltMs <= 14400000 {
		e.cfg.SpoofHaltMs = t.SpoofHaltMs
	}
}

// expansionBlocked は拡大系パターンの連続発火を止めるべきかを返す。
// 前回発火から48バー以内に0.3%以上順行していなければ、前回は騙しと
// みなして見送る。順行・逆方向・期限切れは通す。バー数基準のため
// tsMsの固定値テストでも再現性がある。
func (e *Engine) expansionBlocked(symbol string, dir int, px float64, barCount int) bool {
	m, ok := e.expFire[symbol]
	if !ok || m.dir != dir {
		return false
	}
	if barCount-m.bars >= e.expCooldown() {
		return false
	}
	if m.px <= 0 {
		return false
	}
	prog := e.expProgress()
	if dir > 0 {
		return (px-m.px)/m.px < prog
	}
	return (m.px-px)/m.px < prog
}

// recordExpansion は拡大系パターンの発火を記録する。
func (e *Engine) recordExpansion(symbol string, dir int, px float64, barCount int) {
	e.expFire[symbol] = expMark{bars: barCount, px: px, dir: dir}
}

// mmStateName は体制値を記録名に変換する。
func mmStateName(v int) string {
	if v > 0 {
		return "accumulate"
	}
	if v < 0 {
		return "distribute"
	}
	return "balanced"
}

// wickExpiryMs はヒゲ拒否の有効期限（6時間）。
const wickExpiryMs = int64(6 * 3600 * 1000)

// isUpperWick は上ヒゲ拒否足かを返す（純粋関数・テスト対象）。
// 上ヒゲが実体の2倍以上・下ヒゲが上ヒゲ以下・実体がATR4割以上・価格比下限以上。
// 天井圏の売り拒否（1時間21-22時型の長い上ヒゲ）の検出に使う。対称に下ヒゲあり。
func isUpperWick(b Bar, atr, ref float64) bool {
	if atr <= 0 || ref <= 0 {
		return false
	}
	body := Body(b)
	if body < patternMinATR*atr || body < minBodyRatio*ref {
		return false
	}
	var uw, lw float64
	if b.O > b.C {
		uw = b.H - b.O
		lw = b.C - b.L
	} else {
		uw = b.H - b.C
		lw = b.O - b.L
	}
	return uw >= 2*body && lw <= uw
}

// isLowerWick は下ヒゲ拒否足かを返す（対称形）。
func isLowerWick(b Bar, atr, ref float64) bool {
	if atr <= 0 || ref <= 0 {
		return false
	}
	body := Body(b)
	if body < patternMinATR*atr || body < minBodyRatio*ref {
		return false
	}
	var uw, lw float64
	if b.O > b.C {
		uw = b.H - b.O
		lw = b.C - b.L
	} else {
		uw = b.H - b.C
		lw = b.O - b.L
	}
	return lw >= 2*body && uw <= lw
}

// observeWicks は上位・入口足のヒゲ拒否を記録する。
// 上位の直近2本・入口の直近12本を見る（1時間の確定待ちをせず5分で先行検出）。
func (e *Engine) observeWicks(symbol string) {
	if tb := e.trendFor(symbol).Bars(); len(tb) >= 14 {
		atr := ATR(tb, 14)
		ref := tb[len(tb)-1].C
		for _, b := range tb[len(tb)-2:] {
			if isUpperWick(b, atr, ref) && b.Start > e.wickHi[symbol] {
				e.wickHi[symbol] = b.Start
			}
			if isLowerWick(b, atr, ref) && b.Start > e.wickLo[symbol] {
				e.wickLo[symbol] = b.Start
			}
		}
	}
	if eb := e.builderFor(symbol).Bars(); len(eb) >= 14 {
		atr := ATR(eb, 14)
		ref := eb[len(eb)-1].C
		from := len(eb) - 12
		if from < 0 {
			from = 0
		}
		for _, b := range eb[from:] {
			if isUpperWick(b, atr, ref) && b.Start > e.wickHi[symbol] {
				e.wickHi[symbol] = b.Start
			}
			if isLowerWick(b, atr, ref) && b.Start > e.wickLo[symbol] {
				e.wickLo[symbol] = b.Start
			}
		}
	}
}

// wickFreshHi/Lo はヒゲ拒否が有効期限内かを返す。
func (e *Engine) wickFreshHi(symbol string, tsMs int64) bool {
	t, ok := e.wickHi[symbol]
	return ok && t >= 0 && tsMs >= t && tsMs-t <= wickExpiryMs
}

// wickFreshLo は下ヒゲ拒否が有効期限内かを返す（対称形）。
func (e *Engine) wickFreshLo(symbol string, tsMs int64) bool {
	t, ok := e.wickLo[symbol]
	return ok && t >= 0 && tsMs >= t && tsMs-t <= wickExpiryMs
}

// trendWith は上位足が side の順行かを返す（勝ち玉の離脱配分用）。
// trend_up なら Buy 建玉を伸ばし、trend_down なら Sell 建玉を伸ばす。
// range・不明時は偽（速く降りる安全側）。両side対称。
func (e *Engine) trendWith(symbol string, side Side) bool {
	bars := e.trendFor(symbol).Bars()
	if len(bars) == 0 {
		return false
	}
	reg := trendOf(Closes(bars), e.cfg.MAFast, e.cfg.MASlow)
	return (side == Buy && reg == "trend_up") || (side == Sell && reg == "trend_down")
}

// wallRunover は板厚が建玉への逆行方向に加速しているかを返す。
// 直近3件の変化が全て逆行符号かつ合計が WallRunMin 以上のときに真。
// 含み損玉の損切り前倒し専用（勝ち玉・新規には使わない）。
// 不足時は偽（fail-closed）。両side対称。
func (e *Engine) wallRunover(symbol string, side Side) bool {
	fl := e.wflows[symbol]
	n := len(fl)
	if n < 3 {
		return false
	}
	fl = fl[n-3:]
	sum := 0.0
	for _, d := range fl {
		if side == Buy && d >= 0 {
			return false
		}
		if side == Sell && d <= 0 {
			return false
		}
		sum += d
	}
	if side == Buy {
		return sum <= -WallRunMin
	}
	return sum >= WallRunMin
}

// WallRunMin は逆行加速とみなす3件合計の変化量。
const WallRunMin = 0.3

// EvalEntry は確定足時点の入口判定を行う。
func (e *Engine) EvalEntry(symbol string, tsMs int64) *Signal {
	bars := e.builderFor(symbol).Bars()
	mkHold := func(reason string) *Signal {
		last := 0.0
		if len(bars) > 0 {
			last = bars[len(bars)-1].C
		}
		return &Signal{Symbol: symbol, Type: Hold, Price: last, Reason: reason, Timestamp: tsMs}
	}
	need := e.cfg.MASlow + e.cfg.MACDSig + 2
	if len(bars) < need {
		return mkHold("warming up")
	}
	// 足の連続性：直近3本が欠けていたら指標が歪むため入口を見送る。
	// 収集ギャップ（cronの3分停止）・配信停止時の誤射防止。出口は止めない。
	// ロング・ショート対称（regime分岐の前に両side共通で適用）。
	if !barsContinuous(bars, e.cfg.BarsSec*1000) {
		return mkHold("time gap")
	}
	// ヒゲ拒否の記録：上位・入口足の天井・底拒否を保持する（6時間有効）。
	e.observeWicks(symbol)
	closes := Closes(bars)
	_, upper, lower := Bollinger(closes, e.cfg.BBPeriod, e.cfg.BBK)
	rsi := RSI(closes, e.cfg.RSIPeriod)
	_, _, hist := MACD(closes, e.cfg.MACDFast, e.cfg.MACDSlow, e.cfg.MACDSig)
	prevHist := 0.0
	if len(closes) >= e.cfg.MACDSlow+e.cfg.MACDSig+1 {
		_, _, prevHist = MACD(closes[:len(closes)-1], e.cfg.MACDFast, e.cfg.MACDSlow, e.cfg.MACDSig)
	}
	prevRSI := 50.0
	if len(closes) >= e.cfg.RSIPeriod+2 {
		prevRSI = RSI(closes[:len(closes)-1], e.cfg.RSIPeriod)
	}
	pb := PercentB(closes[len(closes)-1], upper, lower)
	maF := SMA(closes, e.cfg.MAFast)
	maS := SMA(closes, e.cfg.MASlow)
	bbMid := SMA(closes, e.cfg.BBPeriod)
	macdLine, _, _ := MACD(closes, e.cfg.MACDFast, e.cfg.MACDSlow, e.cfg.MACDSig)
	// 長期MA乖離：長期線付近でのGCは下げ止まりの裏付けになる（株の達人式）。
	longMA := SMA(closes, 75)
	nearLongMA := longMA > 0
	if nearLongMA {
		dev := closes[len(closes)-1] - longMA
		if dev < 0 {
			dev = -dev
		}
		nearLongMA = dev/longMA < 0.01
	}
	// 5本内のRSI底・天井（深い売られすぎ・買われすぎの裏取り用）。
	minRSI5 := rsi
	maxRSI5 := rsi
	for k := 1; k < 5 && len(closes)-k >= e.cfg.RSIPeriod+1; k++ {
		v := RSI(closes[:len(closes)-k], e.cfg.RSIPeriod)
		if v < minRSI5 {
			minRSI5 = v
		}
		if v > maxRSI5 {
			maxRSI5 = v
		}
	}
	// RCI（短期の反転＋長期の重ね）。prev は1本前終点で算出する。
	rciFast, prevRciFast := RCI(closes, rciFastPeriod), 0.0
	if len(closes) > rciFastPeriod {
		prevRciFast = RCI(closes[:len(closes)-1], rciFastPeriod)
	}
	minRciFast3 := rciFast
	maxRciFast3 := rciFast
	minRciFast5 := rciFast
	maxRciFast5 := rciFast
	for k := 1; k < 3 && len(closes)-k >= rciFastPeriod; k++ {
		v := RCI(closes[:len(closes)-k], rciFastPeriod)
		if v < minRciFast3 {
			minRciFast3 = v
		}
		if v > maxRciFast3 {
			maxRciFast3 = v
		}
	}
	for k := 1; k < 5 && len(closes)-k >= rciFastPeriod; k++ {
		v := RCI(closes[:len(closes)-k], rciFastPeriod)
		if v < minRciFast5 {
			minRciFast5 = v
		}
		if v > maxRciFast5 {
			maxRciFast5 = v
		}
	}
	// パラボリック転換（RCI 複合用）。
	sarFlipDir := sarFlip(bars, ParabolicSAR(bars, 0.02, 0.2))
	rciSlow := RCI(closes, rciSlowPeriod)
	rciTrend := RCI(closes, rciTrendPeriod)
	// MAクロス（BB中心線×短期線の転換検出用）。
	maCrossUp, maCrossDown := false, false
	if len(closes) >= e.cfg.MASlow+1 {
		pf, ps := SMA(closes[:len(closes)-1], e.cfg.MAFast), SMA(closes[:len(closes)-1], e.cfg.MASlow)
		if pf > 0 && ps > 0 {
			maCrossUp = pf <= ps && maF > maS
			maCrossDown = pf >= ps && maF < maS
		}
	}
	// 天井底構造（株の達人 売り時/買い時パターン由来）。
	triTopNeck, triTopOK := tripleTopNeck(closes)
	dblTopNeck, dblTopOK := doubleTopNeck(closes)
	dblBotNeck, dblBotOK := doubleBottomNeck(closes)
	invHSNeck, invHSOk := invHeadShoulderNeck(closes)
	lastC := closes[len(closes)-1]
	prevC := closes[len(closes)-2]
	hi20prev := 0.0
	if len(closes) > 20 {
		hi20prev = maxClose(closes[:len(closes)-1], 20)
	}
	sma10 := SMA(closes, 10)
	prevSMA10 := 0.0
	if len(closes) > 10 {
		prevSMA10 = SMA(closes[:len(closes)-1], 10)
	}
	sma75 := SMA(closes, 75)
	sma200 := SMA(closes, 200)
	prevSma200 := 0.0
	if len(closes) > 200 {
		prevSma200 = SMA(closes[:len(closes)-1], 200)
	}
	rise10 := risePct(closes, 10)
	drop5 := dropPct(closes, 5)
	longDCFresh := maCrossDownFresh(closes, 75, 200, 5)
	longDCActive := sma75 > 0 && sma200 > 0 && sma75 < sma200
	dcFresh30 := maCrossDownFresh(closes, 75, 200, 30)
	lowerHigh := false
	if sh := swingHighs(closes, 3, 60); len(sh) >= 2 {
		a, b := sh[len(sh)-2], sh[len(sh)-1]
		lowerHigh = closes[b] < closes[a]
	}
	// MACDヒストグラムの反転頻度（GC/DC繰り返しの保ち合いは休む）。
	flips := HistFlips(closes, e.cfg.MACDFast, e.cfg.MACDSlow, e.cfg.MACDSig, 5)
	// バンド幅の拡大有無（順張りは拡大初動だけ拾う）。
	bwNow, bwPrev := Bandwidth(closes, e.cfg.BBPeriod, e.cfg.BBK), 0.0
	if len(closes) > e.cfg.BBPeriod {
		bwPrev = Bandwidth(closes[:len(closes)-1], e.cfg.BBPeriod, e.cfg.BBK)
	}
	expanding := bwNow > 0 && bwNow >= bwPrev
	// ダイバージェンス（株価と指標の逆行）。
	rsiSer := RSISeries(closes, e.cfg.RSIPeriod)
	hSer := MACDHistSeries(closes, e.cfg.MACDFast, e.cfg.MACDSlow, e.cfg.MACDSig)
	bullRSIDiv := rsi <= 55 && BullishDivergence(closes, rsiSer, divOrder, divLookback)
	bearRSIDiv := rsi >= 45 && BearishDivergence(closes, rsiSer, divOrder, divLookback)
	bullMACDDiv := BullishDivergence(closes, hSer, divOrder, divLookback)
	bearMACDDiv := BearishDivergence(closes, hSer, divOrder, divLookback)
	regime := trendOf(Closes(e.trendFor(symbol).Bars()), e.cfg.MAFast, e.cfg.MASlow)
	if regime == "" {
		return mkHold("no trend basis")
	}
	// 下位足の合流：強逆行は各レジーム枝で見送り、順行は確信度を微加算する。
	// 不足・レンジ・不明時は中立（fail-open）。
	ft := e.fastTrend(symbol)
	fastBonus := 0.0
	// 活発さゲート：死んだ板では何も撃たない（全トリガー共通）。
	if medV := MedianVolume(bars, 20); medV <= 0 || bars[len(bars)-1].V < volumeActivityMin*medV {
		return mkHold("low activity")
	}
	// 迷い足では撃たない：十字線の確定足は方向不定のため全トリガー共通で見送る。
	if IsDoji(bars[len(bars)-1]) {
		return mkHold("doji indecision")
	}
	// パターン足の有意性：ノイズ足（実体が ATR 未満級）は無視する。
	// ATR ゼロ（完全凪）は全パターン不発火。さらに価格比の絶対下限でも縛り、
	// 凪でATRが潰れた際の塵足（つぶれ足）を止める。両side対称。
	atr := ATR(bars, 14)
	sigBody := func(b Bar) bool {
		if !(atr > 0 && Body(b) >= patternMinATR*atr) {
			return false
		}
		if ref := closes[len(closes)-1]; ref > 0 {
			return Body(b) >= minBodyRatio*ref
		}
		return false
	}
	// 上位方向に逆らわない：long は trend_up でのみ、short は trend_down でのみ。
	// 板・テープの裏取り：確信的な逆フロー（売り圧優勢での買い等）は見送る。
	// フロー不明時は通す（fail-open。冷間始動を殺さない）。
	flow := e.flowDir(symbol)
	// テープの裏取り：確信的な逆テープでのエントリーを見送る。
	// 不明時は通す（fail-open）。
	tapeOKLong := e.tapeAligned(symbol, 1)
	tapeOKShort := e.tapeAligned(symbol, -1)
	fill := func(typ Side, pattern string, conf float64, reason string) *Signal {
		// 拡大失敗のクールダウン：前回発火が騙しに終わった直後の同方向の
		// 拡大系（ride/breakout/new_high）は見送る。実績除外より先に判定し、
		// 壊れた連打を最も外側で止める。新規エンジンでは記録が無いため
		// 素通りする（既存テストへの影響なし）。
		if expansionPatterns[pattern] {
			dir := 1
			if typ == Sell {
				dir = -1
			}
			if e.expansionBlocked(symbol, dir, closes[len(closes)-1], len(bars)) {
				return mkHold("expansion cooling")
			}
		}
		// 実績除外（deny 優先）：週次 paper 検証で負け越し確定のパターンは
		// 許可集合の内外を問わず不発火にする。
		if typ == Sell && e.shortDeny[pattern] {
			return mkHold("short pattern denied")
		}
		if typ == Buy && e.longDeny[pattern] {
			return mkHold("long pattern denied")
		}
		// レンジ帯ではブレイク系のみ発火させる（揉み合いでの往復出血防止）。
		// 既存テストの短い足列（80本未満）では不発火のため影響しない。
		if e.cfg.SqueezeGate && e.squeezedEntry(symbol) && !squeezeBreakout[pattern] {
			return mkHold("squeeze non-breakout")
		}
		// ショートは許可集合のみ発火する（paper 検証の勝ち組に限定）。
		// 集合が空のときは全許可（検証用の逃がし）。
		// ただし天井型の除外3種は、ヒゲ確認＋板確信があるときに限り迂回する
		// （教科書の初動売りを殺さないため。条件は呼び出し側の override 枝で
		// 担保し、ここでは二重化して安全側に倒す）。
		if typ == Sell && len(e.shortAllow) > 0 && !e.shortAllow[pattern] && !e.confirmedTopBypass(symbol, tsMs, pattern) {
			return mkHold("short pattern disabled")
		}
		// 実証セル許可制：集合が非空のときはセル外の新規を不発火にする。
		// 空集合は無制限（従来動作・冷間始動を殺さない）。deny より後に
		// 判定するが両方適用される。ロング・ショート対称。
		if len(e.allowCells) > 0 {
			side := "buy"
			if typ == Sell {
				side = "sell"
			}
			if !e.allowCells[allowKey(side, pattern, regime)] {
				return mkHold("cell not allowed")
			}
		}
		// 板・テープの裏取り：確信的な逆フローは見送る。
		// ブレイク系は板の売りを食って伸びるのが本質のため、原則免除する。
		// ただし強い逆板（確信的な反対厚）にぶつかる時と、レンジ帯での
		// ブレイクで板の裏付けがない時は見送る（paper bb_breakdown PF0.21級の
		// 壁跳ね返り対策）。順行・中立・不明時は従来通り通す。両side対称。
		if breakoutExempt[pattern] {
			if typ == Buy && e.wallAgree(symbol, -1) {
				return mkHold("adverse wall")
			}
			if typ == Sell && e.wallAgree(symbol, 1) {
				return mkHold("adverse wall")
			}
			if e.cfg.SqueezeGate && e.squeezedEntry(symbol) {
				if typ == Buy && !e.wallAgree(symbol, 1) {
					return mkHold("squeeze needs board")
				}
				if typ == Sell && !e.wallAgree(symbol, -1) {
					return mkHold("squeeze needs board")
				}
			}
		} else {
			if typ == Buy && (flow < 0 || !tapeOKLong) {
				if flow < 0 {
					return mkHold("against orderflow")
				}
				return mkHold("against tapeflow")
			}
			if typ == Sell && (flow > 0 || !tapeOKShort) {
				if flow > 0 {
					return mkHold("against orderflow")
				}
				return mkHold("against tapeflow")
			}
		}
		// spoof停止：なりすまし壁の方向の新規は1時間止める（P3対抗方針）。
		// 検出中は停止を更新し、検出後も1時間維持する。データ不足時は素通り。
		// 発火記録より先に判定し、止めた発火は記録しない。
		if typ == Buy && e.spoofBlocked(symbol, 1, tsMs) {
			return mkHold("spoof halt")
		}
		if typ == Sell && e.spoofBlocked(symbol, -1, tsMs) {
			return mkHold("spoof halt")
		}
		// 拡大系の発火を記録する（次回以降の騙し連打判定用）。
		// ゲートを全て通過した本物の発火のみ記録する。
		if expansionPatterns[pattern] {
			dir := 1
			if typ == Sell {
				dir = -1
			}
			e.recordExpansion(symbol, dir, closes[len(closes)-1], len(bars))
		}
		return &Signal{
			Symbol: symbol, Type: typ, Price: closes[len(closes)-1],
			Reason: reason, Timestamp: tsMs, Confidence: conf + fastBonus,
			Pattern: pattern, Regime: regime,
			RSI: rsi, PercentB: pb, MACDHist: hist,
			BarO: bars[len(bars)-1].O, BarH: bars[len(bars)-1].H,
			BarL: bars[len(bars)-1].L, BarV: bars[len(bars)-1].V,
			MMState: mmStateName(e.mmState(symbol)),
		}
	}
	// 上位方向に逆らわない：long は trend_up でのみ、short は trend_down でのみ。
	// 板確認ブレイクの対称オーバーライド（LINK 9:45-9:50 見逃し対策）。
	// 通常は regime gate で trend_up時はショートを出さず range時は両方出さないが、
	// 下抜け geometry＋MACDデッドクロス＋売り板持続＋テープ非逆行が揃えば、
	// 板が崩落を先行予告しているため regime に逆らってもショートを撃つ。
	// 対称に上抜け＋GC＋買い板でもロングを撃つ。trend_down/trend_up時の通常経路は
	// 後段で従来通り発火するため、ここでは不一致時のみ扱う（回帰防止）。
	// ShortAllow/SqueezeGate/flow veto は fill() が尊重する（bb_breakdown/
	// bb_breakout_up は元々免除系のため、実質 regime のみの緩和になる）。
	// RSI は通常の bb_breakdown/bb_breakout_up と同じく不問とする
	// （下抜け直後は RSI が一時的に極端化するため。live の short-rsi-floor が
	// 別途安全網になる）。反転頻度のみ要求し、保ち合いの誤射を防ぐ。
	if regime != "trend_down" {
		if _, bbDown0 := bbBreakout(closes, e.cfg.BBPeriod, e.cfg.BBK); bbDown0 {
			if prevHist >= 0 && hist < 0 && flow < 0 && tapeOKShort && e.wallAgree(symbol, -1) {
				if cur0 := bars[len(bars)-1]; sigBody(cur0) {
					if flips < 3 {
						return fill(Sell, "bb_breakdown", 0.64, "board-confirmed breakdown against upper trend")
					}
				}
			}
		}
	}
	if regime != "trend_up" {
		if bbUp0, _ := bbBreakout(closes, e.cfg.BBPeriod, e.cfg.BBK); bbUp0 {
			if prevHist <= 0 && hist > 0 && flow > 0 && tapeOKLong && e.wallAgree(symbol, 1) {
				if cur0 := bars[len(bars)-1]; sigBody(cur0) {
					if flips < 3 {
						return fill(Buy, "bb_breakout_up", 0.64, "board-confirmed breakout against upper trend")
					}
				}
			}
		}
	}
	// エクスパンション下抜けの対称オーバーライド（BTC22:55型の急落対策）。
	// bbBreakout は squeeze 後の抜けしか見ないため、収縮なしの急落
	// （拡大中の下抜け）は regime が下を向くまで拾えない。
	// 直近20本安値更新＋拡大中＋MACDデッドクロス＋売り板確信＋テープ非逆行＋
	// 実体＋保ち合い少数の全点灯でのみ regime に逆らって撃つ。
	// 対称に高値更新の上抜けも同様。trend_down/trend_up時の通常経路と重複しない
	// （不一致時のみ）。fill() の許可・deny は尊重する。
	if regime != "trend_down" {
		if brokeLow20(closes, lastC) && expanding && prevHist >= 0 && hist < 0 && flow < 0 && tapeOKShort && e.wallAgree(symbol, -1) {
			if cur0 := bars[len(bars)-1]; sigBody(cur0) {
				if flips < 3 {
					return fill(Sell, "bb_breakdown", 0.64, "expansion breakdown against upper trend")
				}
			}
		}
	}
	if regime != "trend_up" {
		if brokeHigh20(closes, lastC) && expanding && prevHist <= 0 && hist > 0 && flow > 0 && tapeOKLong && e.wallAgree(symbol, 1) {
			if cur0 := bars[len(bars)-1]; sigBody(cur0) {
				if flips < 3 {
					return fill(Buy, "bb_breakout_up", 0.64, "expansion breakout against upper trend")
				}
			}
		}
	}
	// ヒゲ確認の天井反転（BTC22:55初動型）。
	// 上ヒゲ拒否＋RCI高値反転＋陰線＋板確信が揃えば regime に逆らって撃つ。
	// ShortAllowの除外系（宵の明星・包み）には触らず、実績勝ち組の
	// rci_turn_down を使う。対称に底は rci_turn で拾う。
	if regime != "trend_down" {
		if maxRciFast3 >= 80 && rciFast < prevRciFast && rsi > 30 && IsBearish(bars[len(bars)-1]) && sigBody(bars[len(bars)-1]) && e.wickFreshHi(symbol, tsMs) && e.flowAgree(symbol, -1) && flips < 3 {
			return fill(Sell, "rci_turn_down", 0.58, "wick-confirmed top turn against upper trend")
		}
	}
	// 教科書の天井売り（宵の明星・包み・MACDデッドクロス）。
	// ヒゲ確認＋板確信つきに限り許可集合を迂回する（confirmedTopBypass）。
	// 通常の下降regime経路では従来通り許可制に従う。
	if regime != "trend_down" {
		cur0 := bars[len(bars)-1]
		prev0 := bars[len(bars)-2]
		if IsEveningStar(bars) && sigBody(cur0) && rsi > 28 && hist <= prevHist && rciFast <= prevRciFast && flips < 3 && e.wickFreshHi(symbol, tsMs) && e.flowAgree(symbol, -1) {
			return fill(Sell, "evening_star", 0.65, "textbook top star against upper trend")
		}
		if IsBearishEngulfing(prev0, cur0) && sigBody(cur0) && rsi > 28 && hist <= prevHist && rciFast <= prevRciFast && flips < 3 && e.wickFreshHi(symbol, tsMs) && e.flowAgree(symbol, -1) {
			return fill(Sell, "bearish_engulfing", 0.62, "textbook top engulfing against upper trend")
		}
		if prevHist >= 0 && hist < 0 && rsi > 30 && pb > -0.1 && flips < 3 && IsBearish(cur0) && sigBody(cur0) && e.wickFreshHi(symbol, tsMs) && e.flowAgree(symbol, -1) {
			if macdLine > 0 {
				return fill(Sell, "macd_cross_down_high", 0.60, "textbook dead cross against upper trend")
			}
			return fill(Sell, "macd_cross_down", 0.55, "textbook dead cross against upper trend")
		}
	}
	if regime != "trend_up" {
		if minRciFast3 <= -80 && rciFast > prevRciFast && rsi < 70 && IsBullish(bars[len(bars)-1]) && sigBody(bars[len(bars)-1]) && e.wickFreshLo(symbol, tsMs) && e.flowAgree(symbol, 1) && flips < 3 {
			return fill(Buy, "rci_turn", 0.58, "wick-confirmed bottom turn against upper trend")
		}
	}
	// ride継続の対称オーバーライド（23:25〜3:40の一直線対策）。
	// 下げ足並び＋板確信＋天井ヒゲか安値更新のいずれかがあれば、
	// regime が下を向く前から乗り始める。対称に上げも同様。
	if regime != "trend_down" {
		if isBBRideMomentumShort(bars, pb, rsi, hist, prevHist) && sigBody(bars[len(bars)-1]) && e.flowAgree(symbol, -1) && flips < 3 && (e.wickFreshHi(symbol, tsMs) || brokeLow20(closes, lastC) || e.absorbConfirmed(symbol, -1)) && e.distributing(symbol) != -1 {
			return fill(Sell, "bb_ride_momentum_down", 0.58, "confirmed ride down against upper trend")
		}
	}
	if regime != "trend_up" {
		if isBBRideMomentum(bars, pb, rsi, hist, prevHist) && sigBody(bars[len(bars)-1]) && e.flowAgree(symbol, 1) && flips < 3 && (e.wickFreshLo(symbol, tsMs) || brokeHigh20(closes, lastC) || e.absorbConfirmed(symbol, 1)) && e.distributing(symbol) != 1 {
			return fill(Buy, "bb_ride_momentum", 0.58, "confirmed ride up against upper trend")
		}
	}
	// 天井追い（ride/breakout）のfail-closed条件。
	// 10/02-10/04の天井掴み（ride -116.4・breakout -35.6）の反省：
	// 拡大系の追いは板の確信フローがあるときに限り撃つ。
	// MA乖離は見送った：真の拡大初動ではMAがまだ絡んでおり、
	// 乖離要求は正しい初動まで殺すため。ロング・ショート対称。
	chaseLong := e.flowAgree(symbol, 1)
	chaseShort := e.flowAgree(symbol, -1)
	if regime == "trend_up" {
		if ft == "trend_down" {
			return mkHold("lower-tf against")
		}
		if ft == "trend_up" {
			fastBonus = 0.02
		}
		cur := bars[len(bars)-1]
		prev := bars[len(bars)-2]
		bbUp, _ := bbBreakout(closes, e.cfg.BBPeriod, e.cfg.BBK)
		switch {
		case bbUp && sigBody(cur) && expanding && flips < 3 && chaseLong && e.distributing(symbol) != 1:
			return fill(Buy, "bb_breakout_up", 0.64, "bollinger breakout up in uptrend")
		case isBBRideMomentum(bars, pb, rsi, hist, prevHist) && sigBody(cur) && flips < 3 && chaseLong && e.distributing(symbol) != 1:
			return fill(Buy, "bb_ride_momentum", 0.58, "bb ride momentum continuation in uptrend")
		case IsMorningStar(bars) && sigBody(cur) && rsi < 72 && hist >= prevHist && rciFast >= prevRciFast && flips < 3 && e.flowAgree(symbol, 1):
			return fill(Buy, "morning_star", 0.65, "morning star in uptrend")
		case IsBullishEngulfing(prev, cur) && sigBody(cur) && rsi < 72 && hist >= prevHist && rciFast >= prevRciFast && flips < 3 && e.flowAgree(symbol, 1):
			return fill(Buy, "bullish_engulfing", 0.62, "bullish engulfing in uptrend")
		case IsThreeSoldiers(bars) && sigBody(cur) && rsi < 72 && hist >= prevHist && rciFast >= prevRciFast && flips < 3 && e.flowAgree(symbol, 1):
			return fill(Buy, "three_soldiers", 0.60, "three soldiers in uptrend")
		case isHammerAtLow(bars) && sigBody(cur) && rsi < 72 && hist >= prevHist && rciFast >= prevRciFast && flips < 3 && e.flowAgree(symbol, 1):
			return fill(Buy, "hammer_low", 0.58, "hammer at low in uptrend")
		case minRSI5 <= 20 && prevHist <= 0 && hist > 0 && rsi < 70 && pb < 1.1 && flips < 3 && IsBullish(cur) && sigBody(cur) && e.flowAgree(symbol, 1):
			return fill(Buy, "oversold_macd", 0.62, "rsi20 washed then macd crossed up")
		case bullRSIDiv && IsBullish(cur) && sigBody(cur) && flips < 3:
			return fill(Buy, "rsi_div", 0.60, "rsi bullish divergence in uptrend")
		case bullMACDDiv && rsi <= 60 && IsBullish(cur) && sigBody(cur) && flips < 3:
			return fill(Buy, "macd_div", 0.60, "macd bullish divergence in uptrend")
		case pb <= 0.12 && minRSI5 <= 32 && IsBullish(cur) && sigBody(cur) && hist >= prevHist && rciFast >= prevRciFast && hist > 0 && flips < 3 && e.flowAgree(symbol, 1):
			return fill(Buy, "bb_bounce", 0.60, "bb -2s bounce with rsi in uptrend")
		case minRciFast3 <= -80 && rciFast > prevRciFast && rsi < 70 && IsBullish(cur) && sigBody(cur):
			return fill(Buy, "rci_turn", 0.58, "rci turned up from bottom in uptrend")
		case maCrossUp && pb > 0.5 && pb < 1.0 && expanding && hist > 0 && rsi >= 45 && rsi < 70 && sigBody(cur):
			return fill(Buy, "ma_cross", 0.57, "ma crossed bb mid with expansion")
		case rciTrend >= 80 && rciFast < rciSlow && rciFast > prevRciFast && rsi >= 40 && rsi <= 70 && sigBody(cur):
			return fill(Buy, "rci_layer", 0.57, "rci layered push in uptrend")
		case minRciFast5 <= -70 && sarFlipDir == 1 && IsBullish(cur) && sigBody(cur) && rsi < 72:
			return fill(Buy, "rci_sar", 0.60, "rci pinned low with parabolic flip up")
		case isPullbackToMA(bars, maF, maS) && rsi >= 40 && rsi < 68 && hist > prevHist && hist > 0 && sigBody(cur):
			return fill(Buy, "pullback_to_ma", 0.57, "pullback to MA zone in uptrend")
		case isPullbackToBBMid(bars, bbMid) && rsi >= 40 && rsi < 68 && hist > prevHist && sigBody(cur):
			return fill(Buy, "pullback_to_bbmid", 0.56, "pullback to BB mid in uptrend")
		case dblBotOK && cur.C > dblBotNeck && IsBullish(cur) && sigBody(cur) && rsi < 72 && hist >= prevHist && rciFast >= prevRciFast:
			return fill(Buy, "double_bottom", 0.60, "W bottom neckline break in uptrend")
		case invHSOk && cur.C > invHSNeck && IsBullish(cur) && sigBody(cur) && rsi < 72 && hist >= prevHist && rciFast >= prevRciFast:
			return fill(Buy, "inv_head_shoulder", 0.60, "inverse head and shoulder break in uptrend")
		case drop5 >= 0.02 && IsBullish(cur) && Body(cur) >= 0.5*Range(cur) && rsi < 55 && sigBody(cur) && hist >= prevHist && rciFast >= prevRciFast && hist > 0 && flips < 3 && e.flowAgree(symbol, 1):
			return fill(Buy, "v_bottom", 0.58, "V bottom reversal in uptrend")
		case drop5 >= 0.015 && IsBullish(cur) && Body(cur) >= 0.6*Range(cur) && sigBody(cur) && rsi < 68 && hist >= prevHist && rciFast >= prevRciFast && hist > 0 && flips < 3 && nearLow20(closes, lastC) && e.flowAgree(symbol, 1):
			return fill(Buy, "big_white_dip", 0.58, "big white reversal in uptrend")
		case hi20prev > 0 && lastC > hi20prev && expanding && rsi < 75 && IsBullish(cur) && sigBody(cur) && flips < 3:
			return fill(Buy, "new_high", 0.57, "new high with expansion in uptrend")
		case isVolumeSurge(bars) && IsBullish(cur) && sigBody(cur):
			return fill(Buy, "vol_surge", 0.56, "volume surge with buying in uptrend")
		case prevRSI < 30 && rsi >= 30 && IsBullish(cur) && sigBody(cur) && e.flowAgree(symbol, 1):
			return fill(Buy, "rsi_recover", 0.55, "rsi recovered from oversold")
		case prevHist <= 0 && hist > 0:
			if rsi < 70 && pb < 1.1 && flips < 3 && IsBullish(cur) && sigBody(cur) && e.flowAgree(symbol, 1) {
				if macdLine < 0 || nearLongMA {
					return fill(Buy, "macd_cross_deep", 0.60, "macd crossed up from deep/near support")
				}
				return fill(Buy, "macd_cross", 0.55, "macd crossed up")
			}
		}
		return mkHold("no long trigger")
	}
	if regime == "trend_down" {
		if ft == "trend_up" {
			return mkHold("lower-tf against")
		}
		if ft == "trend_down" {
			fastBonus = 0.02
		}
		cur := bars[len(bars)-1]
		prev := bars[len(bars)-2]
		_, bbDown := bbBreakout(closes, e.cfg.BBPeriod, e.cfg.BBK)
		switch {
		case bbDown && sigBody(cur) && expanding && flips < 3 && chaseShort && e.distributing(symbol) != -1:
			return fill(Sell, "bb_breakdown", 0.64, "bollinger breakdown in downtrend")
		case isBBRideMomentumShort(bars, pb, rsi, hist, prevHist) && sigBody(cur) && flips < 3 && chaseShort && e.distributing(symbol) != -1:
			return fill(Sell, "bb_ride_momentum_down", 0.58, "bb ride momentum continuation in downtrend")
		case IsEveningStar(bars) && sigBody(cur) && rsi > 28 && hist <= prevHist && rciFast <= prevRciFast && flips < 3 && e.flowAgree(symbol, -1):
			return fill(Sell, "evening_star", 0.65, "evening star in downtrend")
		case IsBearishEngulfing(prev, cur) && sigBody(cur) && rsi > 28 && hist <= prevHist && rciFast <= prevRciFast && flips < 3 && e.flowAgree(symbol, -1):
			return fill(Sell, "bearish_engulfing", 0.62, "bearish engulfing in downtrend")
		case IsThreeCrows(bars) && sigBody(cur) && rsi > 28 && hist <= prevHist && rciFast <= prevRciFast && flips < 3 && e.flowAgree(symbol, -1):
			return fill(Sell, "three_crows", 0.60, "three crows in downtrend")
		case isShootingStarAtHigh(bars) && sigBody(cur) && rsi > 28 && hist <= prevHist && rciFast <= prevRciFast && flips < 3 && e.flowAgree(symbol, -1):
			return fill(Sell, "shooting_star_high", 0.58, "shooting star at high in downtrend")
		case maxRSI5 >= 80 && prevHist >= 0 && hist < 0 && rsi > 30 && pb > -0.1 && flips < 3 && IsBearish(cur) && sigBody(cur) && e.flowAgree(symbol, -1):
			return fill(Sell, "overheat_macd", 0.62, "rsi80 heated then macd crossed down")
		case bearRSIDiv && IsBearish(cur) && sigBody(cur) && flips < 3:
			return fill(Sell, "rsi_div_down", 0.60, "rsi bearish divergence in downtrend")
		case bearMACDDiv && rsi >= 40 && IsBearish(cur) && sigBody(cur) && flips < 3:
			return fill(Sell, "macd_div_down", 0.60, "macd bearish divergence in downtrend")
		case pb >= 0.88 && maxRSI5 >= 68 && IsBearish(cur) && sigBody(cur) && hist <= prevHist && rciFast <= prevRciFast && hist < 0 && flips < 3 && e.flowAgree(symbol, -1):
			return fill(Sell, "bb_bounce_down", 0.60, "bb +2s rejection with rsi in downtrend")
		case maxRciFast3 >= 80 && rciFast < prevRciFast && rsi > 30 && IsBearish(cur) && sigBody(cur):
			return fill(Sell, "rci_turn_down", 0.58, "rci turned down from top in downtrend")
		case maCrossDown && pb < 0.5 && pb > 0 && expanding && hist < 0 && rsi > 30 && rsi <= 55 && sigBody(cur):
			return fill(Sell, "ma_cross_down", 0.57, "ma crossed down bb mid with expansion")
		case rciTrend <= -80 && rciFast > rciSlow && rciFast < prevRciFast && rsi >= 30 && rsi <= 60 && sigBody(cur):
			return fill(Sell, "rci_layer_down", 0.57, "rci layered drag in downtrend")
		case maxRciFast5 >= 70 && sarFlipDir == -1 && IsBearish(cur) && sigBody(cur) && rsi > 28:
			return fill(Sell, "rci_sar_down", 0.60, "rci pinned high with parabolic flip down")
		case isPullbackToMAShort(bars, maF, maS) && rsi > 32 && rsi <= 60 && hist < prevHist && hist < 0 && sigBody(cur):
			return fill(Sell, "pullback_to_ma_down", 0.57, "rally to MA zone in downtrend")
		case isPullbackToBBMidShort(bars, bbMid) && rsi > 32 && rsi <= 60 && hist < prevHist && sigBody(cur):
			return fill(Sell, "pullback_to_bbmid_down", 0.56, "rally to BB mid in downtrend")
		case triTopOK && cur.C < triTopNeck && IsBearish(cur) && sigBody(cur) && rsi > 28 && hist <= prevHist && rciFast <= prevRciFast:
			return fill(Sell, "triple_top", 0.60, "triple top neckline break in downtrend")
		case dblTopOK && cur.C < dblTopNeck && IsBearish(cur) && sigBody(cur) && rsi > 28 && hist <= prevHist && rciFast <= prevRciFast:
			return fill(Sell, "double_top", 0.60, "M top neckline break in downtrend")
		case rise10 >= 0.025 && IsBearish(cur) && Body(cur) >= 0.6*Range(cur) && sigBody(cur) && rsi > 28 && hist <= prevHist && rciFast <= prevRciFast && hist < 0 && flips < 3 && e.flowAgree(symbol, -1):
			return fill(Sell, "spike_top_black", 0.58, "big black after spike in downtrend")
		case rise10 >= 0.025 && sma10 > 0 && prevC > prevSMA10 && lastC < sma10 && IsBearish(cur) && sigBody(cur) && rsi > 28:
			return fill(Sell, "spike_ma_break", 0.57, "spike top lost short MA in downtrend")
		case hi20prev > 0 && (hi20prev-lastC)/hi20prev < 0.005 && IsBearish(cur) && Body(cur) >= 0.6*Range(cur) && sigBody(cur) && rsi > 28 && hist <= prevHist && rciFast <= prevRciFast && hist < 0 && flips < 3 && e.flowAgree(symbol, -1):
			return fill(Sell, "high_black", 0.58, "big black near high in downtrend")
		case lowerHigh && maS > 0 && lastC < maS && IsBearish(cur) && sigBody(cur) && rsi > 28:
			return fill(Sell, "lower_high_break", 0.57, "lower high broke MA in downtrend")
		case longDCFresh && IsBearish(cur) && sigBody(cur) && rsi > 28:
			return fill(Sell, "long_dc", 0.60, "long MA dead cross in downtrend")
		case longDCActive && dcFresh30 && maS > 0 && bars[len(bars)-1].H >= maS*0.995 && lastC < maS && IsBearish(cur) && sigBody(cur) && rsi > 28 && rsi < 65:
			return fill(Sell, "dc_pullback", 0.58, "first rally after long dead cross")
		case sma200 > 0 && prevSma200 > 0 && sma200 < prevSma200 && lastC < sma200 && bars[len(bars)-1].H >= sma200*0.995 && IsBearish(cur) && sigBody(cur) && rsi > 28:
			return fill(Sell, "dma_resist", 0.57, "rally rejected at declining long MA")
		case isVolumeSurge(bars) && IsBearish(cur) && sigBody(cur):
			return fill(Sell, "vol_surge_down", 0.56, "volume surge with selling in downtrend")
		case prevRSI > 70 && rsi <= 70 && IsBearish(cur) && sigBody(cur) && e.flowAgree(symbol, -1):
			return fill(Sell, "rsi_fade", 0.55, "rsi faded from overbought")
		case prevHist >= 0 && hist < 0:
			if rsi > 30 && pb > -0.1 && flips < 3 && IsBearish(cur) && sigBody(cur) && e.flowAgree(symbol, -1) {
				if macdLine > 0 {
					return fill(Sell, "macd_cross_down_high", 0.60, "macd crossed down from high")
				}
				return fill(Sell, "macd_cross_down", 0.55, "macd crossed down")
			}
		}
		return mkHold("no short trigger")
	}
	return mkHold("range regime holds fire")
}

// isHammerAtLow は安値圏のカラカサかどうかを返す。
// 安値圏＝終値が直近20本の最安付近（下位25%以内）。
func isHammerAtLow(bars []Bar) bool {
	if len(bars) < 20 {
		return false
	}
	cur := bars[len(bars)-1]
	if !IsHammer(cur) {
		return false
	}
	lo, hi := bars[len(bars)-20].L, bars[len(bars)-20].H
	for _, b := range bars[len(bars)-20:] {
		if b.L < lo {
			lo = b.L
		}
		if b.H > hi {
			hi = b.H
		}
	}
	if hi <= lo {
		return false
	}
	return cur.C <= lo+0.25*(hi-lo)
}

// isShootingStarAtHigh は高値圏の流星かどうかを返す。
func isShootingStarAtHigh(bars []Bar) bool {
	if len(bars) < 20 {
		return false
	}
	cur := bars[len(bars)-1]
	if !IsShootingStar(cur) {
		return false
	}
	lo, hi := bars[len(bars)-20].L, bars[len(bars)-20].H
	for _, b := range bars[len(bars)-20:] {
		if b.L < lo {
			lo = b.L
		}
		if b.H > hi {
			hi = b.H
		}
	}
	if hi <= lo {
		return false
	}
	return cur.C >= lo+0.75*(hi-lo)
}

// isBBRideMomentum はBB上 riding 中の連続陽線モメンタムかどうかを返す。
// 従来 bbBreakout の squeeze 必須では拾えない「GC+上抜け+続伸」型を救う。
// 条件: 2連続陽線・上昇・pb>1.0・hist>0かつ減衰なし・rsi<80。
// 出来高・ATR実体・flow/tape は呼び出し側の共通ゲートで担保する。
func isBBRideMomentum(bars []Bar, pb, rsi, hist, prevHist float64) bool {
	if len(bars) < 2 {
		return false
	}
	cur := bars[len(bars)-1]
	prev := bars[len(bars)-2]
	if !IsBullish(prev) || !IsBullish(cur) {
		return false
	}
	if cur.C <= prev.C {
		return false
	}
	if pb <= 1.0 {
		return false
	}
	if rsi >= 80 {
		return false
	}
	if hist <= 0 {
		return false
	}
	if hist < prevHist {
		return false
	}
	return true
}

// --- 天井圏・底値圏の構造パターン（株の達人 売り時/買い時パターン由来）---
// いずれも純粋関数。swings は swingHighs/swingLows の index 列を使う。

// tripleTopNeck は三尊天井のネックラインを返す（中央が最高・両端±1.5%）。
func tripleTopNeck(closes []float64) (float64, bool) {
	highs := swingHighs(closes, 3, 60)
	if len(highs) < 3 {
		return 0, false
	}
	a, b, c := highs[len(highs)-3], highs[len(highs)-2], highs[len(highs)-1]
	if !(closes[b] > closes[a] && closes[b] > closes[c]) {
		return 0, false
	}
	dev := closes[a] - closes[c]
	if dev < 0 {
		dev = -dev
	}
	if closes[c] <= 0 || dev/closes[c] > 0.015 {
		return 0, false
	}
	neck := closes[a]
	for _, v := range closes[a : c+1] {
		if v < neck {
			neck = v
		}
	}
	return neck, neck > 0
}

// doubleTopNeck はM型天井のネックラインを返す（2峰±1%）。
func doubleTopNeck(closes []float64) (float64, bool) {
	highs := swingHighs(closes, 3, 60)
	if len(highs) < 2 {
		return 0, false
	}
	a, b := highs[len(highs)-2], highs[len(highs)-1]
	dev := closes[a] - closes[b]
	if dev < 0 {
		dev = -dev
	}
	if closes[b] <= 0 || dev/closes[b] > 0.01 {
		return 0, false
	}
	neck := closes[a]
	for _, v := range closes[a : b+1] {
		if v < neck {
			neck = v
		}
	}
	return neck, neck > 0
}

// doubleBottomNeck はW底のネックラインを返す（2谷±1%）。
func doubleBottomNeck(closes []float64) (float64, bool) {
	lows := swingLows(closes, 3, 60)
	if len(lows) < 2 {
		return 0, false
	}
	a, b := lows[len(lows)-2], lows[len(lows)-1]
	dev := closes[a] - closes[b]
	if dev < 0 {
		dev = -dev
	}
	if closes[b] <= 0 || dev/closes[b] > 0.01 {
		return 0, false
	}
	neck := closes[a]
	for _, v := range closes[a : b+1] {
		if v > neck {
			neck = v
		}
	}
	return neck, neck > 0
}

// invHeadShoulderNeck は逆三尊のネックラインを返す（中央が最安・両端±1.5%）。
func invHeadShoulderNeck(closes []float64) (float64, bool) {
	lows := swingLows(closes, 3, 60)
	if len(lows) < 3 {
		return 0, false
	}
	a, b, c := lows[len(lows)-3], lows[len(lows)-2], lows[len(lows)-1]
	if !(closes[b] < closes[a] && closes[b] < closes[c]) {
		return 0, false
	}
	dev := closes[a] - closes[c]
	if dev < 0 {
		dev = -dev
	}
	if closes[c] <= 0 || dev/closes[c] > 0.015 {
		return 0, false
	}
	neck := closes[a]
	for _, v := range closes[a : c+1] {
		if v > neck {
			neck = v
		}
	}
	return neck, neck > 0
}

// risePct は直近 n 本の上昇率を返す（不足時は 0）。
func risePct(closes []float64, n int) float64 {
	if n <= 0 || len(closes) < n+1 {
		return 0
	}
	base := closes[len(closes)-n-1]
	if base <= 0 {
		return 0
	}
	return (closes[len(closes)-1] - base) / base
}

// dropPct は直近 n 本の下落率を返す（正値・不足時は 0）。
func dropPct(closes []float64, n int) float64 {
	return -risePct(closes, n)
}

// maxClose/minClose は直近 n 本の高値・安値（不足時は 0）。
func maxClose(closes []float64, n int) float64 {
	if n <= 0 || len(closes) < n {
		return 0
	}
	mx := closes[len(closes)-n]
	for _, v := range closes[len(closes)-n+1:] {
		if v > mx {
			mx = v
		}
	}
	return mx
}

func minClose(closes []float64, n int) float64 {
	if n <= 0 || len(closes) < n {
		return 0
	}
	mn := closes[len(closes)-n]
	for _, v := range closes[len(closes)-n+1:] {
		if v < mn {
			mn = v
		}
	}
	return mn
}

// maCrossDownFresh は短期MAの長期MA下抜けの新鮮さを返す（N本以内に発生）。
func maCrossDownFresh(closes []float64, fast, slow, within int) bool {
	n := len(closes)
	if n < slow+2 || within <= 0 {
		return false
	}
	if SMA(closes, fast) == 0 || SMA(closes, slow) == 0 {
		return false
	}
	if SMA(closes, fast) >= SMA(closes, slow) {
		return false
	}
	for k := 1; k <= within && n-k >= slow; k++ {
		pf, ps := SMA(closes[:n-k], fast), SMA(closes[:n-k], slow)
		if pf > 0 && ps > 0 && pf >= ps {
			return true
		}
	}
	return false
}

// maCrossUpFresh は上抜けの対称形。
func maCrossUpFresh(closes []float64, fast, slow, within int) bool {
	n := len(closes)
	if n < slow+2 || within <= 0 {
		return false
	}
	if SMA(closes, fast) == 0 || SMA(closes, slow) == 0 {
		return false
	}
	if SMA(closes, fast) <= SMA(closes, slow) {
		return false
	}
	for k := 1; k <= within && n-k >= slow; k++ {
		pf, ps := SMA(closes[:n-k], fast), SMA(closes[:n-k], slow)
		if pf > 0 && ps > 0 && pf <= ps {
			return true
		}
	}
	return false
}

// isBBRideMomentumShort は下 riding 対称形（2連続陰線・下降・pb<0）。
func isBBRideMomentumShort(bars []Bar, pb, rsi, hist, prevHist float64) bool {
	if len(bars) < 2 {
		return false
	}
	cur := bars[len(bars)-1]
	prev := bars[len(bars)-2]
	if !IsBearish(prev) || !IsBearish(cur) {
		return false
	}
	if cur.C >= prev.C {
		return false
	}
	if pb >= 0 {
		return false
	}
	if rsi <= 20 {
		return false
	}
	if hist >= 0 {
		return false
	}
	if hist > prevHist {
		return false
	}
	return true
}

// isPullbackToMA はMA帯への押し目かどうかを返す。
// 直近2本のいずれかがMA帯（MAfast〜MAslow）に触れ、現足が帯上で陽引けしたら復帰。
// ブレイク継続ではなく「押して拾う」型。過熱（RSI/MACD）は呼び出し側で縛る。
func isPullbackToMA(bars []Bar, maF, maS float64) bool {
	if len(bars) < 2 || maF <= 0 || maS <= 0 {
		return false
	}
	cur := bars[len(bars)-1]
	prev := bars[len(bars)-2]
	zoneTop := maF
	if maS > zoneTop {
		zoneTop = maS
	}
	zoneBot := maF
	if maS < zoneBot {
		zoneBot = maS
	}
	low2 := prev.L
	if cur.L < low2 {
		low2 = cur.L
	}
	if low2 > zoneTop {
		return false
	}
	if cur.C < zoneBot {
		return false
	}
	return IsBullish(cur)
}

// isPullbackToMAShort は上への戻り売り対称形（MA帯まで戻して陰引け）。
func isPullbackToMAShort(bars []Bar, maF, maS float64) bool {
	if len(bars) < 2 || maF <= 0 || maS <= 0 {
		return false
	}
	cur := bars[len(bars)-1]
	prev := bars[len(bars)-2]
	zoneTop := maF
	if maS > zoneTop {
		zoneTop = maS
	}
	zoneBot := maF
	if maS < zoneBot {
		zoneBot = maS
	}
	high2 := prev.H
	if cur.H > high2 {
		high2 = cur.H
	}
	if high2 < zoneBot {
		return false
	}
	if cur.C > zoneTop {
		return false
	}
	return IsBearish(cur)
}

// isPullbackToBBMid はBBミッドへの押し目かどうかを返す（ミッド割れ→奪回）。
func isPullbackToBBMid(bars []Bar, mid float64) bool {
	if len(bars) < 2 || mid <= 0 {
		return false
	}
	cur := bars[len(bars)-1]
	prev := bars[len(bars)-2]
	low2 := prev.L
	if cur.L < low2 {
		low2 = cur.L
	}
	if low2 > mid {
		return false
	}
	if cur.C < mid {
		return false
	}
	return IsBullish(cur)
}

// isPullbackToBBMidShort はBBミッドへの戻り対称形（上抜け→失速）。
func isPullbackToBBMidShort(bars []Bar, mid float64) bool {
	if len(bars) < 2 || mid <= 0 {
		return false
	}
	cur := bars[len(bars)-1]
	prev := bars[len(bars)-2]
	high2 := prev.H
	if cur.H > high2 {
		high2 = cur.H
	}
	if high2 < mid {
		return false
	}
	if cur.C > mid {
		return false
	}
	return IsBearish(cur)
}

// EvalExit は建玉の出口判定を行う。決済すべきなら理由を返す。
// 反対シグナル・TP・SL・時間切れ・トレイルに加え、板の反対並びが続けば
// 損切り前に離脱する（imbalance_flip）。
// EvalExit は出口判定の公開窓口。ExitDeny に載ったソフト出口（rsi_weak 等）
// を握り潰し、stop_loss/take_profit/max_hold/確定反転系の安全出口は常に通す。
func (e *Engine) EvalExit(symbol string, pos OpenPos, mid float64, tsMs int64) (bool, string) {
	ok, reason := e.evalExitRaw(symbol, pos, mid, tsMs)
	if ok && e.exitDenied(reason) {
		return false, ""
	}
	return ok, reason
}

// exitDenied は理由を禁止集合で判定する（hardExit は常に許可）。
func (e *Engine) exitDenied(reason string) bool {
	if len(e.cfg.ExitDeny) == 0 || hardExit(reason) {
		return false
	}
	for _, r := range e.cfg.ExitDeny {
		if strings.EqualFold(strings.TrimSpace(r), reason) {
			return true
		}
	}
	return false
}

// hardExit は安全・確定のため禁止できない出口かを返す。
func hardExit(reason string) bool {
	switch reason {
	case "stop_loss", "take_profit", "max_hold", "trailing_stop",
		"strong_reversal", "trend_flip", "wall_runover":
		return true
	}
	return false
}

func (e *Engine) evalExitRaw(symbol string, pos OpenPos, mid float64, tsMs int64) (bool, string) {
	if mid <= 0 || pos.EntryPrice <= 0 {
		return false, ""
	}
	var pnlR float64
	if pos.Side == Buy {
		pnlR = (mid - pos.EntryPrice) / pos.EntryPrice
	} else {
		pnlR = (pos.EntryPrice - mid) / pos.EntryPrice
	}
	// ATR は足があれば使える（不足時は 0＝固定値動作に退化）。
	var atrNow float64
	if bars := e.builderFor(symbol).Bars(); len(bars) >= 14 {
		atrNow = ATR(bars, 14)
	}
	// SL：固定比率と ATR×倍率の大きい方を閾値にする。
	slEff := e.cfg.SLRatio
	if e.cfg.SLATR > 0 && atrNow > 0 && pos.EntryPrice > 0 {
		if v := e.cfg.SLATR * atrNow / pos.EntryPrice; v > slEff {
			slEff = v
		}
	}
	if slEff > 0 && pnlR <= -slEff {
		return true, "stop_loss"
	}
	if e.cfg.TPRatio > 0 && pnlR >= e.cfg.TPRatio {
		return true, "take_profit"
	}
	if e.cfg.MaxHoldSec > 0 && float64(tsMs-pos.EntryTime)/1000.0 >= e.cfg.MaxHoldSec {
		return true, "max_hold"
	}
	// 世代管理＋高値/安値/%Bピーク追跡（出口フラグ off でも追跡だけ行う）。
	st := e.trail[symbol]
	if st == nil || st.entryTime != pos.EntryTime {
		st = &trailState{entryTime: pos.EntryTime, high: mid, low: mid, maxPb: -1, minPb: 2}
		e.trail[symbol] = st
	}
	if mid > st.high {
		st.high = mid
	}
	if mid < st.low {
		st.low = mid
	}
	// 指標系の出口（株の達人式）。足が温まっていない間は fail-open で飛ばす。
	exitNeed := e.cfg.MACDSlow + e.cfg.MACDSig
	if v := e.cfg.BBPeriod; v > exitNeed {
		exitNeed = v
	}
	if v := e.cfg.RSIPeriod + 1; v > exitNeed {
		exitNeed = v
	}
	haveBars := false
	var pbNow, rsiNow, histNow, prevHistNow, rciNow, prevRciNow, maFastNow float64
	var lastBarStart int64
	if bars := e.builderFor(symbol).Bars(); len(bars) >= exitNeed {
		haveBars = true
		lastBarStart = bars[len(bars)-1].Start
		xcloses := Closes(bars)
		_, xup, xlow := Bollinger(xcloses, e.cfg.BBPeriod, e.cfg.BBK)
		pbNow = PercentB(xcloses[len(xcloses)-1], xup, xlow)
		if pbNow > st.maxPb {
			st.maxPb = pbNow
		}
		if pbNow < st.minPb {
			st.minPb = pbNow
		}
		rsiNow = RSI(xcloses, e.cfg.RSIPeriod)
		_, _, histNow = MACD(xcloses, e.cfg.MACDFast, e.cfg.MACDSlow, e.cfg.MACDSig)
		if len(xcloses) >= e.cfg.MACDSlow+e.cfg.MACDSig+1 {
			_, _, prevHistNow = MACD(xcloses[:len(xcloses)-1], e.cfg.MACDFast, e.cfg.MACDSlow, e.cfg.MACDSig)
		}
		rciNow = RCI(xcloses, rciFastPeriod)
		if len(xcloses) > rciFastPeriod {
			prevRciNow = RCI(xcloses[:len(xcloses)-1], rciFastPeriod)
		}
		maFastNow = SMA(xcloses, e.cfg.MAFast)
	}
	// トレイル：発動益率を超えたら高値（売りは安値）を追い、
	// そこからの押し戻しで利確する。コマ保ち合いでの利食い逃しを防ぐ。
	// ATR 倍率が有効なときは固定比率と ATR 側の大きい方を使う。
	actEff, backEff := e.cfg.TrailActivateR, e.cfg.TrailBackR
	if e.cfg.TrailActivateATR > 0 && atrNow > 0 && pos.EntryPrice > 0 {
		if v := e.cfg.TrailActivateATR * atrNow / pos.EntryPrice; v > actEff {
			actEff = v
		}
	}
	if e.cfg.TrailBackATR > 0 && atrNow > 0 {
		if st.high > 0 {
			if v := e.cfg.TrailBackATR * atrNow / st.high; v > backEff {
				backEff = v
			}
		}
		if st.low > 0 {
			if v := e.cfg.TrailBackATR * atrNow / st.low; v > backEff {
				backEff = v
			}
		}
	}
	if actEff > 0 && backEff > 0 {
		if pos.Side == Buy {
			if gainR := (st.high - pos.EntryPrice) / pos.EntryPrice; gainR >= actEff {
				if dropR := (st.high - mid) / st.high; st.high > 0 && dropR >= backEff {
					return true, "trailing_stop"
				}
			}
		} else if pos.Side == Sell {
			if gainR := (pos.EntryPrice - st.low) / pos.EntryPrice; gainR >= actEff {
				if backR := (mid - st.low) / st.low; st.low > 0 && backR >= backEff {
					return true, "trailing_stop"
				}
			}
		}
	}
	// 株の達人式の利確・手仕舞い（足が温まっている間のみ）。
	if haveBars {
		lastClose := 0.0
		if bars := e.builderFor(symbol).Bars(); len(bars) > 0 {
			lastClose = bars[len(bars)-1].C
		}
		if pos.Side == Buy {
			// 逆張り entry の利確目安：+2σ到達・RSI80%（含み益がある場合）。
			// 順張りはバンドウォークを伸ばすため +1σ割れまで待つ。
			contrarian := pos.EntryPattern == "bb_bounce" || pos.EntryPattern == "rsi_recover" ||
				pos.EntryPattern == "oversold_macd" || pos.EntryPattern == "rsi_div" || pos.EntryPattern == "macd_div"
			if contrarian && pnlR > 0 && (pbNow >= 1.0 || rsiNow >= 80) {
				return true, "rsi_overheat"
			}
			// バンドウォーク後は+1σ割れで売り（歩き切った後の失速）。
			if st.maxPb >= 1.0 && pbNow < 0.75 && pnlR > 0 {
				return true, "plus1s_break"
			}
			// 勝ち玉の強包み単発離脱（天井打ち初動対策・売り玉と対称）。
			// 微益・含み損玉の2本確定ルート（後段）とは別に、意味のある含み益
			// （>0.5%）があり、実体の強い陰包み＋売り板反転＋MACD反転が揃えば
			// 単発で離脱する。弱い十字・小実体・板順行・MACD順行では保持する。
			// 確証投票も要求する（板・MACD・反対足で立ち、2票を満たす形）。
			// 汎用の macd_dead_cross より具体的なため先に判定する
			// （入口 switch の macd_cross 後置と同思想）。
			// ただし上位が順行の間は譲歩し、トレイル・ma_break に伸ばさせる
			// （勝ちを伸ばす方向。不明時は速く降りる安全側）。
			if pnlR > 0.005 && e.exitConfirmed(symbol, pos.Side) && !e.trendWith(symbol, Buy) {
				if b := e.builderFor(symbol).Bars(); len(b) >= 2 {
					prevB, curB := b[len(b)-2], b[len(b)-1]
					if IsBearishEngulfing(prevB, curB) && atrNow > 0 && Body(curB) >= patternMinATR*atrNow && Body(curB) >= 0.6*Range(curB) {
						if e.flowDir(symbol) < 0 && histNow < prevHistNow {
							return true, "strong_reversal"
						}
					}
				}
			}
			// MACDデットクロスで利確（含み益がある場合）。
			if prevHistNow >= 0 && histNow < 0 && pnlR > 0 {
				return true, "macd_dead_cross"
			}
			// 拡大期の短期MA割れで利確（含み益がある場合）。
			// 単一要因のため確証投票を要求する。
			if maFastNow > 0 && lastClose > 0 && lastClose < maFastNow && pnlR > 0.005 && e.exitConfirmed(symbol, pos.Side) {
				return true, "ma_break"
			}
			// RCI高値圏からの反転で利確（含み益がある場合）。
			// 微反転では降ろさない：8pt以上の切り下げ＋MACD減速の裏付けを要求する。
			// RCI単独の小反転は揉み合いのノイズ（12:50 BTC の早売りの反省）。
			if prevRciNow >= 80 && prevRciNow-rciNow >= 8 && histNow < prevHistNow && pnlR > 0 {
				return true, "rci_fade"
			}
			// RSIが50%を維持できない場合は下降転換の疑いで手仕舞い。
			// 単一要因のため確証投票を要求する（弱い出口の誤射防止）。
			if pos.EntryRSI >= 50 && rsiNow < 47 && e.exitConfirmed(symbol, pos.Side) {
				return true, "rsi_weak"
			}
		} else if pos.Side == Sell {
			contrarian := pos.EntryPattern == "bb_bounce_down" || pos.EntryPattern == "rsi_fade" ||
				pos.EntryPattern == "overheat_macd"
			if contrarian && pnlR > 0 && (pbNow <= 0 || rsiNow <= 20) {
				return true, "rsi_overheat"
			}
			// 下へのバンドウォーク後は-1σ（pb 0.25）超えで売り（対称形）。
			if st.minPb <= 0 && pbNow > 0.25 && pnlR > 0 {
				return true, "plus1s_break"
			}
			// 勝ち玉の強包み単発離脱（XRP 10:20型の底打ち初動対策・買い玉と対称）。
			// 意味のある含み益（>0.5%）があり、実体の強い陽包み＋買い板反転＋
			// MACD反転が揃えば単発で離脱する。弱い足・板売り残・MACD逆行では保持。
			// 確証投票も要求する（板・MACD・反対足で立ち、2票を満たす形）。
			// 汎用の macd_dead_cross より具体的なため先に判定する。
			// 上位順行の間は譲歩し、勝ちを伸ばす（不明時は速く降りる）。
			if pnlR > 0.005 && e.exitConfirmed(symbol, pos.Side) && !e.trendWith(symbol, Sell) {
				if b := e.builderFor(symbol).Bars(); len(b) >= 2 {
					prevB, curB := b[len(b)-2], b[len(b)-1]
					if IsBullishEngulfing(prevB, curB) && atrNow > 0 && Body(curB) >= patternMinATR*atrNow && Body(curB) >= 0.6*Range(curB) {
						if e.flowDir(symbol) > 0 && histNow > prevHistNow {
							return true, "strong_reversal"
						}
					}
				}
			}
			if prevHistNow <= 0 && histNow > 0 && pnlR > 0 {
				return true, "macd_dead_cross"
			}
			if maFastNow > 0 && lastClose > 0 && lastClose > maFastNow && pnlR > 0.005 && e.exitConfirmed(symbol, pos.Side) {
				return true, "ma_break"
			}
			if prevRciNow <= -80 && rciNow-prevRciNow >= 8 && histNow > prevHistNow && pnlR > 0 {
				return true, "rci_fade"
			}
			if pos.EntryRSI <= 50 && rsiNow > 53 && e.exitConfirmed(symbol, pos.Side) {
				return true, "rsi_weak"
			}
		}
	}
	// 反転離脱：微益・含み損の玉に反対方向の反転が連続2足で確定し、かつ
	// 確証票が足りたら離脱する。入口ゲート（レジーム・許可集合・フロー・
	// 活発さ・下位足）に縛られない未加工の判定を使う。従来は EvalEntry の
	// フィルタ済みシグナルだけを見ていたため、許可外パターン
	// （evening_star等）やレンジ帯では一生売られず放置された
	// （FCR/BCH/SOL放置の主因）。
	// 一方、単発足での即離脱は往復ビンタの往路になる（2026-09-26 LINK が
	// 5分・5秒で売買往復した実績）。そのため反転の連続確定を要求する：
	// 初発で印を付け、途切れたら解除し、別足で再発したら離脱する。
	// さらに弱い出口と同様に確証投票を要求し、板・指標の裏付けのない
	// 反転では降ろさない（総括判定）。SL・時間切れ・指標系は即時
	// （変更なし）。トレンド反転も即時。
	// 意味のある含み益（>0.5%。ma_break と同基準）がある玉は既存の
	// 勝ち玉管理（トレイル・指標系）に任せる。既存の指標系離脱を優先するため後置する。
	if haveBars && pnlR <= 0.005 {
		if _, ok := e.reversalExitPattern(symbol, pos.Side); ok {
			m := e.revExit[symbol]
			if m == nil || m.entryTime != pos.EntryTime {
				m = &revMark{entryTime: pos.EntryTime}
				e.revExit[symbol] = m
			}
			if m.armed && m.barStart != lastBarStart && e.exitConfirmed(symbol, pos.Side) {
				return true, "signal"
			}
			m.armed = true
			m.barStart = lastBarStart
		} else if m := e.revExit[symbol]; m != nil && m.entryTime == pos.EntryTime {
			// 反転が途切れたら解除する（連続性が必要）。
			m.armed = false
		}
		// トレンド反転の安全網：明確な逆トレンドが出たら手仕舞う。
		// パターン不発でも緩やかなドリフト反転を拾う。足不足時は不明
		//扱いで何もしない（fail-open。冷間始動を殺さない）。
		if reg := trendOf(Closes(e.trendFor(symbol).Bars()), e.cfg.MAFast, e.cfg.MASlow); reg != "" {
			if pos.Side == Buy && reg == "trend_down" {
				return true, "trend_flip"
			}
			if pos.Side == Sell && reg == "trend_up" {
				return true, "trend_flip"
			}
		}
	}
	// 売り圧（買い玉なら売り並び）の連続で離脱する。
	// ただしバンドウォーク中（+2σ riding）は微細ノイズで降ろさない。
	// さらに含み益の玉は確定シグナル（反対信号・DC・+1σ割れ等）まで保持し、
	// 板ノイズでの離脱は含み損の玉の損切り前倒しに限定する（無駄な回転の削減）。
	// 圧力の確信も要求する：符号の連続だけでなく平均傾きが閾値以上。
	// 弱い偏り（-0.1〜-0.3の行ったり来たり）での離脱が往復の主因だった
	// 実績（2026-09-29 LINK）に対応する。
	riding := haveBars && ((pos.Side == Buy && pbNow > 0.9) || (pos.Side == Sell && pbNow < 0.1))
	cutLoss := pnlR < 0
	// 板系離脱は最低保有秒数を満たした玉だけ（足未確定の即切り禁止）。
	holdAgeSec := float64(tsMs-pos.EntryTime) / 1000.0
	imbReady := e.cfg.ImbMinHoldSec <= 0 || holdAgeSec >= e.cfg.ImbMinHoldSec
	// 板系も単一要因のため確証投票を要求する（板自体の票は自動的に入る）。
	if !riding && cutLoss && imbReady && e.exitConfirmed(symbol, pos.Side) {
		if hist := e.imbs[symbol]; len(hist) >= ImbFlipN {
			against := 0
			mean := 0.0
			win := hist[len(hist)-ImbFlipN:]
			for _, v := range win {
				mean += v
				if pos.Side == Buy && v < 0 {
					against++
				} else if pos.Side == Sell && v > 0 {
					against++
				}
			}
			mean /= float64(len(win))
			firm := (pos.Side == Buy && mean <= -imbFlipMinMean) ||
				(pos.Side == Sell && mean >= imbFlipMinMean)
			if against >= ImbFlipN && firm {
				return true, "imbalance_flip"
			}
		}
		// 吸収を検出したら損切り前に離脱する（隠れ反対圧力）。
		if e.absorbed(symbol) {
			return true, "absorption"
		}
		// 壁の逆行加速では損切り前に離脱する（轢かれる前の退出）。
		// 含み損玉専用・確証投票つき。勝ち玉・新規には使わない。両side対称。
		if e.wallRunover(symbol, pos.Side) {
			return true, "wall_runover"
		}
	}
	return false, ""
}

// exitConfirmed は弱い出口の確証が足りているかを返す。
// ExitConfirmVotes<=0 のときは常に true（従来動作を完全維持）。
func (e *Engine) exitConfirmed(symbol string, side Side) bool {
	if e.cfg.ExitConfirmVotes <= 0 {
		return true
	}
	return e.exitVotes(symbol, side) >= e.cfg.ExitConfirmVotes
}

// exitVotes は出口確証の票数を数える（最大4票・単純多数決）。
// 内訳はいずれも建玉への逆行：上位トレンド反転・MACD減速・強い反対足・
// 板逆行。不明・不足時はその票を落とす（fail-closed。弱い出口だけを
// 止め、強い出口には一切影響しない）。
func (e *Engine) exitVotes(symbol string, side Side) int {
	votes := 0
	bars := e.builderFor(symbol).Bars()
	var closes []float64
	if len(bars) > 0 {
		closes = Closes(bars)
	}
	// 1票：上位トレンドの逆行。
	if len(closes) > 0 {
		reg := trendOf(Closes(e.trendFor(symbol).Bars()), e.cfg.MAFast, e.cfg.MASlow)
		if (side == Buy && reg == "trend_down") || (side == Sell && reg == "trend_up") {
			votes++
		}
	}
	// 1票：MACDヒストグラムの減速（建玉への逆行）。
	if len(closes) >= e.cfg.MACDSlow+e.cfg.MACDSig+1 {
		_, _, hist := MACD(closes, e.cfg.MACDFast, e.cfg.MACDSlow, e.cfg.MACDSig)
		_, _, prevHist := MACD(closes[:len(closes)-1], e.cfg.MACDFast, e.cfg.MACDSlow, e.cfg.MACDSig)
		if (side == Buy && hist < prevHist) || (side == Sell && hist > prevHist) {
			votes++
		}
	}
	// 1票：強い反対足（実体が ATR の4割以上の確定足）。
	if len(bars) > 0 {
		atr := ATR(bars, 14)
		cur := bars[len(bars)-1]
		if atr > 0 && Body(cur) >= patternMinATR*atr {
			if (side == Buy && IsBearish(cur)) || (side == Sell && IsBullish(cur)) {
				votes++
			}
		}
	}
	// 1票：板の逆行。
	if side == Buy && e.flowDir(symbol) < 0 {
		votes++
	}
	if side == Sell && e.flowDir(symbol) > 0 {
		votes++
	}
	return votes
}

// reversalExitPattern は建玉と反対方向の反転パターンが出ているかを返す。
// 入口ゲート（レジーム・ShortAllow許可集合・板/テープ裏取り・活発さ・
// 下位足 veto）を一切掛けない未加工の判定。EvalExit からのみ使う。
//
// 形状の幾何条件・順序・RSI域・hist/RCI方向は EvalEntry の switch と
// 鏡写しに保つこと。ただし確信投票系（hist符号・flips・板厚wall・
// 絶対実体・高値0.5%/安値近接）は意図的に掛けない：入口は厳しく・
// 出口は敏感にし、含み損玉の離脱を遅らせないため。2本確定＋確証投票が
// 出口側の安全網になる。
// TestReversalMirrorsEntry が両者の乖離（許可外パターンで entry は
// Hold だが exit は検出すること）を検証する。
func (e *Engine) reversalExitPattern(symbol string, side Side) (string, bool) {
	bars := e.builderFor(symbol).Bars()
	need := e.cfg.MASlow + e.cfg.MACDSig + 2
	if len(bars) < need || len(bars) < 2 {
		return "", false
	}
	closes := Closes(bars)
	_, upper, lower := Bollinger(closes, e.cfg.BBPeriod, e.cfg.BBK)
	rsi := RSI(closes, e.cfg.RSIPeriod)
	_, _, hist := MACD(closes, e.cfg.MACDFast, e.cfg.MACDSlow, e.cfg.MACDSig)
	prevHist := 0.0
	if len(closes) >= e.cfg.MACDSlow+e.cfg.MACDSig+1 {
		_, _, prevHist = MACD(closes[:len(closes)-1], e.cfg.MACDFast, e.cfg.MACDSlow, e.cfg.MACDSig)
	}
	prevRSI := 50.0
	if len(closes) >= e.cfg.RSIPeriod+2 {
		prevRSI = RSI(closes[:len(closes)-1], e.cfg.RSIPeriod)
	}
	pb := PercentB(closes[len(closes)-1], upper, lower)
	maF := SMA(closes, e.cfg.MAFast)
	maS := SMA(closes, e.cfg.MASlow)
	bbMid := SMA(closes, e.cfg.BBPeriod)
	macdLine, _, _ := MACD(closes, e.cfg.MACDFast, e.cfg.MACDSlow, e.cfg.MACDSig)
	longMA := SMA(closes, 75)
	nearLongMA := longMA > 0
	if nearLongMA {
		dev := closes[len(closes)-1] - longMA
		if dev < 0 {
			dev = -dev
		}
		nearLongMA = dev/longMA < 0.01
	}
	minRSI5 := rsi
	maxRSI5 := rsi
	for k := 1; k < 5 && len(closes)-k >= e.cfg.RSIPeriod+1; k++ {
		v := RSI(closes[:len(closes)-k], e.cfg.RSIPeriod)
		if v < minRSI5 {
			minRSI5 = v
		}
		if v > maxRSI5 {
			maxRSI5 = v
		}
	}
	rciFast, prevRciFast := RCI(closes, rciFastPeriod), 0.0
	if len(closes) > rciFastPeriod {
		prevRciFast = RCI(closes[:len(closes)-1], rciFastPeriod)
	}
	minRciFast3 := rciFast
	maxRciFast3 := rciFast
	minRciFast5 := rciFast
	maxRciFast5 := rciFast
	for k := 1; k < 3 && len(closes)-k >= rciFastPeriod; k++ {
		v := RCI(closes[:len(closes)-k], rciFastPeriod)
		if v < minRciFast3 {
			minRciFast3 = v
		}
		if v > maxRciFast3 {
			maxRciFast3 = v
		}
	}
	for k := 1; k < 5 && len(closes)-k >= rciFastPeriod; k++ {
		v := RCI(closes[:len(closes)-k], rciFastPeriod)
		if v < minRciFast5 {
			minRciFast5 = v
		}
		if v > maxRciFast5 {
			maxRciFast5 = v
		}
	}
	sarFlipDir := sarFlip(bars, ParabolicSAR(bars, 0.02, 0.2))
	rciSlow := RCI(closes, rciSlowPeriod)
	rciTrend := RCI(closes, rciTrendPeriod)
	maCrossUp, maCrossDown := false, false
	if len(closes) >= e.cfg.MASlow+1 {
		pf, ps := SMA(closes[:len(closes)-1], e.cfg.MAFast), SMA(closes[:len(closes)-1], e.cfg.MASlow)
		if pf > 0 && ps > 0 {
			maCrossUp = pf <= ps && maF > maS
			maCrossDown = pf >= ps && maF < maS
		}
	}
	triTopNeck, triTopOK := tripleTopNeck(closes)
	dblTopNeck, dblTopOK := doubleTopNeck(closes)
	dblBotNeck, dblBotOK := doubleBottomNeck(closes)
	invHSNeck, invHSOk := invHeadShoulderNeck(closes)
	lastC := closes[len(closes)-1]
	prevC := closes[len(closes)-2]
	hi20prev := 0.0
	if len(closes) > 20 {
		hi20prev = maxClose(closes[:len(closes)-1], 20)
	}
	sma10 := SMA(closes, 10)
	prevSMA10 := 0.0
	if len(closes) > 10 {
		prevSMA10 = SMA(closes[:len(closes)-1], 10)
	}
	sma75 := SMA(closes, 75)
	sma200 := SMA(closes, 200)
	prevSma200 := 0.0
	if len(closes) > 200 {
		prevSma200 = SMA(closes[:len(closes)-1], 200)
	}
	rise10 := risePct(closes, 10)
	drop5 := dropPct(closes, 5)
	longDCFresh := maCrossDownFresh(closes, 75, 200, 5)
	longDCActive := sma75 > 0 && sma200 > 0 && sma75 < sma200
	dcFresh30 := maCrossDownFresh(closes, 75, 200, 30)
	lowerHigh := false
	if sh := swingHighs(closes, 3, 60); len(sh) >= 2 {
		a, b := sh[len(sh)-2], sh[len(sh)-1]
		lowerHigh = closes[b] < closes[a]
	}
	flips := HistFlips(closes, e.cfg.MACDFast, e.cfg.MACDSlow, e.cfg.MACDSig, 5)
	bwNow, bwPrev := Bandwidth(closes, e.cfg.BBPeriod, e.cfg.BBK), 0.0
	if len(closes) > e.cfg.BBPeriod {
		bwPrev = Bandwidth(closes[:len(closes)-1], e.cfg.BBPeriod, e.cfg.BBK)
	}
	expanding := bwNow > 0 && bwNow >= bwPrev
	rsiSer := RSISeries(closes, e.cfg.RSIPeriod)
	hSer := MACDHistSeries(closes, e.cfg.MACDFast, e.cfg.MACDSlow, e.cfg.MACDSig)
	bullRSIDiv := rsi <= 55 && BullishDivergence(closes, rsiSer, divOrder, divLookback)
	bearRSIDiv := rsi >= 45 && BearishDivergence(closes, rsiSer, divOrder, divLookback)
	bullMACDDiv := BullishDivergence(closes, hSer, divOrder, divLookback)
	bearMACDDiv := BearishDivergence(closes, hSer, divOrder, divLookback)
	atr := ATR(bars, 14)
	sigBody := func(b Bar) bool {
		return atr > 0 && Body(b) >= patternMinATR*atr
	}
	cur := bars[len(bars)-1]
	prev := bars[len(bars)-2]
	// 買い玉の出口：売りパターン群（EvalEntry trend_down 枝と同条件・同順序）。
	if side == Buy {
		_, bbDown := bbBreakout(closes, e.cfg.BBPeriod, e.cfg.BBK)
		switch {
		case bbDown:
			return "bb_breakdown", true
		case isBBRideMomentumShort(bars, pb, rsi, hist, prevHist) && sigBody(cur):
			return "bb_ride_momentum_down", true
		case IsEveningStar(bars) && sigBody(cur) && rsi > 28:
			return "evening_star", true
		case IsBearishEngulfing(prev, cur) && sigBody(cur) && rsi > 28:
			return "bearish_engulfing", true
		case IsThreeCrows(bars) && sigBody(cur) && rsi > 28:
			return "three_crows", true
		case isShootingStarAtHigh(bars) && sigBody(cur) && rsi > 28:
			return "shooting_star_high", true
		case maxRSI5 >= 80 && prevHist >= 0 && hist < 0 && rsi > 30 && pb > -0.1 && flips < 3 && IsBearish(cur) && sigBody(cur):
			return "overheat_macd", true
		case bearRSIDiv && IsBearish(cur) && sigBody(cur):
			return "rsi_div_down", true
		case bearMACDDiv && rsi >= 40 && IsBearish(cur) && sigBody(cur):
			return "macd_div_down", true
		case pb >= 0.88 && maxRSI5 >= 68 && IsBearish(cur) && sigBody(cur) && hist <= prevHist && rciFast <= prevRciFast:
			return "bb_bounce_down", true
		case maxRciFast3 >= 80 && rciFast < prevRciFast && rsi > 30 && IsBearish(cur) && sigBody(cur):
			return "rci_turn_down", true
		case maCrossDown && pb < 0.5 && pb > 0 && expanding && hist < 0 && rsi > 30 && rsi <= 55 && sigBody(cur):
			return "ma_cross_down", true
		case rciTrend <= -80 && rciFast > rciSlow && rciFast < prevRciFast && rsi >= 30 && rsi <= 60 && sigBody(cur):
			return "rci_layer_down", true
		case maxRciFast5 >= 70 && sarFlipDir == -1 && IsBearish(cur) && sigBody(cur) && rsi > 28:
			return "rci_sar_down", true
		case isPullbackToMAShort(bars, maF, maS) && rsi > 32 && rsi <= 60 && hist < prevHist && hist < 0 && sigBody(cur):
			return "pullback_to_ma_down", true
		case isPullbackToBBMidShort(bars, bbMid) && rsi > 32 && rsi <= 60 && hist < prevHist && sigBody(cur):
			return "pullback_to_bbmid_down", true
		case triTopOK && cur.C < triTopNeck && IsBearish(cur) && sigBody(cur) && rsi > 28:
			return "triple_top", true
		case dblTopOK && cur.C < dblTopNeck && IsBearish(cur) && sigBody(cur) && rsi > 28:
			return "double_top", true
		case rise10 >= 0.025 && IsBearish(cur) && Body(cur) >= 0.6*Range(cur) && sigBody(cur) && rsi > 28 && hist <= prevHist && rciFast <= prevRciFast:
			return "spike_top_black", true
		case rise10 >= 0.025 && sma10 > 0 && prevC > prevSMA10 && lastC < sma10 && IsBearish(cur) && sigBody(cur) && rsi > 28:
			return "spike_ma_break", true
		case hi20prev > 0 && (hi20prev-lastC)/hi20prev < 0.01 && IsBearish(cur) && Body(cur) >= 0.6*Range(cur) && sigBody(cur) && rsi > 28 && hist <= prevHist && rciFast <= prevRciFast:
			return "high_black", true
		case lowerHigh && maS > 0 && lastC < maS && IsBearish(cur) && sigBody(cur) && rsi > 28:
			return "lower_high_break", true
		case longDCFresh && IsBearish(cur) && sigBody(cur) && rsi > 28:
			return "long_dc", true
		case longDCActive && dcFresh30 && maS > 0 && bars[len(bars)-1].H >= maS*0.995 && lastC < maS && IsBearish(cur) && sigBody(cur) && rsi > 28 && rsi < 65:
			return "dc_pullback", true
		case sma200 > 0 && prevSma200 > 0 && sma200 < prevSma200 && lastC < sma200 && bars[len(bars)-1].H >= sma200*0.995 && IsBearish(cur) && sigBody(cur) && rsi > 28:
			return "dma_resist", true
		case isVolumeSurge(bars) && IsBearish(cur) && sigBody(cur):
			return "vol_surge_down", true
		case prevRSI > 70 && rsi <= 70 && IsBearish(cur) && sigBody(cur):
			return "rsi_fade", true
		case prevHist >= 0 && hist < 0:
			if rsi > 30 && pb > -0.1 && flips < 3 && IsBearish(cur) && sigBody(cur) {
				if macdLine > 0 {
					return "macd_cross_down_high", true
				}
				return "macd_cross_down", true
			}
		}
		return "", false
	}
	// 売り玉の出口：買いパターン群（EvalEntry trend_up 枝と同条件・同順序）。
	if side == Sell {
		bbUp, _ := bbBreakout(closes, e.cfg.BBPeriod, e.cfg.BBK)
		switch {
		case bbUp:
			return "bb_breakout_up", true
		case isBBRideMomentum(bars, pb, rsi, hist, prevHist) && sigBody(cur):
			return "bb_ride_momentum", true
		case IsMorningStar(bars) && sigBody(cur) && rsi < 72:
			return "morning_star", true
		case IsBullishEngulfing(prev, cur) && sigBody(cur) && rsi < 72:
			return "bullish_engulfing", true
		case IsThreeSoldiers(bars) && sigBody(cur) && rsi < 72:
			return "three_soldiers", true
		case isHammerAtLow(bars) && sigBody(cur) && rsi < 72:
			return "hammer_low", true
		case minRSI5 <= 20 && prevHist <= 0 && hist > 0 && rsi < 70 && pb < 1.1 && flips < 3 && IsBullish(cur) && sigBody(cur):
			return "oversold_macd", true
		case bullRSIDiv && IsBullish(cur) && sigBody(cur):
			return "rsi_div", true
		case bullMACDDiv && rsi <= 60 && IsBullish(cur) && sigBody(cur):
			return "macd_div", true
		case pb <= 0.12 && minRSI5 <= 32 && IsBullish(cur) && sigBody(cur) && hist >= prevHist && rciFast >= prevRciFast:
			return "bb_bounce", true
		case minRciFast3 <= -80 && rciFast > prevRciFast && rsi < 70 && IsBullish(cur) && sigBody(cur):
			return "rci_turn", true
		case maCrossUp && pb > 0.5 && pb < 1.0 && expanding && hist > 0 && rsi >= 45 && rsi < 70 && sigBody(cur):
			return "ma_cross", true
		case rciTrend >= 80 && rciFast < rciSlow && rciFast > prevRciFast && rsi >= 40 && rsi <= 70 && sigBody(cur):
			return "rci_layer", true
		case minRciFast5 <= -70 && sarFlipDir == 1 && IsBullish(cur) && sigBody(cur) && rsi < 72:
			return "rci_sar", true
		case isPullbackToMA(bars, maF, maS) && rsi >= 40 && rsi < 68 && hist > prevHist && hist > 0 && sigBody(cur):
			return "pullback_to_ma", true
		case isPullbackToBBMid(bars, bbMid) && rsi >= 40 && rsi < 68 && hist > prevHist && sigBody(cur):
			return "pullback_to_bbmid", true
		case dblBotOK && cur.C > dblBotNeck && IsBullish(cur) && sigBody(cur) && rsi < 72:
			return "double_bottom", true
		case invHSOk && cur.C > invHSNeck && IsBullish(cur) && sigBody(cur) && rsi < 72:
			return "inv_head_shoulder", true
		case drop5 >= 0.02 && IsBullish(cur) && Body(cur) >= 0.5*Range(cur) && rsi < 55 && sigBody(cur) && hist >= prevHist && rciFast >= prevRciFast:
			return "v_bottom", true
		case drop5 >= 0.015 && IsBullish(cur) && Body(cur) >= 0.6*Range(cur) && sigBody(cur) && rsi < 68 && hist >= prevHist && rciFast >= prevRciFast:
			return "big_white_dip", true
		case hi20prev > 0 && lastC > hi20prev && expanding && rsi < 75 && IsBullish(cur):
			return "new_high", true
		case isVolumeSurge(bars) && IsBullish(cur) && sigBody(cur):
			return "vol_surge", true
		case prevRSI < 30 && rsi >= 30 && IsBullish(cur) && sigBody(cur):
			return "rsi_recover", true
		case prevHist <= 0 && hist > 0:
			if rsi < 70 && pb < 1.1 && flips < 3 && IsBullish(cur) && sigBody(cur) {
				if macdLine < 0 || nearLongMA {
					return "macd_cross_deep", true
				}
				return "macd_cross", true
			}
		}
		return "", false
	}
	return "", false
}
