package bot

import (
	"context"
	"fmt"
	"time"

	"github.com/kuromii5/supertrend_trade_bot/configs"
	"github.com/kuromii5/supertrend_trade_bot/internal/cache"
	"github.com/kuromii5/supertrend_trade_bot/internal/indicators"
	"github.com/kuromii5/supertrend_trade_bot/internal/log"
)

func (b *Bot) strategyUpdater(ctx context.Context, interval time.Duration, instruments []string, candlesAmount int) {
	update := func() {
		for _, instId := range instruments {
			// Обновляем основные индикаторы для торгового таймфрейма
			for _, tf := range configs.BotCurrentConfig.Timeframes {
				candles, err := b.client.GetCandlesticks(instId, tf, candlesAmount)
				if err != nil {
					log.Log.Error("[strategyUpdater] Ошибка получения свечей", "inst", instId, "tf", tf, "error", err)
					continue
				}

				ATRs := indicators.CalculateATR(candles, configs.BotCurrentConfig.ATRPeriod)
				atr := ATRs[len(ATRs)-1]

				stValue, isUptrend := indicators.CalculateSupertrend(candles[configs.BotCurrentConfig.ATRPeriod-1:], ATRs[configs.BotCurrentConfig.ATRPeriod-1:], configs.BotCurrentConfig.ATRPeriod, configs.BotCurrentConfig.Multiplier)

				cache.Get().SetIndicatorData(instId, tf, cache.IndicatorData{
					Supertrend: stValue,
					ATR:        atr,
					IsUptrend:  isUptrend,
				})
			}

			// Обновляем MACD для MACD таймфрейма
			if configs.BotCurrentConfig.MacdTimeframe != "" {
				log.Log.Debug("[strategyUpdater] Начинаем расчет MACD", "inst", instId, "tf", configs.BotCurrentConfig.MacdTimeframe, "candlesAmount", candlesAmount)
				
				macdCandles, err := b.client.GetCandlesticks(instId, configs.BotCurrentConfig.MacdTimeframe, candlesAmount)
				if err != nil {
					log.Log.Error("[strategyUpdater] Ошибка получения свечей для MACD", "inst", instId, "tf", configs.BotCurrentConfig.MacdTimeframe, "error", err)
					continue
				}

				log.Log.Debug("[strategyUpdater] Получены свечи для MACD", "inst", instId, "candlesCount", len(macdCandles))

				// Рассчитываем MACD с параметрами из конфигурации
				macdData := indicators.CalculateMACD(macdCandles, configs.BotCurrentConfig.MacdFastPeriod, configs.BotCurrentConfig.MacdSlowPeriod, configs.BotCurrentConfig.MacdSignalPeriod)
				log.Log.Debug("[strategyUpdater] MACD рассчитан", "inst", instId, "macdDataLength", len(macdData))
				
				if len(macdData) > 0 {
					lastMacd := macdData[len(macdData)-1]
					buySignal, sellSignal := indicators.GetMACDSignal(macdData)

					cache.Get().SetMACDData(instId, cache.MACDIndicatorData{
						MACD:      lastMacd.MACD,
						Signal:    lastMacd.Signal,
						Histogram: lastMacd.Histogram,
						BuySignal:  buySignal,
						SellSignal: sellSignal,
					})

					log.Log.Info("[strategyUpdater] MACD данные сохранены в кэш", "inst", instId, "MACD", lastMacd.MACD, "Signal", lastMacd.Signal, "BuySignal", buySignal, "SellSignal", sellSignal)
					
					// Дополнительное логирование для отслеживания сигналов
					if buySignal {
						log.Log.Info(fmt.Sprintf("[strategyUpdater] MACD BUY сигнал для %s: MACD=%.6f Signal=%.6f", instId, lastMacd.MACD, lastMacd.Signal))
					}
					if sellSignal {
						log.Log.Info(fmt.Sprintf("[strategyUpdater] MACD SELL сигнал для %s: MACD=%.6f Signal=%.6f", instId, lastMacd.MACD, lastMacd.Signal))
					}
				} else {
					log.Log.Warn("[strategyUpdater] MACD данные пусты", "inst", instId, "candlesCount", len(macdCandles), "requiredMin", configs.BotCurrentConfig.MacdSlowPeriod+configs.BotCurrentConfig.MacdSignalPeriod)
				}
			} else {
				log.Log.Warn("[strategyUpdater] MacdTimeframe не установлен в конфигурации")
			}
		}
	}

	// Первичное обновление, чтобы были начальные данные
	update()

	// Выравниваемся к ближайшей границе интервала, чтобы последующие вызовы шли чётко по границам свечей
	now := time.Now()
	next := now.Truncate(interval).Add(interval)
	time.Sleep(next.Sub(now))

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Обновляем прямо на границе
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
