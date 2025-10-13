package indicators

import (
	"math"

	"github.com/kuromii5/supertrend_trade_bot/internal/models"
)

// MACDData содержит данные MACD индикатора
type MACDData struct {
	MACD    float64 // MACD линия
	Signal  float64 // Сигнальная линия
	Histogram float64 // Гистограмма (MACD - Signal)
}

// CalculateMACD рассчитывает MACD индикатор
// fastPeriod - период быстрой EMA (обычно 12)
// slowPeriod - период медленной EMA (обычно 26)
// signalPeriod - период сигнальной линии (обычно 9)
func CalculateMACD(candles []models.Candlestick, fastPeriod, slowPeriod, signalPeriod int) []MACDData {
	if len(candles) < slowPeriod {
		return make([]MACDData, len(candles))
	}

	// Извлекаем цены закрытия
	closes := make([]float64, len(candles))
	for i, candle := range candles {
		closes[i] = candle.Close
	}

	// Рассчитываем EMA
	fastEMA := calculateEMA(closes, fastPeriod)
	slowEMA := calculateEMA(closes, slowPeriod)

	// Рассчитываем MACD линию
	macdLine := make([]float64, len(closes))
	for i := range macdLine {
		if i >= slowPeriod-1 {
			macdLine[i] = fastEMA[i] - slowEMA[i]
		}
	}

	// Рассчитываем сигнальную линию (EMA от MACD)
	signalLine := calculateEMA(macdLine, signalPeriod)

	// Формируем результат
	result := make([]MACDData, len(candles))
	for i := range result {
		if i >= slowPeriod+signalPeriod-2 {
			result[i] = MACDData{
				MACD:     macdLine[i],
				Signal:   signalLine[i],
				Histogram: macdLine[i] - signalLine[i],
			}
		}
	}

	return result
}

// calculateEMA рассчитывает экспоненциальную скользящую среднюю
func calculateEMA(data []float64, period int) []float64 {
	if len(data) < period {
		return make([]float64, len(data))
	}

	ema := make([]float64, len(data))
	multiplier := 2.0 / (float64(period) + 1.0)

	// Первое значение EMA - это простая средняя
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += data[i]
	}
	ema[period-1] = sum / float64(period)

	// Рассчитываем остальные значения EMA
	for i := period; i < len(data); i++ {
		ema[i] = (data[i] * multiplier) + (ema[i-1] * (1 - multiplier))
	}

	return ema
}

// GetMACDSignal определяет сигнал MACD для торговли
// Возвращает: true если сигнал на покупку (MACD выше сигнальной линии), false если на продажу
func GetMACDSignal(macdData []MACDData) (bool, bool) {
	if len(macdData) < 2 {
		return false, false
	}

	// Берем последние два значения для определения тренда
	last := macdData[len(macdData)-1]
	prev := macdData[len(macdData)-2]

	// Проверяем, что у нас есть валидные данные
	if math.IsNaN(last.MACD) || math.IsNaN(last.Signal) || 
	   math.IsNaN(prev.MACD) || math.IsNaN(prev.Signal) {
		return false, false
	}

	// Сигнал на покупку: MACD пересекает сигнальную линию снизу вверх
	buySignal := prev.MACD <= prev.Signal && last.MACD > last.Signal
	
	// Сигнал на продажу: MACD пересекает сигнальную линию сверху вниз
	sellSignal := prev.MACD >= prev.Signal && last.MACD < last.Signal

	return buySignal, sellSignal
}
