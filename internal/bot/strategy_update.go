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

				stValue, isUptrend := indicators.CalculateSupertrend(candles[configs.BotCurrentConfig.ATRPeriod-1:], ATRs[configs.BotCurrentConfig.ATRPeriod-1:], configs.BotCurrentConfig.ATRPeriod, configs.BotCurrentConfig.Multiplier)

				cache.Get().SetIndicatorData(instId, tf, cache.IndicatorData{
					Supertrend: stValue,
					ATR:        atr,
					IsUptrend:  isUptrend,
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
