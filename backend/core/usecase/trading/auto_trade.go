package trading

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cryptoEngine/core/domain"
	"github.com/cryptoEngine/core/infrastructure/logging"
)

// AutoTradeUseCase はシグナルを実際の注文へ変換する自動売買パイプライン。
//
//  1. AUTO_TRADE_ENABLED / リスク停止を確認
//  2. BUY は最大ポジションサイズをチェック（超過なら拒否）
//  3. Binance へ成行注文（リトライ最大 3 回）
//  4. ポジションを更新（BUY: 平均取得価格再計算 / SELL: 減少 + 確定損益）
//  5. 日次損失を記録し、STOP_ON_LOSS で自動売買を自動 OFF
//  6. trade log CSV へ監査ログを追記
//
// 注文実績・ポジション・リスク状態を JSON で返し、API 側で UI 表示に使う。
type AutoTradeUseCase struct {
	executor  OrderExecutor
	positions *PositionManager
	risk      *RiskManager
	log       *TradeLog
	logger    *logging.Logger
}

// NewAutoTradeUseCase は自動売買パイプラインを生成する。
func NewAutoTradeUseCase(executor OrderExecutor, positions PositionStore, risk *RiskManager, log *TradeLog, logger *logging.Logger) *AutoTradeUseCase {
	risk.SeedDailyLossFromLog(log)
	return &AutoTradeUseCase{
		executor:  executor,
		positions: NewPositionManager(positions, logger),
		risk:      risk,
		log:       log,
		logger:    logger,
	}
}

// ExecuteOrderInput は 1 回の自動売買実行の入力。
type ExecuteOrderInput struct {
	Symbol string           `json:"symbol"`
	Side   domain.TradeSide `json:"side"`
	Price  float64          `json:"price"`  // 数量計算・時価評価に使う参照価格（シグナルの midPrice）
	Reason string           `json:"reason"` // signal / tp / sl / risk / max_hold
	// Quantity は発注数量（base asset）。>0 の場合は auto-loop の SizeForSignal
	// 確定値（Kelly/explore＋ギア＋余力上限済み）をそのまま使う。0 以下は
	// 従来通り TradeSize/Price から算出する（v1 execute-order CLI 互換）。
	Quantity float64 `json:"quantity"`
	// OrderType が "limit" の場合は指値注文（LimitPrice 必須、TimeInForce 既定 FAK）。
	// 空・"market" は成行（従来通り）。指値未対応 executor ではエラーを返す。
	OrderType   string  `json:"order_type"`
	LimitPrice  float64 `json:"limit_price"`
	TimeInForce string  `json:"time_in_force"`
}

// ClosePosition は保有全量の決済（タイムストップ用）。
// ExecuteOrder と異なり数量を TradeSize から計算せず、現在保有の全量を
// 成行 SELL する。建玉なし・リスク停止時は実行せず status を返す。
// 最小数量未満の端数（dust）は executor が拒否するため、その旨を返す。
func (a *AutoTradeUseCase) ClosePosition(ctx context.Context, symbol string, price float64, reason string) (*ExecuteOrderOutput, error) {
	out := &ExecuteOrderOutput{Status: "error", Risk: a.risk.Status(0)}
	now := time.Now().UTC().Format(time.RFC3339)
	if reason == "" {
		reason = "max_hold"
	}

	if !a.risk.Enabled() {
		out.Status = "skipped"
		out.Message = "auto trade disabled"
		return out, nil
	}
	if a.risk.Stopped() {
		out.Status = "rejected"
		out.Message = "auto trade stopped by risk management"
		return out, nil
	}
	if symbol == "" {
		return out, fmt.Errorf("symbol is required")
	}
	if price <= 0 {
		return out, fmt.Errorf("price must be positive: %.2f", price)
	}

	current, err := a.positions.Current(ctx, symbol)
	if err != nil {
		return out, fmt.Errorf("read position: %w", err)
	}
	if current == nil || current.Volume <= 0 {
		out.Status = "skipped"
		out.Message = "no open position"
		return out, nil
	}
	quantity := current.Volume

	order, err := a.executor.MarketOrder(ctx, symbol, domain.TradeSell, quantity)
	// DB 建玉と取引所現物残高の乖離時は全量 SELL が毎回失敗し、
	// maxPos=1 が永久に詰まる。available へ切り詰めて1回再試行する
	// （残りは次 tick 以降に再処理。部分約定の ApplySell が残量を保持する）。
	if err != nil && isInsufficientBaseError(err) {
		if avail, ok := a.exchangeAvailable(ctx, symbol); ok && avail > 0 && avail < quantity {
			a.logger.Warn("close position qty clamped to exchange-available %.8f (db vol %.8f): %v",
				avail, quantity, err)
			quantity = avail
			order, err = a.executor.MarketOrder(ctx, symbol, domain.TradeSell, quantity)
		}
	}
	if err != nil {
		entry := TradeLogEntry{
			Timestamp: now, Action: "SELL", Symbol: symbol,
			Price: price, Amount: quantity,
			Reason: reason, Status: "error",
		}
		_ = a.log.Append(entry)
		a.logger.Error("close position order failed: %v", err)
		return out, err
	}
	out.Order = order

	// ExecuteOrder と同一理由の fill 価格フォールバック（参照 mid で代替）。
	if out.Order.Price <= 0 {
		if price > 0 {
			a.logger.Warn("fill price unknown for %s SELL order %s; falling back to reference price %.2f",
				symbol, order.OrderID, price)
			out.Order.Price = price
		} else {
			return out, fmt.Errorf("fill price unknown and no reference price for %s", symbol)
		}
	}

	pnl, remaining, err := a.positions.ApplySell(ctx, symbol, order.Price, order.Quantity, order.Timestamp)
	if err != nil {
		return out, fmt.Errorf("apply sell: %w", err)
	}
	out.Position = remaining
	// 手数料控除会計：報告損益は実費ベースにする（スリッページは含まない）。
	out.PnlUsdt = a.deductFees(pnl, order.Price, order.Quantity)

	stopped, stopReason := a.risk.RecordTrade(out.PnlUsdt)
	if stopped {
		out.Message = stopReason
	}
	pnlCopy := out.PnlUsdt
	entry := TradeLogEntry{
		Timestamp: now,
		Action:    "SELL",
		Symbol:    symbol,
		Price:     order.Price,
		Amount:    order.Quantity,
		Pnl:       &pnlCopy,
		Reason:    reason,
		OrderID:   order.OrderID,
		Status:    order.Status,
	}
	if err := a.log.Append(entry); err != nil {
		a.logger.Warn("failed to append trade log: %v", err)
	}

	out.Status = "filled"
	if stopped {
		out.Status = "stopped"
	}
	posValue, _ := a.positions.MarketValue(ctx, symbol, price)
	out.Risk = a.risk.Status(posValue)
	return out, nil
}

// isInsufficientBaseError は取引所現物残高不足エラーの判定。
// GMO executor の "insufficient <BASE> balance" と汎用の funds 失敗を拾う。
func isInsufficientBaseError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "insufficient") && strings.Contains(msg, "balance")
}

// exchangeAvailable は取引所の基軸通貨 available を返す。
// executor が残高照会を持たない場合 (Binance 等) は ok=false。
func (a *AutoTradeUseCase) exchangeAvailable(ctx context.Context, symbol string) (avail float64, ok bool) {
	type assetFetcher interface {
		FetchAssets(ctx context.Context) ([]GMOAsset, error)
	}
	af, isOk := a.executor.(assetFetcher)
	if !isOk {
		return 0, false
	}
	assets, err := af.FetchAssets(ctx)
	if err != nil {
		return 0, false
	}
	base := gmoBaseCurrency(symbol)
	avail = assetAvailable(assets, base)
	if avail <= 0 {
		return 0, false
	}
	return avail, true
}

// feeRate は執行手数料率（片道）を返す。GMO 現物は公表値、それ以外は
// Binance taker 相当の悲観値を使う。スリッページは含まない
// （別途 shadow 監査で評価する）。
func (a *AutoTradeUseCase) feeRate() float64 {
	if _, ok := a.executor.(*GMOOrderExecutor); ok {
		return GMOOrderFeeBps
	}
	return 0.001
}

// deductFees は確定損益から往復手数料を控除する。
// 報告損益を実費ベースにするための調整。pnl と同一通貨単位で計算する。
func (a *AutoTradeUseCase) deductFees(pnl, price, quantity float64) float64 {
	if price <= 0 || quantity <= 0 {
		return pnl
	}
	return pnl - 2*a.feeRate()*price*quantity
}

// ExecuteOrderOutput は実行結果（status: filled / skipped / rejected / error）。
type ExecuteOrderOutput struct {
	Status   string           `json:"status"`
	Message  string           `json:"message"`
	Order    *OrderResult     `json:"order,omitempty"`
	Position *domain.Position `json:"position,omitempty"`
	PnlUsdt  float64          `json:"pnl_usdt"`
	Risk     *RiskStatus      `json:"risk"`
}

// ExecuteOrder は入力のシグナルを実際の注文に変換して実行する。
// 失敗した注文は trade log に error で記録してから error を返す。
func (a *AutoTradeUseCase) ExecuteOrder(ctx context.Context, in ExecuteOrderInput) (*ExecuteOrderOutput, error) {
	out := &ExecuteOrderOutput{Status: "error", Risk: a.risk.Status(0)}
	now := time.Now().UTC().Format(time.RFC3339)

	// 1. 自動売買の ON/OFF ゲート。
	if !a.risk.Enabled() {
		out.Status = "skipped"
		out.Message = "auto trade disabled"
		return out, nil
	}
	if a.risk.Stopped() {
		out.Status = "rejected"
		out.Message = "auto trade stopped by risk management"
		return out, nil
	}

	// 数量の計算。参照価格が無い場合はエラー。
	if in.Symbol == "" {
		return out, fmt.Errorf("symbol is required")
	}
	if in.Side != domain.TradeBuy && in.Side != domain.TradeSell {
		return out, fmt.Errorf("invalid side: %s", in.Side)
	}
	if in.Price <= 0 {
		return out, fmt.Errorf("price must be positive: %.2f", in.Price)
	}
	// 数量は auto-loop 確定値（Quantity>0）を優先し、未指定時のみ従来式を使う。
	// これが無いと Kelly/explore＋ギア＋余力上限の計算が死にコードになり、
	// 実発注が TradeSize 固定に戻って資金不足・最小数量割れを起こす。
	quantity := in.Quantity
	if quantity <= 0 {
		quantity = a.risk.TradeSizeUsdt() / in.Price
	}

	// 2. BUY のポジションサイズチェック（実発注額面で判定する）。
	if in.Side == domain.TradeBuy {
		marketValue, err := a.positions.MarketValue(ctx, in.Symbol, in.Price)
		if err != nil {
			return out, fmt.Errorf("read position: %w", err)
		}
		if ok, msg := a.risk.AllowBuyWithAmount(marketValue, quantity*in.Price); !ok {
			out.Status = "rejected"
			out.Message = msg
			a.log.Append(TradeLogEntry{
				Timestamp: now, Action: "BUY", Symbol: in.Symbol,
				Price: in.Price, Amount: quantity,
				Reason: ReasonRisk, Status: "rejected",
			})
			out.Risk = a.risk.Status(marketValue)
			return out, nil
		}
	}

	// 3. 注文の実行（リトライ含む）。指値指定時は LimitOrder 対応 executor が必要。
	var order *OrderResult
	var err error
	if strings.EqualFold(in.OrderType, "limit") {
		if in.LimitPrice <= 0 {
			return out, fmt.Errorf("limit order requires a positive limit price")
		}
		le, ok := a.executor.(LimitOrderExecutor)
		if !ok {
			return out, fmt.Errorf("limit order not supported by this executor")
		}
		order, err = le.LimitOrder(ctx, in.Symbol, in.Side, quantity, in.LimitPrice, in.TimeInForce)
	} else {
		order, err = a.executor.MarketOrder(ctx, in.Symbol, in.Side, quantity)
	}
	if err != nil {
		entry := TradeLogEntry{
			Timestamp: now, Action: strings.ToUpper(string(in.Side)), Symbol: in.Symbol,
			Price: in.Price, Amount: quantity,
			Reason: in.Reason, Status: "error",
		}
		_ = a.log.Append(entry)
		a.logger.Error("auto trade order failed: %v", err)
		return out, err
	}
	out.Order = order

	// GMO は executions 照会の遅延で fill 価格 0 の filled を返すことがある。
	// 0 のまま ApplyBuy/Sell すると AvgPrice=0 の破壊ポジションになり、
	// 次 tick の TP 判定が巨大な見せかけ利益で誤発火する。参照価格（mid）で
	// 代替し、order_id と共に監査ログへ残す（価格不確かさは明示する）。
	if out.Order.Price <= 0 {
		if in.Price > 0 {
			a.logger.Warn("fill price unknown for %s %s order %s; falling back to reference price %.2f",
				in.Symbol, order.Side, order.OrderID, in.Price)
			out.Order.Price = in.Price
		} else {
			return out, fmt.Errorf("fill price unknown and no reference price for %s", in.Symbol)
		}
	}

	// 4. ポジションの更新。
	if order.Side == domain.TradeBuy {
		pos, err := a.positions.ApplyBuy(ctx, in.Symbol, order.Price, order.Quantity, order.Timestamp)
		if err != nil {
			return out, fmt.Errorf("apply buy: %w", err)
		}
		out.Position = pos
	} else {
		pnl, remaining, err := a.positions.ApplySell(ctx, in.Symbol, order.Price, order.Quantity, order.Timestamp)
		if err != nil {
			return out, fmt.Errorf("apply sell: %w", err)
		}
		out.Position = remaining
		// 手数料控除会計：報告損益は実費ベースにする（スリッページは含まない）。
		out.PnlUsdt = a.deductFees(pnl, order.Price, order.Quantity)
	}

	// 5. 日次損失の記録。
	stopped, reason := a.risk.RecordTrade(out.PnlUsdt)
	if stopped {
		out.Message = reason
	}

	// 6. 監査ログの追記。
	pnl := &out.PnlUsdt
	var entryPnl *float64
	if out.PnlUsdt != 0 || (in.Side == domain.TradeSell && out.Order.Status == "filled") {
		entryPnl = pnl
	}
	entry := TradeLogEntry{
		Timestamp: now,
		Action:    strings.ToUpper(string(order.Side)),
		Symbol:    in.Symbol,
		Price:     order.Price,
		Amount:    order.Quantity,
		Pnl:       entryPnl,
		Reason:    in.Reason,
		OrderID:   order.OrderID,
		Status:    order.Status,
	}
	if err := a.log.Append(entry); err != nil {
		a.logger.Warn("failed to append trade log: %v", err)
	}

	out.Status = "filled"
	if stopped {
		out.Status = "stopped"
	}
	posValue, _ := a.positions.MarketValue(ctx, in.Symbol, in.Price)
	out.Risk = a.risk.Status(posValue)
	return out, nil
}
