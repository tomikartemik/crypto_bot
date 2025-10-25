package indicators

import "math"

type MACDPoint struct {
	Value     float64
	Signal    float64
	Histogram float64
}

func CalculateMACD(values []float64, fast, slow, signal int) []MACDPoint {
	length := len(values)
	points := make([]MACDPoint, length)
	if length == 0 || fast <= 0 || slow <= 0 || signal <= 0 {
		return points
	}

	fastEMA := CalculateEMA(values, fast)
	slowEMA := CalculateEMA(values, slow)

	macdLine := make([]float64, length)
	for i := range values {
		macdLine[i] = fastEMA[i] - slowEMA[i]
	}

	signalLine := CalculateEMA(macdLine, signal)

	for i := range values {
		points[i] = MACDPoint{
			Value:     macdLine[i],
			Signal:    signalLine[i],
			Histogram: macdLine[i] - signalLine[i],
		}
	}

	return points
}

func SmoothSeries(values []float64, length int) []float64 {
	result := make([]float64, len(values))
	if length <= 1 {
		copy(result, values)
		return result
	}

	window := make([]float64, 0, length)
	sum := 0.0
	for i, v := range values {
		window = append(window, v)
		sum += v
		if len(window) > length {
			sum -= window[0]
			window = window[1:]
		}
		result[i] = sum / math.Max(float64(len(window)), 1)
	}

	return result
}
