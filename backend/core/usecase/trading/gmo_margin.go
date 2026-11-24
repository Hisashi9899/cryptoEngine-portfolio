package trading

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cryptoEngine/core/domain"
)

// gmo_margin.go は GMO 取引所レバレッジの等倍執行を提供する。
//
// 方針（ユーザ指示）：レバレッジは掛けない。証拠金以下の額面（実効1倍）で
// 空売りを含む両建てを可能にする。勝率が安定するまで倍率は上げない。
// 発注は行わない読み取り（FetchMargin/FetchOpenPositions）と合わせ、
// 建玉の特定には positionId を使う（closeOrder に必須）。
//
// 証拠金シンボルは Public GET /v1/symbols から取得する（XXX_JPY 12銘柄）。
// 現物17銘柄のうち XLM/ASTR/FCR/NAC/WILD に証拠金シンボルは無い。

// defaultMarginSymbolsURL は証拠金ルール取得先（GMO Public API）。
const defaultMarginSymbolsURL = "https://api.coin.z.com/public/v1/symbols"

// marginLeverageFeeRate は建玉管理料率（日次・6:00 JST 跨ぎで課金）。
const marginLeverageFeeRate = 0.0004

// marginSymbolsURL はテスト差し替え可能にする。
func marginSymbolsURL() string {
	if v := os.Getenv("GMO_PUBLIC_SYMBOLS_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return defaultMarginSymbolsURL
}

// parseMarginSymbols は Public symbols 応答から XXX_JPY の rules を抜く。
func parseMarginSymbols(body []byte) (map[string]gmoSizeRule, error) {
	var env struct {
		Status int `json:"status"`
		Data   []struct {
			Symbol       string `json:"symbol"`
			MinOrderSize string `json:"minOrderSize"`
			SizeStep     string `json:"sizeStep"`
			TakerFee     string `json:"takerFee"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("gmo unmarshal symbols: %w", err)
	}
	if env.Status != 0 {
		return nil, fmt.Errorf("gmo symbols rejected: status=%d", env.Status)
	}
	out := map[string]gmoSizeRule{}
	for _, s := range env.Data {
		sym := strings.ToUpper(strings.TrimSpace(s.Symbol))
		if !strings.HasSuffix(sym, "_JPY") {
			continue
		}
		mn, err1 := strconv.ParseFloat(s.MinOrderSize, 64)
		st, err2 := strconv.ParseFloat(s.SizeStep, 64)
		if err1 != nil || err2 != nil || mn <= 0 || st <= 0 {
			continue
		}
		out[sym] = gmoSizeRule{Min: mn, Step: st}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("gmo no margin symbols in response")
	}
	return out, nil
}

// toMarginSymbol は現物短縮形を証拠金形式に変換する（XRP→XRP_JPY）。
// 証拠金シンボルが無い銘柄はエラーを返す。
func toMarginSymbol(spot string) (string, error) {
	s := strings.ToUpper(strings.TrimSpace(spot))
	if s == "" {
		return "", fmt.Errorf("symbol is required")
	}
	if strings.HasSuffix(s, "_JPY") {
		return s, nil
	}
	// Binance 形式（BTCUSDT 等）は PAIR の _JPY 化を防ぐため拒否する。
	if strings.HasSuffix(s, "USDT") {
		return "", fmt.Errorf("gmo margin requires spot symbol (e.g. XRP, not XRPUSDT), got %q", spot)
	}
	return s + "_JPY", nil
}

// LoadMarginRules は証拠金ルールを取得して executor に保持する。
func (e *GMOOrderExecutor) LoadMarginRules(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, marginSymbolsURL(), nil)
	if err != nil {
		return err
	}
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gmo margin symbols request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("gmo margin symbols read: %w", err)
	}
	rules, err := parseMarginSymbols(body)
	if err != nil {
		return err
	}
	e.marginRules = rules
	return nil
}

// marginRule は読み込み済み証拠金ルールを返す。未読込・未登録は fail-closed。
func (e *GMOOrderExecutor) marginRule(marginSymbol string) (gmoSizeRule, bool) {
	if len(e.marginRules) == 0 {
		return gmoSizeRule{}, false
	}
	rule, ok := e.marginRules[strings.ToUpper(strings.TrimSpace(marginSymbol))]
	return rule, ok
}

// SupportsMargin は現物シンボルが証拠金取引可能かを返す。
func (e *GMOOrderExecutor) SupportsMargin(spot string) bool {
	gmoSymbol, err := toMarginSymbol(spot)
	if err != nil {
		return false
	}
	_, ok := e.marginRule(gmoSymbol)
	return ok
}

// SpotMid は現物ティッカーの中値を返す（exit sweep の予備取得用）。
func (e *GMOOrderExecutor) SpotMid(symbol string) (float64, error) {
	t, err := e.mdClient.FetchTicker(symbol)
	if err != nil {
		return 0, err
	}
	if t.Bid > 0 && t.Ask > 0 {
		return (t.Bid + t.Ask) / 2, nil
	}
	if t.Last > 0 {
		return t.Last, nil
	}
	return 0, fmt.Errorf("gmo no price for %s", symbol)
}

// marginSpotPrice は証拠金発注の参照価格を現物ティッカーから取る（同一値）。
func (e *GMOOrderExecutor) marginSpotPrice(spot string) (float64, error) {
	t, err := e.mdClient.FetchTicker(spot)
	if err != nil {
		return 0, fmt.Errorf("gmo margin price fetch failed (ticker %s): %w", spot, err)
	}
	ask := t.Ask
	if ask <= 0 {
		ask = t.Last
	}
	if ask <= 0 {
		return 0, fmt.Errorf("gmo margin price unavailable for %s", spot)
	}
	return ask, nil
}

// MarginSizeFor は証拠金の発注数量を決める（ギア＋等倍上限）。
// 基準額面＝余力×リスク割合。想定額面が余力を超えたら余力まで切り詰め、
// 最小未満になれば見送る（ok=false）。レバレッジは掛けない。
// fallbackMult は基準未満時のフォールバック上限倍率（基準×倍率）。
// 旧実装の「余力いっぱい」は確信度を無視して余力の大半を一発に投げる
// hogging（2026-09-26 BCH 0.4枚＝余力87%の実績）になるため、上限付きの
// 確信度連動に変えた。0以下でフォールバック無効（現物と同等の見送り）。
func (e *GMOOrderExecutor) MarginSizeFor(spot string, price, conf, balance, riskFraction, spreadRatio, fallbackMult float64) (float64, bool) {
	gmoSymbol, err := toMarginSymbol(spot)
	if err != nil {
		return 0, false
	}
	rule, ok := e.marginRule(gmoSymbol)
	if !ok || price <= 0 {
		return 0, false
	}
	base := balance * riskFraction
	if base <= 0 {
		return 0, false
	}
	if spreadRatio < 0 {
		spreadRatio = 0
	}
	qty, ok := SizeForSignal(base, price, rule.Min, rule.Step, conf, spreadRatio, GearParams{
		TradeSize: base, MaxPos: base * 1.5,
	})
	if !ok {
		if fallbackMult <= 0 {
			return 0, false
		}
		confC := conf
		if confC < 0 {
			confC = 0
		}
		if confC > 1 {
			confC = 1
		}
		// 基準額面×確信度倍率を刻み上げ（最小数量に届かせる）。
		// 浮動小数点ダストでの1刻み超過を避ける微差引きで切り上げする。
		scaled := base * (0.5 + confC)
		up := math.Ceil(scaled/price/rule.Step-1e-9) * rule.Step
		// 上限は基準×倍率（切り捨て）と最小数量の大きい方。
		// 最小数量自体は買える場合に限り許可し、余力で買えない額面は
		// 発注せず見送る（等倍厳守。約定時の資金不足エラーを防ぐ）。
		capQty := math.Floor(base*fallbackMult/price/rule.Step) * rule.Step
		if capQty < rule.Min {
			capQty = rule.Min
		}
		qty = up
		if qty > capQty {
			qty = capQty
		}
		if qty < rule.Min || qty*price > balance {
			return 0, false
		}
		return qty, true
	}
	// 等倍上限：額面が余力を超えたら切り詰める。
	if notional := qty * price; balance > 0 && notional > balance {
		qty = math.Floor(balance/price/rule.Step) * rule.Step
		if qty < rule.Min {
			return 0, false
		}
	}
	return qty, true
}

// checkMarginFunds は等倍ガード：額面が証拠金余力を超えれば拒否する。
// レバレッジは掛けない（notional <= available）。
func (e *GMOOrderExecutor) checkMarginFunds(ctx context.Context, qty, price float64) error {
	assets, err := e.FetchMargin(ctx)
	if err != nil {
		return fmt.Errorf("gmo margin funds check failed: %w", err)
	}
	var avail float64
	fmt.Sscanf(strings.TrimSpace(assets.AvailableAmount), "%f", &avail)
	need := qty * price * (1 + GMOOrderFeeBps)
	if fundsShort(avail, need) {
		return fmt.Errorf("insufficient margin: need ~%.0f JPY, available %.0f JPY (1x: notional must fit margin)",
			need, avail)
	}
	return nil
}

// MarginMarketOrder は証拠金シンボルへ成行で建てる（1x：額面は余力内）。
// spot は現物短縮形（XRP）。数量は約定数量を返す。
func (e *GMOOrderExecutor) MarginMarketOrder(ctx context.Context, spot string, side domain.TradeSide, quantity float64) (*OrderResult, error) {
	gmoSymbol, err := toMarginSymbol(spot)
	if err != nil {
		return nil, err
	}
	if quantity <= 0 {
		return nil, fmt.Errorf("quantity must be positive: %.8f", quantity)
	}
	if e.apiKey == "" || e.apiSecret == "" {
		return nil, fmt.Errorf("gmo api key/secret not configured (GMO_API_KEY / GMO_API_SECRET)")
	}
	if os.Getenv("GMO_MAINTENANCE_CHECK") != "off" && inMaintenanceWindow(e.now()) {
		return nil, fmt.Errorf("gmo maintenance window (Sat 09:00-11:00 JST): order skipped")
	}
	sideStr := strings.ToUpper(string(side))
	if sideStr != "BUY" && sideStr != "SELL" {
		return nil, fmt.Errorf("invalid side: %s", side)
	}
	rule, ok := e.marginRule(gmoSymbol)
	if !ok {
		return nil, fmt.Errorf("gmo no margin rule for %s (no XXX_JPY symbol or rules not loaded)", gmoSymbol)
	}
	qty := roundDownSize(quantity, rule.Step)
	if qty < rule.Min {
		return nil, fmt.Errorf("gmo margin quantity %v below minimum %v (%s)", qty, rule.Min, gmoSymbol)
	}
	price, err := e.marginSpotPrice(strings.ToUpper(strings.TrimSpace(spot)))
	if err != nil {
		return nil, err
	}
	if err := e.checkMarginFunds(ctx, qty, price); err != nil {
		return nil, err
	}
	return e.orderLoop(ctx, gmoSymbol, sideStr, "MARKET", "", "", qty, spot)
}

// CloseMarginPosition は建玉を positionId 指定で決済する。
// GMO は1発注を複数 positionId に分割するため、数量が残る限り
// side 一致の建玉を崩し続ける（単一始末で終わらせない）。
// 2026-09-26に単一始末で BCH 0.6 のうち 0.1 だけ閉じて残りを
// 孤児化した実績あり（bot は全閉じと誤認して追跡放棄）。
// 照会が古い一覧を返しても、処理済み ID は二重決済しない。
// 全量決済できなければ、決済分＋残量エラーを返す。呼び出し側は
// 建玉を減量保持して再試行すること（全量失敗時は従来通り維持）。
func (e *GMOOrderExecutor) CloseMarginPosition(ctx context.Context, spot string, side domain.TradeSide, quantity float64) (*OrderResult, error) {
	gmoSymbol, err := toMarginSymbol(spot)
	if err != nil {
		return nil, err
	}
	if quantity <= 0 {
		return nil, fmt.Errorf("quantity must be positive: %.8f", quantity)
	}
	// 建玉サイドと決済サイドは逆向き。
	closeSide := "SELL"
	if strings.ToUpper(string(side)) == "SELL" {
		closeSide = "BUY"
	}
	wantSide := strings.ToUpper(string(side))
	// 丸め残差では再試行しない。
	const dustQty = 1e-9
	remaining := quantity
	doneIDs := map[int64]bool{}
	var totalQty, totalValue float64
	var knownQty float64
	var lastOrderID string
	var firstErr error
	closeSideT := domain.TradeSell
	if closeSide == "BUY" {
		closeSideT = domain.TradeBuy
	}
	for remaining > dustQty {
		positions, err := e.FetchOpenPositions(ctx, gmoSymbol)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			break
		}
		var target *GMOOpenPosition
		var have float64
		for i := range positions {
			if strings.ToUpper(positions[i].Side) != wantSide {
				continue
			}
			if doneIDs[positions[i].PositionID] {
				continue
			}
			var sz float64
			fmt.Sscanf(strings.TrimSpace(positions[i].Size), "%f", &sz)
			doneIDs[positions[i].PositionID] = true
			if sz <= 0 {
				continue
			}
			p := positions[i]
			target = &p
			have = sz
			break
		}
		if target == nil {
			break
		}
		size := remaining
		if size > have {
			size = have
		}
		res, err := e.closeOnce(ctx, gmoSymbol, closeSide, target.PositionID, size, spot)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if res.Quantity > 0 {
			totalQty += res.Quantity
			remaining -= res.Quantity
			lastOrderID = res.OrderID
			if res.Price > 0 {
				totalValue += res.Price * res.Quantity
				knownQty += res.Quantity
			}
		}
	}
	if totalQty <= 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, fmt.Errorf("gmo no open %s position for %s to close", wantSide, gmoSymbol)
	}
	price := 0.0
	if knownQty > 0 {
		price = totalValue / knownQty
	}
	out := &OrderResult{
		Symbol:    spot,
		Side:      closeSideT,
		Price:     price,
		Quantity:  totalQty,
		OrderID:   lastOrderID,
		Status:    "filled",
		Timestamp: e.now().UnixMilli(),
	}
	if remaining > dustQty {
		if firstErr == nil {
			firstErr = fmt.Errorf("gmo partial close %s: closed %.8f, remaining %.8f", gmoSymbol, totalQty, remaining)
		}
		return out, firstErr
	}
	return out, nil
}

// closeOnce は単一建玉の決済注文を出す（リトライは orderLoop 相当の1回のみ）。
func (e *GMOOrderExecutor) closeOnce(ctx context.Context, gmoSymbol, sideStr string, positionID int64, qty float64, origSymbol string) (*OrderResult, error) {
	timestamp := strconv.FormatInt(e.now().UnixMilli(), 10)
	body, err := json.Marshal(map[string]any{
		"symbol":        gmoSymbol,
		"side":          sideStr,
		"executionType": "MARKET",
		"settlePosition": []map[string]any{
			{"positionId": positionID, "size": formatSize(qty, e.sizeStepFor(gmoSymbol))},
		},
	})
	if err != nil {
		return nil, err
	}
	path := "/v1/closeOrder"
	signature := signGMO(timestamp+"POST"+path+string(body), e.apiSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+path, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("API-KEY", e.apiKey)
	req.Header.Set("API-TIMESTAMP", timestamp)
	req.Header.Set("API-SIGN", signature)
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gmo close order network: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gmo close order read: %w", err)
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("gmo unexpected status %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}
	var raw gmoOrderResponse
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("gmo unmarshal close response: %w", err)
	}
	if raw.Status != 0 {
		var gerr gmoErrorResponse
		detail := ""
		if json.Unmarshal(respBody, &gerr) == nil && len(gerr.Messages) > 0 {
			detail = gerr.Messages[0].MessageString
		}
		return nil, fmt.Errorf("gmo close rejected: status=%d %s (sent %s)", raw.Status, detail, truncate(string(body), 200))
	}
	if raw.Data == "" {
		return nil, fmt.Errorf("gmo empty order id in close response")
	}
	price := e.fetchFillPrice(ctx, gmoSymbol, raw.Data)
	side := domain.TradeSell
	if sideStr == "BUY" {
		side = domain.TradeBuy
	}
	return &OrderResult{
		Symbol:    origSymbol,
		Side:      side,
		Price:     price,
		Quantity:  qty,
		OrderID:   raw.Data,
		Status:    "filled",
		Timestamp: e.now().UnixMilli(),
	}, nil
}

// jstSixAMCuts は entry→exit 間に跨いだ 6:00 JST の回数を返す。
// 建玉管理料（0.04%/日）はこの回数分だけ課金される。
// 日付境界は JST カレンダーで判定する（Truncate の絶対時刻丸めは使わない）。
func jstSixAMCuts(entryMs, exitMs int64) int {
	if exitMs <= entryMs {
		return 0
	}
	jst := time.FixedZone("JST", 9*60*60)
	sy, sm, sd := time.UnixMilli(entryMs).In(jst).Date()
	ey, em, ed := time.UnixMilli(exitMs).In(jst).Date()
	cuts := 0
	for d := time.Date(sy, sm, sd, 0, 0, 0, 0, jst); ; d = d.AddDate(0, 0, 1) {
		six := time.Date(d.Year(), d.Month(), d.Day(), 6, 0, 0, 0, jst).UnixMilli()
		if six > entryMs && six <= exitMs {
			cuts++
		}
		if d.Year() == ey && d.Month() == em && d.Day() == ed {
			break
		}
		if cuts > 365 {
			break
		}
	}
	return cuts
}

// MarginFee は建玉管理料の概算（額面×0.04%×跨ぎ日数）を返す。
func MarginFee(notional float64, entryMs, exitMs int64) float64 {
	if notional <= 0 {
		return 0
	}
	cuts := jstSixAMCuts(entryMs, exitMs)
	fee := notional * marginLeverageFeeRate * float64(cuts)
	return math.Floor(fee*100) / 100
}
