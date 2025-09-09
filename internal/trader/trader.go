package trader

import (
	"context"
	"log"
	"time"

	"github.com/kuromii5/supertrend_trade_bot/configs"
	"github.com/kuromii5/supertrend_trade_bot/internal/cache"
	"github.com/kuromii5/supertrend_trade_bot/internal/exchanger"
	"github.com/kuromii5/supertrend_trade_bot/internal/exchanger/okx"
	"github.com/kuromii5/supertrend_trade_bot/internal/models"
)

type Trader struct {
	cfg                configs.TraderConfig
	Client             exchanger.TradingAccount
	trailingActivated  map[string]bool
	trailingStopPrices map[string]float64
	extremePrices      map[string]float64
	lastIsUptrend      map[string]*bool
	isPositionOpen     map[string]bool
	waitingTrendChange map[string]bool
	positions          map[string]models.Position
}

func NewTrader(cfg configs.TraderConfig) *Trader {
	client := okx.NewClient(cfg.APIKey, cfg.APISecret, cfg.Passphrase)

	return &Trader{
		cfg:                cfg,
		Client:             client,
		trailingActivated:  make(map[string]bool),
		trailingStopPrices: make(map[string]float64),
		extremePrices:      make(map[string]float64),
		lastIsUptrend:      make(map[string]*bool),
		isPositionOpen:     make(map[string]bool),
		waitingTrendChange: make(map[string]bool),
		positions:          make(map[string]models.Position),
	}
}

func (t *Trader) Run(ctx context.Context, interval time.Duration) {
	tickerTrade := time.NewTicker(interval)
	defer tickerTrade.Stop()

	tickerTrailing := time.NewTicker(10 * time.Second)
	defer tickerTrailing.Stop()

	for _, instId := range configs.BotCurrentConfig.TradingPairs {
		t.waitingTrendChange[instId] = true
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-tickerTrade.C:
			t.trade()
		case <-tickerTrailing.C:
			t.monitorTrailingStop()
		}
	}
}

func (t *Trader) trade() {
	for _, instId := range configs.BotCurrentConfig.TradingPairs {
		data, ok := cache.Get().GetIndicatorData(instId, configs.BotCurrentConfig.Timeframes[0])
		if !ok {
			log.Printf("[Trader %s][%s] Нет данных в кэше по монете", t.cfg.APIKey, instId)
			continue
		}

		tfIsUptrend := data.IsUptrend
		price, ok := cache.Get().GetPrice(instId)
		if !ok {
			log.Printf("[Trader %s][%s] Нет актуальной цены в кэше — пропуск шага", t.cfg.APIKey, instId)
			continue
		}

		// Инициализация при первом запуске
		if t.lastIsUptrend[instId] == nil {
			t.lastIsUptrend[instId] = new(bool)
			*t.lastIsUptrend[instId] = tfIsUptrend
			continue
		}

		// Обнаружение смены тренда
		trendChanged := tfIsUptrend != *t.lastIsUptrend[instId]

		// Если тренд изменился и у нас есть открытая позиция - закрываем её
		if trendChanged && t.isPositionOpen[instId] {
			log.Printf("[Trader %s][%s] Обнаружена смена тренда, закрываем позицию", t.cfg.APIKey, instId)
			t.closePosition(instId)
		}

		// Если тренд изменился (независимо от наличия позиции) - открываем новую позицию
		if trendChanged {
			log.Printf("[Trader %s][%s] Открываем позицию по новому тренду", t.cfg.APIKey, instId)

			tradeSize, err := t.Client.GetTradeSize(instId, "USDT", configs.BotCurrentConfig.RiskPercent, price)
			if err != nil {
				log.Printf("[Trader %s][%s] Не удалось получить tradeSize: %s", t.cfg.APIKey, instId, err)
				continue
			}

			if tfIsUptrend {
				if err := t.Client.PlaceOrder(instId, "buy", "long", tradeSize); err != nil {
					log.Printf("[Trader %s][%s] Ошибка при открытии LONG: %v", t.cfg.APIKey, instId, err)
				} else {
					t.trailingActivated[instId] = false
					t.extremePrices[instId] = price
					stopLossPrice := t.calculateStopLoss(instId, price, true)
					t.positions[instId] = models.Position{
						InstId:        instId,
						PosSide:       "long",
						TradeSize:     tradeSize,
						EntryPrice:    price,
						StopLossPrice: stopLossPrice,
					}
					t.isPositionOpen[instId] = true
					log.Printf("[Trader %s][%s] Открыт LONG", t.cfg.APIKey, instId)
				}
			} else {
				if err := t.Client.PlaceOrder(instId, "sell", "short", tradeSize); err != nil {
					log.Printf("[Trader %s][%s] Ошибка при открытии SHORT: %v", t.cfg.APIKey, instId, err)
				} else {
					t.trailingActivated[instId] = false
					t.extremePrices[instId] = price
					stopLossPrice := t.calculateStopLoss(instId, price, false)
					t.positions[instId] = models.Position{
						InstId:        instId,
						PosSide:       "short",
						TradeSize:     tradeSize,
						EntryPrice:    price,
						StopLossPrice: stopLossPrice,
					}
					t.isPositionOpen[instId] = true
					log.Printf("[Trader %s][%s] Открыт SHORT", t.cfg.APIKey, instId)
				}
			}
		}

		// Обновляем последнее известное направление тренда
		*t.lastIsUptrend[instId] = tfIsUptrend
	}
}

func (t *Trader) monitorTrailingStop() {
	for _, instId := range configs.BotCurrentConfig.TradingPairs {
		if !t.isPositionOpen[instId] {
			continue
		}

		price, ok := cache.Get().GetPrice(instId)
		if !ok {
			log.Printf("Нет данных по монете в кэше: %s", instId)
			continue
		}

		// Берем ATR с нужного таймфрейма
		data, ok := cache.Get().GetIndicatorData(instId, configs.BotCurrentConfig.Timeframes[0])
		if !ok || data.ATR == 0 {
			continue
		}
		atr := data.ATR

		dir := 1.0 // 1 = LONG, -1 = SHORT
		if t.positions[instId].PosSide == "short" {
			dir = -1
		}

		// Проверка активации трейлинга
		if !t.trailingActivated[instId] && dir*(price-t.positions[instId].EntryPrice) > atr*1.2 {
			t.trailingActivated[instId] = true
			t.extremePrices[instId] = price
			t.trailingStopPrices[instId] = price - dir*atr*configs.BotCurrentConfig.ATRMultiplierTrailing
			log.Printf("[Trader %s][%s] Активация трейлинг-стопа (%s): Entry=%.4f, Price=%.4f (Δ=%.4f, %.2f%%), ATR=%.4f, TrailingStop=%.4f", t.cfg.APIKey, instId, t.positions[instId].PosSide, t.positions[instId].EntryPrice, price, price-t.positions[instId].EntryPrice, 100*(price-t.positions[instId].EntryPrice)/t.positions[instId].EntryPrice, atr, t.trailingStopPrices[instId])
		}

		if t.trailingActivated[instId] {
			oldExtreme := t.extremePrices[instId]
			oldTrailing := t.trailingStopPrices[instId]
			if dir*(price-t.extremePrices[instId]) > 0 {
				t.extremePrices[instId] = price
				t.trailingStopPrices[instId] = t.extremePrices[instId] - dir*atr*configs.BotCurrentConfig.ATRMultiplierTrailing
				log.Printf("[Trader %s][%s] Подвинут трейлинг (%s): Price=%.4f, Extreme=%.4f→%.4f, Stop=%.4f→%.4f (ΔStop=%.4f)", t.cfg.APIKey, instId, t.positions[instId].PosSide, price, oldExtreme, t.extremePrices[instId], oldTrailing, t.trailingStopPrices[instId], t.trailingStopPrices[instId]-oldTrailing)
			}

			// Закрытие по трейлинг-стопу
			if dir*(price-t.trailingStopPrices[instId]) <= 0 {
				log.Printf("[Trader %s][%s] Закрыт %s по трейлинг-стопу: Entry=%.4f, Stop=%.4f, Price=%.4f, PnL=%.2f%%", t.cfg.APIKey, instId, t.positions[instId].PosSide, t.positions[instId].EntryPrice, t.trailingStopPrices[instId], price, 100*(price-t.positions[instId].EntryPrice)/t.positions[instId].EntryPrice*dir)
				t.closePosition(instId)
				continue
			}
		}

		// Стоп-лосс
		if dir*(price-t.positions[instId].EntryPrice) <= -atr*configs.BotCurrentConfig.ATRMultiplierStopLoss {
			log.Printf("[Trader %s][%s] Сработал стоп-лосс %s: Entry=%.4f, Stop=%.4f, Price=%.4f, PnL=%.2f%%", t.cfg.APIKey, instId, t.positions[instId].PosSide, t.positions[instId].EntryPrice, t.positions[instId].StopLossPrice, price, 100*(price-t.positions[instId].EntryPrice)/t.positions[instId].EntryPrice*dir)
			t.closePosition(instId)
		}
	}
}

func (t *Trader) calculateStopLoss(instId string, price float64, isUptrend bool) float64 {
	data, ok := cache.Get().GetIndicatorData(instId, configs.BotCurrentConfig.Timeframes[0])
	if !ok || data.ATR == 0 {
		// Fallback к фиксированным процентам если ATR недоступен
		if isUptrend {
			return price * 0.992
		}
		return price * 1.008
	}

	stopLossDistance := data.ATR * configs.BotCurrentConfig.ATRMultiplierStopLoss
	if isUptrend {
		return price - stopLossDistance
	}
	return price + stopLossDistance
}

func (t *Trader) closePosition(instId string) {
	if !t.isPositionOpen[instId] {
		return
	}

	side := "sell"
	if t.positions[instId].PosSide == "short" {
		side = "buy"
	}

	if err := t.Client.PlaceOrder(t.positions[instId].InstId, side, t.positions[instId].PosSide, t.positions[instId].TradeSize); err == nil {
		log.Printf("[Trader %s][%s] Позиция закрыта", t.cfg.APIKey, instId)
		t.isPositionOpen[instId] = false
		t.trailingActivated[instId] = false
		delete(t.positions, instId)
	} else {
		log.Printf("[Trader %s][%s] Ошибка при закрытии позиции: %v", t.cfg.APIKey, instId, err)
	}
}

func (t *Trader) Stop() {
	for _, instId := range configs.BotCurrentConfig.TradingPairs {
		log.Printf("[Trader %s][%s] Закрытие позиции", t.cfg.APIKey, instId)
		t.closePosition(instId)
	}
}
