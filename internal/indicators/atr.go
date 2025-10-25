package indicators

import (
	"math"

	"github.com/kuromii5/supertrend_trade_bot/internal/models"
)

func CalculateATR(c []models.Candlestick, period int) []float64 {
	atr := make([]float64, len(c))
	atr[0] = c[0].High - c[0].Low
	for i := 1; i < len(c); i++ {
		tr := math.Max(c[i].High-c[i].Low, math.Max(math.Abs(c[i].High-c[i-1].Close), math.Abs(c[i].Low-c[i-1].Close)))
		if i == period {
			sum := 0.0
			for j := i - period + 1; j <= i; j++ {
				trj := math.Max(c[j].High-c[j].Low, math.Max(math.Abs(c[j].High-c[j-1].Close), math.Abs(c[j].Low-c[j-1].Close)))
				sum += trj
			}
			atr[i] = sum / float64(period)
		} else if i > period {
			atr[i] = (atr[i-1]*(float64(period)-1) + tr) / float64(period)
		}
	}
	return atr
}
