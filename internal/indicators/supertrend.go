package indicators

import (
	"math"

	"github.com/kuromii5/supertrend_trade_bot/internal/models"
)

// SupertrendResult содержит результат расчета Supertrend
type SupertrendResult struct {
	Value     float64
	IsUptrend bool
	Trend     int // 1 для восходящего тренда, -1 для нисходящего
}

func CalculateSupertrend(c []models.Candlestick, atr []float64, period int, multiplier float64) (float64, bool) {
	result := CalculateSupertrendDetailed(c, atr, period, multiplier)
	return result.Value, result.IsUptrend
}

// CalculateSupertrendDetailed возвращает детальную информацию о Supertrend согласно TradingView
func CalculateSupertrendDetailed(c []models.Candlestick, atr []float64, period int, multiplier float64) SupertrendResult {
	if len(c) < period {
		return SupertrendResult{Value: 0, IsUptrend: true, Trend: 1}
	}

	// Инициализация согласно TradingView логике
	hl2 := (c[0].High + c[0].Low) / 2
	up := hl2 - multiplier*atr[0]  // TradingView: up = src - (Multiplier * atr)
	dn := hl2 + multiplier*atr[0] // TradingView: dn = src + (Multiplier * atr)
	
	// Первое значение тренда
	trend := 1
	if c[0].Close > dn {
		trend = 1
	} else {
		trend = -1
	}

	// Расчет для всех свечей согласно TradingView логике
	for i := 1; i < len(c); i++ {
		hl2 := (c[i].High + c[i].Low) / 2
		upper := hl2 - multiplier*atr[i]
		lower := hl2 + multiplier*atr[i]
		
		// Обновление up линии согласно TradingView логике
		up1 := up
		if c[i-1].Close > up1 {
			up = math.Max(upper, up1)
		} else {
			up = upper
		}
		
		// Обновление dn линии согласно TradingView логике
		dn1 := dn
		if c[i-1].Close < dn1 {
			dn = math.Min(lower, dn1)
		} else {
			dn = lower
		}
		
		// Определение тренда согласно TradingView логике
		if trend == -1 && c[i].Close > dn1 {
			trend = 1
		} else if trend == 1 && c[i].Close < up1 {
			trend = -1
		}
	}

	// Определение финального значения
	var value float64
	var isUptrend bool
	
	if trend == 1 {
		value = up
		isUptrend = true
	} else {
		value = dn
		isUptrend = false
	}

	return SupertrendResult{
		Value:     value,
		IsUptrend: isUptrend,
		Trend:     trend,
	}
}

// CalculateSupertrendWithSignals рассчитывает Supertrend и определяет сигналы смены тренда
func CalculateSupertrendWithSignals(c []models.Candlestick, atr []float64, period int, multiplier float64) (SupertrendResult, bool, bool) {
	if len(c) < period+1 {
		result := CalculateSupertrendDetailed(c, atr, period, multiplier)
		return result, false, false
	}

	// Получаем предыдущий результат для сравнения
	prevCandles := c[:len(c)-1]
	prevResult := CalculateSupertrendDetailed(prevCandles, atr[:len(atr)-1], period, multiplier)
	
	// Получаем текущий результат
	currentResult := CalculateSupertrendDetailed(c, atr, period, multiplier)
	
	// Определяем сигналы согласно TradingView логике
	buySignal := currentResult.Trend == 1 && prevResult.Trend == -1
	sellSignal := currentResult.Trend == -1 && prevResult.Trend == 1
	
	return currentResult, buySignal, sellSignal
}
