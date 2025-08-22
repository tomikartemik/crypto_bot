package indicators

import (
	"math"

	"github.com/kuromii5/supertrend_trade_bot/internal/models"
)

func CalculateSupertrend(c []models.Candlestick, atr []float64, period int, multiplier float64) (float64, bool) {
	if len(c) < period {
		return 0, true
	}

	var fUpper, fLower float64
	isUp := true

	for i := 0; i < len(c); i++ {
		if i < period {
			continue
		}

		hl2 := (c[i].High + c[i].Low) / 2
		upper := hl2 + multiplier*atr[i]
		lower := hl2 - multiplier*atr[i]

		if i == period {
			fUpper = upper
			fLower = lower
			isUp = c[i].Close > fUpper
		} else {
			if isUp && c[i].Close <= fLower {
				isUp = false
			} else if !isUp && c[i].Close >= fUpper {
				isUp = true
			}

			if isUp {
				fLower = math.Max(lower, fLower)
				fUpper = upper
			} else {
				fUpper = math.Min(upper, fUpper)
				fLower = lower
			}
		}
	}

	val := fLower
	if !isUp {
		val = fUpper
	}

	return val, isUp
}
