package bot

import (
	"context"
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

				// Используем новую функцию с детектированием сигналов для мгновенного входа
				stResult, buySignal, sellSignal := indicators.CalculateSupertrendWithSignals(
					candles[configs.BotCurrentConfig.ATRPeriod-1:], 
					ATRs[configs.BotCurrentConfig.ATRPeriod-1:], 
					configs.BotCurrentConfig.ATRPeriod, 
					configs.BotCurrentConfig.Multiplier,
				)

				// Логируем сигналы смены тренда и уведомляем трейдеров
				if buySignal {
					log.Log.Info("🟢 BUY SIGNAL: Supertrend сменился на восходящий", 
						"inst", instId, "tf", tf, "value", stResult.Value)
					// Уведомляем всех трейдеров о смене тренда для мгновенного входа
					for _, trader := range b.traders {
						trader.NotifyTrendChange(instId)
					}
				}
				if sellSignal {
					log.Log.Info("🔴 SELL SIGNAL: Supertrend сменился на нисходящий", 
						"inst", instId, "tf", tf, "value", stResult.Value)
					// Уведомляем всех трейдеров о смене тренда для мгновенного входа
					for _, trader := range b.traders {
						trader.NotifyTrendChange(instId)
					}
				}

				cache.Get().SetIndicatorData(instId, tf, cache.IndicatorData{
					Supertrend: stResult.Value,
					ATR:        atr,
					IsUptrend:  stResult.IsUptrend,
				})
			}

			// MACD теперь рассчитывается в trader с правильным таймингом
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
