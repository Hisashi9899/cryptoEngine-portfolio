package trading

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/cryptoEngine/core/domain"
)

// GMOOrderFeeBps は発注時の概算手数料率（taker 想定・余裕側）。
const GMOOrderFeeBps = 0.0005

// SymbolAffordability は1銘柄の取引可否。
type SymbolAffordability struct {
	Symbol     string  `json:"symbol"`
	MinQty     float64 `json:"min_qty"`
	Price      float64 `json:"price"`
	MinJPY     float64 `json:"min_jpy"`
	Affordable bool    `json:"affordable"`
}

// AffordabilityReport は口座余力に対する全銘柄の取引可否。
type AffordabilityReport struct {
	JPYAvailable float64               `json:"jpy_available"`
	FeeBps       float64               `json:"fee_bps"`
	Symbols      []SymbolAffordability `json:"symbols"`
}

// AffordableSymbols は取引可能な銘柄一覧を返す。
func (r *AffordabilityReport) AffordableSymbols() []string {
	out := []string{}
	for _, s := range r.Symbols {
		if s.Affordable {
			out = append(out, s.Symbol)
		}
	}
	return out
}

// gmoBaseCurrency は GMO シンボルから基軸通貨を取り出す（現物は短縮形のため通常そのまま）。
func gmoBaseCurrency(gmoSymbol string) string {
	if i := strings.Index(gmoSymbol, "_"); i > 0 {
		return gmoSymbol[:i]
	}
	return gmoSymbol
}

// assetAvailable は資産一覧から指定通貨の available を返す。なければ 0。
func assetAvailable(assets []GMOAsset, symbol string) float64 {
	for _, a := range assets {
		if strings.EqualFold(a.Symbol, symbol) {
			var v float64
			fmt.Sscanf(a.Available, "%f", &v)
			if v < 0 {
				return 0
			}
			return v
		}
	}
	return 0
}

// CheckAffordability は余力に対する銘柄別の取引可否を判定する。
// fetchTicker は銘柄の現在値取得（Last、なければ Bid/Ask 中値を使う）。
func CheckAffordability(assets []GMOAsset, fetchTicker func(ctx context.Context, symbol string) (*domain.Ticker, error)) (*AffordabilityReport, error) {
	jpy := assetAvailable(assets, "JPY")
	report := &AffordabilityReport{JPYAvailable: jpy, FeeBps: GMOOrderFeeBps}
	ctx := context.Background()
	for symbol, rule := range gmoSizeRules {
		entry := SymbolAffordability{Symbol: symbol, MinQty: rule.Min}
		t, err := fetchTicker(ctx, symbol)
		if err != nil || t == nil {
			report.Symbols = append(report.Symbols, entry)
			continue
		}
		price := t.Last
		if price <= 0 && t.Bid > 0 && t.Ask > 0 {
			price = (t.Bid + t.Ask) / 2
		}
		if price <= 0 {
			report.Symbols = append(report.Symbols, entry)
			continue
		}
		entry.Price = price
		entry.MinJPY = rule.Min * price * (1 + GMOOrderFeeBps)
		entry.Affordable = jpy >= entry.MinJPY
		report.Symbols = append(report.Symbols, entry)
	}
	return report, nil
}

// fundsShort は資金不足かを判定する。数量計算の浮動小数点ダスト
// （例：保有 14.6 に対し計算数量 14.600000000000001）での誤検出を防ぐ
// ため、相対 1e-9 の微差は不足とみなさない。実不足（刻み未満の欠落等）
// は正しく検出する。2026-09-26に SUI 決済不能（5秒毎の失敗スパム＋
// 塩漬け）を起こした実績あり。発注電文自体は formatSize で正規化される
// ため、ここでの許容は約定内容に影響しない。
func fundsShort(have, need float64) bool {
	if need <= 0 {
		return false
	}
	return have < need-1e-9*math.Max(1, math.Abs(need))
}

// checkFunds は発注前の資金ガード。不足時は具体額入りのエラーを返す。
// BUY: JPY 余力が数量×想定価格＋手数料を下回れば拒否。
// SELL: 基軸通貨の保有が必要数量を下回れば拒否（ダスト許容付き）。
func (e *GMOOrderExecutor) checkFunds(ctx context.Context, side, gmoSymbol string, qty float64) error {
	assets, err := e.FetchAssets(ctx)
	if err != nil {
		return fmt.Errorf("gmo funds check failed (assets): %w", err)
	}
	if side == "SELL" {
		base := gmoBaseCurrency(gmoSymbol)
		have := assetAvailable(assets, base)
		if fundsShort(have, qty) {
			return fmt.Errorf("insufficient %s balance: need %v %s, available %v (gmo-affordability で確認できます)",
				base, qty, gmoSymbol, have)
		}
		return nil
	}
	t, err := e.mdClient.FetchTicker(gmoSymbol)
	if err != nil {
		return fmt.Errorf("gmo funds check failed (ticker %s): %w", gmoSymbol, err)
	}
	ask := t.Ask
	if ask <= 0 {
		ask = t.Last
	}
	if ask <= 0 {
		return fmt.Errorf("gmo funds check failed: no price for %s", gmoSymbol)
	}
	need := qty * ask * (1 + GMOOrderFeeBps)
	jpy := assetAvailable(assets, "JPY")
	if fundsShort(jpy, need) {
		return fmt.Errorf("insufficient JPY balance: need ~%.0f JPY (%v %s + fee), available %.0f JPY (gmo-affordability で取引可能な銘柄を確認できます)",
			need, qty, gmoSymbol, jpy)
	}
	return nil
}

// gmoTickerFetcher は現在値取得の抽象。exchange.GMOClient が実装する。
type gmoTickerFetcher interface {
	FetchTicker(symbol string) (*domain.Ticker, error)
}
