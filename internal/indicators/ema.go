package indicators

func CalculateEMA(values []float64, period int) []float64 {
	result := make([]float64, len(values))
	if period <= 0 || len(values) == 0 {
		return result
	}

	multiplier := 2.0 / float64(period+1)
	result[0] = values[0]
	for i := 1; i < len(values); i++ {
		result[i] = (values[i]-result[i-1])*multiplier + result[i-1]
	}

	return result
}
