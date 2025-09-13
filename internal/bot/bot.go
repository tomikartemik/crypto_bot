package bot

import (
	"context"
	"log"
	"time"

	"github.com/kuromii5/supertrend_trade_bot/configs"
	"github.com/kuromii5/supertrend_trade_bot/internal/cache"
	"github.com/kuromii5/supertrend_trade_bot/internal/exchanger"
	"github.com/kuromii5/supertrend_trade_bot/internal/exchanger/okx"
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
		log.Fatalf("failed to load bot config: %s", err)
	}
	configs.BotCurrentConfig = config

	traderConfigs, err := configs.LoadConfigs("trader_configs.json")
	if err != nil {
		log.Fatalf("failed to load api configs: %s", err)
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
	log.Println("=== Запуск бота ===")
	log.Printf("Trading pairs: %v", configs.BotCurrentConfig.TradingPairs)

	if err := b.client.Subscribe(ctx, configs.BotCurrentConfig.TradingPairs); err != nil {
		log.Printf("Ошибка подписки на монеты: %v", err)
	} else {
		log.Println("Подписка на websocket успешно")
	}

	for _, inst := range configs.BotCurrentConfig.TradingPairs {
		log.Printf("Запрос информации по инструменту %s", inst)
		instInfo, err := b.client.GetInstrumentInfo(inst)
		if err != nil {
			log.Printf("Не удалось получить info для %s: %v", inst, err)
		} else {
			cache.Get().SetLotSize(inst, instInfo.MinSize)
			cache.Get().SetContractValue(inst, instInfo.CtVal)
			log.Printf("Инструмент %s: MinSize=%f CtVal=%f", inst, instInfo.MinSize, instInfo.CtVal)
		}

		for _, trader := range b.traders {
			if err := trader.Client.SetLeverage(inst); err != nil {
				log.Printf("SetLeverage error: %s", err)
			}
		}
	}

	interval, err := utils.ParseTimeframe(configs.BotCurrentConfig.Timeframes[0])
	if err != nil {
		log.Fatalf("failed to parse timeframe %s: %v", configs.BotCurrentConfig.Timeframes[0], err)
	}

	log.Println("Запуск начального strategyUpdater (синхронно)")
	b.strategyUpdater(ctx, interval, configs.BotCurrentConfig.TradingPairs, configs.BotCurrentConfig.CandlesAmount)

	go b.strategyUpdater(ctx, interval, configs.BotCurrentConfig.TradingPairs, configs.BotCurrentConfig.CandlesAmount)

	for _, t := range b.traders {
		log.Printf("Запуск трейдера")
		go t.Run(ctx, interval)
	}

	now := time.Now()
	nextInterval := now.Truncate(interval).Add(interval)
	initialDelay := nextInterval.Sub(now)
	log.Printf("Ожидаем до следующей свечи: %v (now=%s next=%s)", initialDelay, now.Format(time.RFC3339), nextInterval.Format(time.RFC3339))
	time.Sleep(initialDelay)

	log.Println("Bot.Run: старт завершён")
}

func (b *Bot) Stop() {
	for _, t := range b.traders {
		t.Stop()
	}
}
