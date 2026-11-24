package domain

type Balance struct {
	Asset     string  `json:"asset"`
	Free      float64 `json:"free"`
	Locked    float64 `json:"locked"`
	Timestamp int64   `json:"timestamp"`
}
