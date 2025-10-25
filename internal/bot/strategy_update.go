package bot

import (
	"context"
	"log"
	"math"
	"time"

	"github.com/kuromii5/supertrend_trade_bot/configs"
	"github.com/kuromii5/supertrend_trade_bot/internal/cache"
	"github.com/kuromii5/supertrend_trade_bot/internal/indicators"
	"github.com/kuromii5/supertrend_trade_bot/internal/models"
)

func (b *Bot) strategyUpdater(ctx context.Context, interval time.Duration, instruments []string, candlesAmount int) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	frames := configs.BotCurrentConfig.Timeframes
	if len(frames) == 0 {
		frames = []string{configs.BotCurrentConfig.EntryTimeframe}
		if configs.BotCurrentConfig.UseMTF && configs.BotCurrentConfig.TrendTimeframe != "" && configs.BotCurrentConfig.TrendTimeframe != configs.BotCurrentConfig.EntryTimeframe {
			frames = append(frames, configs.BotCurrentConfig.TrendTimeframe)
		}
	}

	update := func() {
		for _, instId := range instruments {
			for _, tf := range frames {
				candles, err := b.client.GetCandlesticks(instId, tf, candlesAmount)
				if err != nil || len(candles) == 0 {
					log.Printf("[strategyUpdater] Ошибка получения свечей %s %s: %v", instId, tf, err)
					continue
				}

				closes, highs, lows := extractSeries(candles)

				atr := indicators.CalculateATR(candles, configs.BotCurrentConfig.ATRPeriod)
				stSeries := indicators.CalculateSupertrendSeries(candles, atr, configs.BotCurrentConfig.ATRPeriod, configs.BotCurrentConfig.Multiplier)

				emaFast := indicators.CalculateEMA(closes, configs.BotCurrentConfig.EMAFast)
				emaSlow := indicators.CalculateEMA(closes, configs.BotCurrentConfig.EMASlow)
				rsi := indicators.CalculateRSI(closes, configs.BotCurrentConfig.RSIPeriod)
				adxComp := indicators.CalculateADX(highs, lows, closes, configs.BotCurrentConfig.ADXPeriod)

				macdPoints := indicators.CalculateMACD(closes, configs.BotCurrentConfig.MACDFast, configs.BotCurrentConfig.MACDSlow, configs.BotCurrentConfig.MACDSignal)
				macdHist := make([]float64, len(macdPoints))
				for i, p := range macdPoints {
					macdHist[i] = p.Histogram
				}
				smoothedHist := indicators.SmoothSeries(macdHist, configs.BotCurrentConfig.MACDSmoothBars)

				atrTrailing := indicators.CalculateATR(candles, configs.BotCurrentConfig.ATRPeriodTrailing)
				nbarLow, nbarHigh := calcNBarExtremes(candles, configs.BotCurrentConfig.TrailLookbackBars)

				lastIdx := len(candles) - 1
				stPoint := stSeries[lastIdx]
				lastCandle := candles[lastIdx]

				prevData, _ := cache.Get().GetIndicatorData(instId, tf)
				snapshot := cache.IndicatorData{
					Timestamp:   lastCandle.TsMillis,
					Close:       lastCandle.Close,
					Supertrend:  stPoint.Value,
					IsUptrend:   stPoint.IsUptrend,
					EMAFast:     safeIndex(emaFast, lastIdx),
					EMASlow:     safeIndex(emaSlow, lastIdx),
					RSI:         safeIndex(rsi, lastIdx),
					ADX:         safeIndex(adxComp.ADX, lastIdx),
					ATR:         safeIndex(atr, lastIdx),
					ATRTrailing: safeIndex(atrTrailing, lastIdx),
					MACD: cache.MACDData{
						Value:                 macdPoints[lastIdx].Value,
						Signal:                macdPoints[lastIdx].Signal,
						Histogram:             macdPoints[lastIdx].Histogram,
						SmoothedHistogram:     safeIndex(smoothedHist, lastIdx),
						PrevSmoothedHistogram: safeIndex(smoothedHist, lastIdx-1),
						PrevHistogram:         safeIndex(macdHist, lastIdx-1),
					},
					NBarLow:  nbarLow,
					NBarHigh: nbarHigh,
				}

				if prevData.Timestamp != 0 && prevData.IsUptrend != snapshot.IsUptrend {
					snapshot.SupertrendFlipped = true
				}

				cache.Get().SetIndicatorData(instId, tf, snapshot)
			}
		}
	}

	update()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			update()
		}
	}
}

func extractSeries(candles []models.Candlestick) (closes, highs, lows []float64) {
	closes = make([]float64, len(candles))
	highs = make([]float64, len(candles))
	lows = make([]float64, len(candles))
	for i, c := range candles {
		closes[i] = c.Close
		highs[i] = c.High
		lows[i] = c.Low
	}
	return closes, highs, lows
}

func safeIndex(values []float64, idx int) float64 {
	if idx < 0 || idx >= len(values) {
		return 0
	}
	return values[idx]
}

func calcNBarExtremes(candles []models.Candlestick, lookback int) (float64, float64) {
	if len(candles) == 0 {
		return 0, 0
	}
	if lookback <= 0 {
		lookback = 1
	}
	start := len(candles) - lookback
	if start < 0 {
		start = 0
	}
	low := math.MaxFloat64
	high := -math.MaxFloat64
	for i := start; i < len(candles); i++ {
		if candles[i].Low < low {
			low = candles[i].Low
		}
		if candles[i].High > high {
			high = candles[i].High
		}
	}
	return low, high
}
