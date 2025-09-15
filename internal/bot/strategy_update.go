package bot

import (
	"context"
	"log"
	"time"

	"github.com/kuromii5/supertrend_trade_bot/configs"
	"github.com/kuromii5/supertrend_trade_bot/internal/cache"
	"github.com/kuromii5/supertrend_trade_bot/internal/indicators"
)

func (b *Bot) strategyUpdater(ctx context.Context, interval time.Duration, instruments []string, candlesAmount int) {
	update := func() {
		for _, instId := range instruments {
			for _, tf := range configs.BotCurrentConfig.Timeframes {
				candles, err := b.client.GetCandlesticks(instId, tf, candlesAmount)
				if err != nil {
					log.Printf("[strategyUpdater] Ошибка получения свечей %s %s: %v", instId, tf, err)
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
		}
	}

	update()

	// Выравниваемся к ближайшей границе интервала, чтобы последующие вызовы шли чётко по границам свечей
	now := time.Now()
	next := now.Truncate(interval).Add(interval)
	time.Sleep(next.Sub(now))

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			update()
		}
	}
}
