package indicators

import "math"

type ADXComponents struct {
	ADX     []float64
	PlusDI  []float64
	MinusDI []float64
}

func CalculateADX(highs, lows, closes []float64, period int) ADXComponents {
	length := len(closes)
	comp := ADXComponents{
		ADX:     make([]float64, length),
		PlusDI:  make([]float64, length),
		MinusDI: make([]float64, length),
	}

	if period <= 0 || length == 0 {
		return comp
	}

	var smoothedTR, smoothedPlusDM, smoothedMinusDM float64
	var dxValues []float64

	for i := 1; i < length; i++ {
		upMove := highs[i] - highs[i-1]
		downMove := lows[i-1] - lows[i]

		plusDM := 0.0
		if upMove > downMove && upMove > 0 {
			plusDM = upMove
		}

		minusDM := 0.0
		if downMove > upMove && downMove > 0 {
			minusDM = downMove
		}

		tr := trueRange(highs[i], lows[i], closes[i-1])

		if i <= period {
			smoothedTR += tr
			smoothedPlusDM += plusDM
			smoothedMinusDM += minusDM
			if i == period {
				comp.PlusDI[i] = 100 * (smoothedPlusDM / smoothedTR)
				comp.MinusDI[i] = 100 * (smoothedMinusDM / smoothedTR)
				dx := directionalIndex(comp.PlusDI[i], comp.MinusDI[i])
				dxValues = append(dxValues, dx)
				comp.ADX[i] = average(dxValues)
			}
			continue
		}

		smoothedTR = smoothedTR - smoothedTR/float64(period) + tr
		smoothedPlusDM = smoothedPlusDM - smoothedPlusDM/float64(period) + plusDM
		smoothedMinusDM = smoothedMinusDM - smoothedMinusDM/float64(period) + minusDM

		comp.PlusDI[i] = 100 * (smoothedPlusDM / smoothedTR)
		comp.MinusDI[i] = 100 * (smoothedMinusDM / smoothedTR)

		dx := directionalIndex(comp.PlusDI[i], comp.MinusDI[i])
		if len(dxValues) == 0 {
			dxValues = append(dxValues, dx)
			comp.ADX[i] = dx
			continue
		}

		previousADX := comp.ADX[i-1]
		comp.ADX[i] = (previousADX*(float64(period)-1) + dx) / float64(period)
	}

	return comp
}

func trueRange(high, low, prevClose float64) float64 {
	return math.Max(high-low, math.Max(math.Abs(high-prevClose), math.Abs(low-prevClose)))
}

func directionalIndex(plusDI, minusDI float64) float64 {
	sum := plusDI + minusDI
	if sum == 0 {
		return 0
	}
	return math.Abs(plusDI-minusDI) / sum * 100
}

func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}
