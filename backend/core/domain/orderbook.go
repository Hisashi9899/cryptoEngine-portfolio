package domain

type Order struct {
	Price  float64 `json:"price"`
	Volume float64 `json:"volume"`
}

type OrderBook struct {
	Symbol    string  `json:"symbol"`
	Bids      []Order `json:"bids"`
	Asks      []Order `json:"asks"`
	Timestamp int64   `json:"timestamp"`
	MidPrice  float64 `json:"mid_price"`
	Spread    float64 `json:"spread"`
}

type Ticker struct {
	Symbol string  `json:"symbol"`
	Last   float64 `json:"last"`
	Bid    float64 `json:"bid"`
	Ask    float64 `json:"ask"`
	Volume float64 `json:"volume"`
	// High/Low は24時間の高値・安値（GMO /v1/ticker 等）。
	// 未対応の取引所では 0 のままになり、日次バイアスは不明扱い。
	High float64 `json:"high"`
	Low  float64 `json:"low"`
}
