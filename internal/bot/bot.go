package bot

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/kuromii5/supertrend_trade_bot/configs"
	"github.com/kuromii5/supertrend_trade_bot/internal/cache"
	"github.com/kuromii5/supertrend_trade_bot/internal/exchanger"
	"github.com/kuromii5/supertrend_trade_bot/internal/exchanger/okx"
	"github.com/kuromii5/supertrend_trade_bot/internal/log"
	"github.com/kuromii5/supertrend_trade_bot/internal/trader"
	"github.com/kuromii5/supertrend_trade_bot/internal/utils"
)

type Bot struct {
	client  exchanger.MarketProvider
	traders []*trader.Trader
}

func New() *Bot {
	config, err := configs.LoadBotConfig("bot_config.json")
	if err != nil {
		log.Log.Error("Ошибка чтения настроек бота", "error", err)
		os.Exit(1)
	}
	configs.BotCurrentConfig = config
	
	// Логируем MACD конфигурацию для диагностики
	log.Log.Info("MACD конфигурация", "MacdTimeframe", config.MacdTimeframe, "MacdFastPeriod", config.MacdFastPeriod, "MacdSlowPeriod", config.MacdSlowPeriod, "MacdSignalPeriod", config.MacdSignalPeriod)

	traderConfigs, err := configs.LoadConfigs("trader_configs.json")
	if err != nil {
		log.Log.Error("Ошибка чтения настроек API трейдеров", "error", err)
		os.Exit(1)
	}

	var traders []*trader.Trader
	for _, traderCfg := range traderConfigs {
		traders = append(traders, trader.NewTrader(traderCfg))
	}

	client := okx.NewBotClient()

	return &Bot{
		client:  client,
		traders: traders,
	}
}

func (b *Bot) Run(ctx context.Context) {
	log.Log.Info("=== Запуск бота ===")
	log.Log.Debug("Trading pairs", "pairs", configs.BotCurrentConfig.TradingPairs)

	if err := b.client.Subscribe(context.Background(), configs.BotCurrentConfig.TradingPairs); err != nil {
		log.Log.Error("Ошибка подписки на монеты", "err", err)
	} else {
		log.Log.Debug("Подписка на websocket успешна")
	}

	for _, inst := range configs.BotCurrentConfig.TradingPairs {
		log.Log.Debug("Запрос информации по инструменту", "instId", inst)

		instInfo, err := b.client.GetInstrumentInfo(inst)
		if err != nil {
			log.Log.Error("Не удалось получить MinSize / CtVal", "instId", inst, "err", err)
		} else {
			cache.Get().SetLotSize(inst, instInfo.MinSize)
			cache.Get().SetContractValue(inst, instInfo.CtVal)
			log.Log.Debug("Данные по инструменту", "instId", inst, "MinSize", instInfo.MinSize, "CtVal", instInfo.CtVal)
		}

		for _, trader := range b.traders {
			if err := trader.Client.SetLeverage(inst); err != nil {
				log.Log.Error("SetLeverage error", "instId", inst, "err", err)
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	interval, err := utils.ParseTimeframe(configs.BotCurrentConfig.Timeframes[0])
	if err != nil {
		log.Log.Error("failed to parse timeframe", "timeframe", configs.BotCurrentConfig.Timeframes[0], "err", err)
		os.Exit(1)
	}

	go b.strategyUpdater(ctx, interval, configs.BotCurrentConfig.TradingPairs, configs.BotCurrentConfig.CandlesAmount)

	for _, t := range b.traders {
		log.Log.Info("Запуск трейдера", "trader", fmt.Sprintf("%T", t))
		go t.Run(ctx, interval)
	}

	now := time.Now()
	nextInterval := now.Truncate(interval).Add(interval)
	initialDelay := nextInterval.Sub(now)

	log.Log.Debug("Ожидаем до следующей свечи", "delay", initialDelay, "now", now.Format(time.RFC3339), "next", nextInterval.Format(time.RFC3339))
	time.Sleep(initialDelay)

	log.Log.Debug("Старт бота завершён")
}

func (b *Bot) Stop() {
	for _, t := range b.traders {
		t.Stop()
	}
	log.Log.SaveToFile("bot.log")
}
