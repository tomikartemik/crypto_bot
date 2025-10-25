package indicators

import "math"

func CalculateRSI(values []float64, period int) []float64 {
	result := make([]float64, len(values))
	if period <= 0 || len(values) == 0 {
		return result
	}

	var avgGain, avgLoss float64
	for i := 1; i < len(values); i++ {
		change := values[i] - values[i-1]
		gain := math.Max(change, 0)
		loss := math.Max(-change, 0)

		if i <= period {
			avgGain += gain
			avgLoss += loss
			if i == period {
				avgGain /= float64(period)
				avgLoss /= float64(period)
				result[i] = computeRSI(avgGain, avgLoss)
			}
			continue
		}

		avgGain = (avgGain*(float64(period)-1) + gain) / float64(period)
		avgLoss = (avgLoss*(float64(period)-1) + loss) / float64(period)
		result[i] = computeRSI(avgGain, avgLoss)
	}

	return result
}

func computeRSI(avgGain, avgLoss float64) float64 {
	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}
