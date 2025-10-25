package indicators

import (
	"math"

	"github.com/kuromii5/supertrend_trade_bot/internal/models"
)

type SupertrendPoint struct {
	Value     float64
	IsUptrend bool
}

func CalculateSupertrendSeries(c []models.Candlestick, atr []float64, period int, multiplier float64) []SupertrendPoint {
	points := make([]SupertrendPoint, len(c))
	if len(c) == 0 || len(atr) != len(c) || period <= 0 {
		return points
	}

	var finalUpper, finalLower float64
	isUp := true

	for i := 0; i < len(c); i++ {
		if i < period {
			points[i] = SupertrendPoint{IsUptrend: true}
			continue
		}

		hl2 := (c[i].High + c[i].Low) / 2
		upper := hl2 + multiplier*atr[i]
		lower := hl2 - multiplier*atr[i]

		if i == period {
			finalUpper = upper
			finalLower = lower
			isUp = c[i].Close >= finalLower
		} else {
			if isUp && c[i].Close <= finalLower {
				isUp = false
			} else if !isUp && c[i].Close >= finalUpper {
				isUp = true
			}

			if isUp {
				finalLower = math.Max(lower, finalLower)
				finalUpper = upper
			} else {
				finalUpper = math.Min(upper, finalUpper)
				finalLower = lower
			}
		}

		val := finalLower
		if !isUp {
			val = finalUpper
		}

		points[i] = SupertrendPoint{Value: val, IsUptrend: isUp}
	}

	return points
}
