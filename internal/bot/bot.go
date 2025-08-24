package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
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

		for _, trader := range b.traders {
			trader.Client.SetLeverage(inst)
		}
	}

	interval, err := parseTimeframe(configs.BotCurrentConfig.Timeframes[0])
	if err != nil {
		log.Fatalf("failed to parse timeframe %s: %v", configs.BotCurrentConfig.Timeframes[0], err)
	}

	now := time.Now()
	next5Min := now.Truncate(interval).Add(interval)
	initialDelay := next5Min.Sub(now)
	time.Sleep(initialDelay)

	go b.strategyUpdater(ctx, interval, configs.BotCurrentConfig.TradingPairs, configs.BotCurrentConfig.CandlesAmount)

	for _, t := range b.traders {
		go t.Run(ctx)
	}
}

func (b *Bot) Stop() {
	for _, t := range b.traders {
		t.Stop()
	}
}

func parseTimeframe(tf string) (time.Duration, error) {
	unit := tf[len(tf)-1]   // последняя буква (m или h)
	value := tf[:len(tf)-1] // всё кроме последней

	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}

	switch unit {
	case 'm', 'M':
		return time.Duration(n) * time.Minute, nil
	case 'h', 'H':
		return time.Duration(n) * time.Hour, nil
	default:
		return 0, fmt.Errorf("unsupported timeframe: %s", tf)
	}
}
