package domain

type TradeSide string

const (
	TradeBuy  TradeSide = "buy"
	TradeSell TradeSide = "sell"
)

type Trade struct {
	Symbol    string    `json:"symbol"`
	Side      TradeSide `json:"side"`
	Price     float64   `json:"price"`
	Volume    float64   `json:"volume"`
	Timestamp int64     `json:"timestamp"`
	OrderID   string    `json:"order_id,omitempty"`
	Status    string    `json:"status"`
}

// Execution は約定テープの1約定（テイカー側の売買）。
// 板スナップショットと違い「誰が食ったか」を表し、CVD・吸収検知の素になる。
type Execution struct {
	Symbol    string    `json:"symbol"`
	Side      TradeSide `json:"side"` // テイカーが buy（買い食い）か sell か
	Price     float64   `json:"price"`
	Qty       float64   `json:"qty"`
	Timestamp int64     `json:"timestamp"`
}
