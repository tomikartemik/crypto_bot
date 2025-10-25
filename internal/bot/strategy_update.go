package bot

import (
	"context"
	"time"

	"github.com/tomikartemik/crypto_bot/configs"
	"github.com/tomikartemik/crypto_bot/internal/cache"
	"github.com/tomikartemik/crypto_bot/internal/indicators"
	"github.com/tomikartemik/crypto_bot/internal/log"
)

func (b *Bot) strategyUpdater(ctx context.Context, interval time.Duration, instruments []string, candlesAmount int) {
	update := func() {
		for _, instId := range instruments {
			for _, tf := range configs.BotCurrentConfig.Timeframes {
				candles, err := b.client.GetCandlesticks(instId, tf, candlesAmount)
				if err != nil {
					log.Log.Error("[strategyUpdater] Ошибка получения свечей", "inst", instId, "tf", tf, "error", err)
					continue
				}

				period := resolveATRPeriodForTF(tf)
				if period <= 0 {
					log.Log.Warn("ATR period не задан, используем значение по умолчанию 10", "tf", tf)
					period = 10
				}

				if len(candles) < period {
					log.Log.Warn("Недостаточно свечей для расчёта по выбранному периоду", "tf", tf, "period", period, "have", len(candles))
					continue
				}

				ATRs := indicators.CalculateATR(candles, period)
				if len(ATRs) < period {
					log.Log.Warn("Недостаточно ATR данных для расчёта", "tf", tf, "period", period, "have", len(ATRs))
					continue
				}
				atr := ATRs[len(ATRs)-1]

				multiplier := resolveMultiplierForTF(tf)

				// Используем новую функцию с детектированием сигналов
				stResult, buySignal, sellSignal := indicators.CalculateSupertrendWithSignals(
					candles[period-1:],
					ATRs[period-1:],
					period,
					multiplier,
				)

				// Логируем сигналы смены тренда и уведомляем трейдеров
				if buySignal {
					log.Log.Info("🟢 BUY SIGNAL: Supertrend сменился на восходящий",
						"inst", instId, "tf", tf, "value", stResult.Value)
					// Уведомляем всех трейдеров о смене тренда
					for _, trader := range b.traders {
						trader.NotifyTrendChange(instId)
					}
				}
				if sellSignal {
					log.Log.Info("🔴 SELL SIGNAL: Supertrend сменился на нисходящий",
						"inst", instId, "tf", tf, "value", stResult.Value)
					// Уведомляем всех трейдеров о смене тренда
					for _, trader := range b.traders {
						trader.NotifyTrendChange(instId)
					}
				}

				cache.Get().SetIndicatorData(instId, tf, cache.IndicatorData{
					Supertrend: stResult.Value,
					ATR:        atr,
					IsUptrend:  stResult.IsUptrend,
					Trend:      stResult.Trend,
				})
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

func resolveMultiplierForTF(tf string) float64 {
	cfg := configs.BotCurrentConfig
	if cfg.TimeframeSettings != nil {
		if setting, ok := cfg.TimeframeSettings[tf]; ok && setting.Multiplier > 0 {
			return setting.Multiplier
		}
	}
	return 3.0
}

func resolveATRPeriodForTF(tf string) int {
	cfg := configs.BotCurrentConfig
	if cfg.TimeframeSettings != nil {
		if setting, ok := cfg.TimeframeSettings[tf]; ok && setting.ATRPeriod > 0 {
			return setting.ATRPeriod
		}
	}
	return 10
}
