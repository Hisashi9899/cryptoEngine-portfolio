package vt

// candle.go はロウソク足の形状判定を提供する。
//
// すべて相対値（全長に対する割合）で定義し、銘柄・価格帯に依存しない。
// 同形でも高値圏と安値圏で意味が反転するもの（カラカサ／首吊り線）は
// 位置条件を呼び出し側（トレンドフィルタ）が付ける。

// Body は実体の長さを返す。
func Body(b Bar) float64 {
	if b.C >= b.O {
		return b.C - b.O
	}
	return b.O - b.C
}

// Range は全長を返す。
func Range(b Bar) float64 {
	if b.H <= b.L {
		return 0
	}
	return b.H - b.L
}

// UpperWick は上ヒゲの長さを返す。
func UpperWick(b Bar) float64 {
	top := b.C
	if b.O > top {
		top = b.O
	}
	if b.H <= top {
		return 0
	}
	return b.H - top
}

// LowerWick は下ヒゲの長さを返す。
func LowerWick(b Bar) float64 {
	bot := b.C
	if b.O < bot {
		bot = b.O
	}
	if b.L >= bot {
		return 0
	}
	return bot - b.L
}

// IsBullish は陽線かどうかを返す。
func IsBullish(b Bar) bool { return b.C > b.O }

// IsBearish は陰線かどうかを返す。
func IsBearish(b Bar) bool { return b.C < b.O }

// IsDoji は十字線かどうかを返す（実体が全長の10%以下）。
func IsDoji(b Bar) bool {
	r := Range(b)
	if r <= 0 {
		return false
	}
	return Body(b) <= 0.10*r
}

// IsHammer はカラカサ形かどうかを返す。
// 下ヒゲが実体の2倍以上・上ヒゲが実体の0.3倍以下。高安の判定は呼び出し側。
func IsHammer(b Bar) bool {
	body := Body(b)
	if body <= 0 {
		return false
	}
	return LowerWick(b) >= 2*body && UpperWick(b) <= 0.3*body
}

// IsShootingStar は流星形かどうかを返す（カラカサの上下反転）。
func IsShootingStar(b Bar) bool {
	body := Body(b)
	if body <= 0 {
		return false
	}
	return UpperWick(b) >= 2*body && LowerWick(b) <= 0.3*body
}

// IsBullishEngulfing は陽の包み線かどうかを返す。
func IsBullishEngulfing(prev, cur Bar) bool {
	if !IsBearish(prev) || !IsBullish(cur) {
		return false
	}
	return cur.O <= prev.C && cur.C >= prev.O
}

// IsBearishEngulfing は陰の包み線かどうかを返す。
func IsBearishEngulfing(prev, cur Bar) bool {
	if !IsBullish(prev) || !IsBearish(cur) {
		return false
	}
	return cur.O >= prev.C && cur.C <= prev.O
}

// IsThreeSoldiers は赤三兵かどうかを返す。
// 3連続陽線・各実体が全長の50%超・上ヒゲ短め。
func IsThreeSoldiers(bars []Bar) bool {
	if len(bars) < 3 {
		return false
	}
	last := bars[len(bars)-3:]
	prevC := 0.0
	for i, b := range last {
		if !IsBullish(b) {
			return false
		}
		if Body(b) < 0.5*Range(b) {
			return false
		}
		if UpperWick(b) > 0.3*Range(b) {
			return false
		}
		if i > 0 && b.C <= prevC {
			return false
		}
		prevC = b.C
	}
	return true
}

// IsThreeCrows は黒三兵かどうかを返す（赤三兵の上下反転）。
func IsThreeCrows(bars []Bar) bool {
	if len(bars) < 3 {
		return false
	}
	last := bars[len(bars)-3:]
	prevC := 0.0
	first := true
	for _, b := range last {
		if !IsBearish(b) {
			return false
		}
		if Body(b) < 0.5*Range(b) {
			return false
		}
		if LowerWick(b) > 0.3*Range(b) {
			return false
		}
		if !first && b.C >= prevC {
			return false
		}
		prevC = b.C
		first = false
	}
	return true
}

// IsMorningStar は明けの明星かどうかを返す。
// 大陰線→小保ち合い→大陽線で、3本目が1本目の実体の50%超まで戻す。
func IsMorningStar(bars []Bar) bool {
	if len(bars) < 3 {
		return false
	}
	a, b, c := bars[len(bars)-3], bars[len(bars)-2], bars[len(bars)-1]
	if !IsBearish(a) || Body(a) < 0.5*Range(a) {
		return false
	}
	if Body(b) > 0.3*Range(b) {
		return false
	}
	if !IsBullish(c) || Body(c) < 0.5*Range(c) {
		return false
	}
	mid := (a.O + a.C) / 2
	return c.C >= mid
}

// IsEveningStar は宵の明星かどうかを返す（明けの明星の上下反転）。
func IsEveningStar(bars []Bar) bool {
	if len(bars) < 3 {
		return false
	}
	a, b, c := bars[len(bars)-3], bars[len(bars)-2], bars[len(bars)-1]
	if !IsBullish(a) || Body(a) < 0.5*Range(a) {
		return false
	}
	if Body(b) > 0.3*Range(b) {
		return false
	}
	if !IsBearish(c) || Body(c) < 0.5*Range(c) {
		return false
	}
	mid := (a.O + a.C) / 2
	return c.C <= mid
}
