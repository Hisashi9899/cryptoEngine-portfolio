package domain

type Position struct {
	Symbol            string    `json:"symbol"`
	Side              TradeSide `json:"side"`
	Volume            float64   `json:"volume"`
	AvgPrice          float64   `json:"avg_price"`
	LastPrice         float64   `json:"last_price"`
	MarketValue       float64   `json:"market_value"`
	UnrealizedPnl     float64   `json:"unrealized_pnl"`
	UnrealizedPnlRate float64   `json:"unrealized_pnl_rate"`
	Timestamp         int64     `json:"timestamp"`
}
