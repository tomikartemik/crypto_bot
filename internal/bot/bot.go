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
	// websocket
	if err := b.client.Subscribe(ctx, configs.BotCurrentConfig.TradingPairs); err != nil {
		log.Printf("Ошибка подписки на монеты: %v", err)
	}

	for _, inst := range configs.BotCurrentConfig.TradingPairs {
		lot, err := b.client.GetLotSize(inst)
		if err == nil {
			cache.Get().SetLotSize(inst, lot)
		}

		ctVal, err := b.client.GetContractValue(inst)
		if err == nil {
			cache.Get().SetContractValue(inst, ctVal)
		}
	}

	now := time.Now()
	next5Min := now.Truncate(15 * time.Minute).Add(15 * time.Minute)
	initialDelay := next5Min.Sub(now)
	time.Sleep(initialDelay)

	go b.strategyUpdater(ctx, 5*time.Minute, configs.BotCurrentConfig.TradingPairs, configs.BotCurrentConfig.CandlesAmount)

	for _, t := range b.traders {
		go t.Run(ctx)
	}
}

func (b *Bot) Stop() {
	for _, t := range b.traders {
		t.Stop()
	}
}
