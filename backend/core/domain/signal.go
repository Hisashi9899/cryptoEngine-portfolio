package domain

type SignalType string

const (
	Buy  SignalType = "buy"
	Sell SignalType = "sell"
	Hold SignalType = "hold"
)

type Signal struct {
	Symbol      string     `json:"symbol"`
	Type        SignalType `json:"type"`
	Confidence  float64    `json:"confidence"`
	Reason      string     `json:"reason"`
	MidPrice    float64    `json:"mid_price"`
	SpreadBasis float64    `json:"spread_basis"`
	Timestamp   int64      `json:"timestamp"`
}
