package trading

import (
	"math"
	"sort"

	"github.com/cryptoEngine/core/domain"
)

// Candidate は1銘柄の評価結果。
type Candidate struct {
	Symbol      string
	Signal      *domain.Signal
	HasOpenPos  bool    // その銘柄に建玉あり
	SpreadRatio float64 // 板のスプレッド率（ギア入力用）
	// TP/MaxTPRatio は到達可能性判定用（実験 E3）。0 は無効扱い。
	// TPRatio > 0 は額面連動モード（比率そのものを上限比較する）。
	TP         float64
	TPRatio    float64
	MaxTPRatio float64
}

// Selection は選定結果。
type Selection struct {
	Symbol      string
	Side        domain.TradeSide
	Price       float64
	Confidence  float64
	Reason      string
	SpreadRatio float64
	TP          float64
	TPRatio     float64
	MaxTPRatio  float64
}

// SelectBest は候補から最良の1件を選ぶ。ルール:
//   - HOLD・信頼度が minConfidence 未満は除外
//   - 建玉なし銘柄の SELL（空売り不可・現物）は除外
//   - 建玉あり銘柄の BUY（ナンピン防止）は除外
//   - 残りから信頼度最大を選択。空なら nil。
func SelectBest(cands []Candidate, minConfidence float64) *Selection {
	var best *Selection
	for _, c := range cands {
		if c.Signal == nil {
			continue
		}
		if c.Signal.Confidence < minConfidence {
			continue
		}
		var side domain.TradeSide
		switch c.Signal.Type {
		case domain.Buy:
			if c.HasOpenPos {
				continue
			}
			side = domain.TradeBuy
		case domain.Sell:
			if !c.HasOpenPos {
				continue
			}
			side = domain.TradeSell
		default:
			continue
		}
		if best == nil || c.Signal.Confidence > best.Confidence {
			best = &Selection{
				Symbol:      c.Symbol,
				Side:        side,
				Price:       c.Signal.MidPrice,
				Confidence:  c.Signal.Confidence,
				Reason:      c.Signal.Reason,
				SpreadRatio: c.SpreadRatio,
				TP:          c.TP,
				TPRatio:     c.TPRatio,
				MaxTPRatio:  c.MaxTPRatio,
			}
		}
	}
	return best
}

// GMOSymbolRule は銘柄の最小数量・刻みを返す。未登録は ok=false。
func GMOSymbolRule(gmoSymbol string) (min, step float64, ok bool) {
	rule, ok := gmoSizeRules[gmoSymbol]
	if !ok {
		return 0, 0, false
	}
	return rule.Min, rule.Step, true
}

// SizeForNotional は想定額から発注数量を決める。最小数量に満たない場合は
// 想定額を最小額面まで引き上げ（余力・上限内で収まる場合のみ ok=true）。
// 余力・上限を超える場合は ok=false（発注見送り）。
func SizeForNotional(tradeSize, price, min, step, maxPos, balance float64) (qty float64, ok bool) {
	if price <= 0 || min <= 0 || step <= 0 {
		return 0, false
	}
	notional := tradeSize
	if floor := min * price * (1 + GMOOrderFeeBps); floor > notional {
		notional = floor
	}
	if maxPos > 0 && notional > maxPos {
		return 0, false
	}
	if balance > 0 && notional > balance {
		return 0, false
	}
	qty = math.Floor(notional/price/step) * step
	if qty < min {
		return 0, false
	}
	return qty, true
}

// SortSymbols は表示・ログ用の安定ソート。
func SortSymbols(symbols []string) []string {
	out := append([]string(nil), symbols...)
	sort.Strings(out)
	return out
}

// GearParams はギア（発注サイズ）自動可変のパラメータ。
type GearParams struct {
	// TradeSize は基準想定額（TRADE_SIZE_USDT）。
	TradeSize float64
	// MaxPos は最大ポジション評価額（0 で無制限扱い）。
	MaxPos float64
	// Balance は JPY 余力（0 以下で上限なし扱い）。
	Balance float64
	// RiskFraction は余力に対する1トレード上限割合（例 0.2＝余力の20%）。
	// 0 以下は無効（Balance 上限を使わない）。
	RiskFraction float64
	// SpreadRef はスプレッド基準値。これ以下なら減速なし。
	SpreadRef float64
}

// SizeForSignal はシグナルに応じた発注数量を決める（ギア自動可変）。
//  1. 下限: 銘柄最小額面（min*price）まで自動引き上げ
//  2. 信頼度スケール: 0.5 + confidence（0.5〜1.5 倍）
//  3. スプレッドペナルティ: spreadRatio が SpreadRef 超過で減速（最低 0.25 倍）
//  4. 上限: MaxPos と Balance*RiskFraction を超えたら見送り
func SizeForSignal(tradeSize, price, min, step float64, conf, spreadRatio float64, p GearParams) (qty float64, ok bool) {
	if price <= 0 || min <= 0 || step <= 0 {
		return 0, false
	}
	if conf < 0 {
		conf = 0
	}
	if conf > 1 {
		conf = 1
	}
	ref := p.SpreadRef
	if ref <= 0 {
		ref = 0.0005
	}
	floor := min * price * (1 + GMOOrderFeeBps)
	notional := tradeSize
	if floor > notional {
		notional = floor
	}
	notional *= 0.5 + conf
	mult := 1.0
	if spreadRatio > ref {
		mult = ref / spreadRatio
		if mult < 0.25 {
			mult = 0.25
		}
	}
	notional *= mult
	if p.MaxPos > 0 && notional > p.MaxPos {
		return 0, false
	}
	if p.RiskFraction > 0 && p.Balance > 0 && notional > p.Balance*p.RiskFraction {
		return 0, false
	}
	if p.RiskFraction <= 0 && p.Balance > 0 && notional > p.Balance {
		return 0, false
	}
	qty = math.Floor(notional/price/step) * step
	if qty < min {
		return 0, false
	}
	return qty, true
}
