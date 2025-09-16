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
	actedOnTrend       map[string]*bool
	isPositionOpen     map[string]bool
	waitingTrendChange map[string]bool
	positions          map[string]models.Position

	updateCh        chan string
	lastIndicatorAt map[string]time.Time
	trendChangeCounter map[string]int
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
		actedOnTrend:       make(map[string]*bool),
		isPositionOpen:     make(map[string]bool),
		waitingTrendChange: make(map[string]bool),
		positions:          make(map[string]models.Position),
		updateCh:           make(chan string, 128),
		lastIndicatorAt:    make(map[string]time.Time),
		trendChangeCounter: make(map[string]int),
	}
}

func (t *Trader) NotifyUpdate(instId string) {
	select {
	case t.updateCh <- instId:
	default:
	}
}

func (t *Trader) Run(ctx context.Context, interval time.Duration) {
	log.Printf("[Trader %s] Run started (interval=%s)", t.cfg.APIKey, interval)

	for _, instId := range configs.BotCurrentConfig.TradingPairs {
		t.waitingTrendChange[instId] = true

		if data, ok := cache.Get().GetIndicatorData(instId, configs.BotCurrentConfig.Timeframes[0]); ok {
			b := data.IsUptrend
			t.lastIsUptrend[instId] = new(bool)
			*t.lastIsUptrend[instId] = b
			log.Printf("[Trader %s][%s] Инициализация lastIsUptrend=%v", t.cfg.APIKey, instId, b)
		} else {
			t.lastIsUptrend[instId] = nil
			log.Printf("[Trader %s][%s] lastIsUptrend оставляем nil (нет данных в кэше)", t.cfg.APIKey, instId)
		}
		t.actedOnTrend[instId] = nil
	}

	log.Printf("[Trader %s] Выполнение initial trade() для всех пар", t.cfg.APIKey)
	// Первичный проход (может не открыть сделок, если индикаторы ещё не посчитаны)
	t.trade()

	// Выравниваемся по границе, чтобы следующее действие было ровно в 15:00/15/30/45
	now := time.Now()
	next := now.Truncate(interval).Add(interval)
	time.Sleep(next.Sub(now))

	// Сразу обрабатываем сигнал на границе свечи
	t.trade()

	tickerTrade := time.NewTicker(interval)
	defer tickerTrade.Stop()

	tickerTrailing := time.NewTicker(10 * time.Second)
	defer tickerTrailing.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[Trader %s] context done, exiting Run", t.cfg.APIKey)
			return
		case inst := <-t.updateCh:
			log.Printf("[Trader %s][%s] NotifyUpdate received — немедленная обработка", t.cfg.APIKey, inst)
			t.tradeFor(inst)
		case <-tickerTrade.C:
			t.trade()
		case <-tickerTrailing.C:
			t.monitorTrailingStop()
		}
	}
}

func (t *Trader) trade() {
	for _, instId := range configs.BotCurrentConfig.TradingPairs {
		t.tradeFor(instId)
	}
}

func (t *Trader) tradeFor(instId string) {
	data, ok := cache.Get().GetIndicatorData(instId, configs.BotCurrentConfig.Timeframes[0])
	if !ok {
		log.Printf("[Trader %s][%s] Нет данных индикаторов в кэше", t.cfg.APIKey, instId)
		return
	}

	price, ok := cache.Get().GetPrice(instId)
	if !ok {
		log.Printf("[Trader %s][%s] Нет актуальной цены в кэше — пропуск шага", t.cfg.APIKey, instId)
		return
	}

	log.Printf("[Trader %s][%s] tradeFor(): time=%s Price=%.6f Supertrend=%.6f ATR=%.6f IsUptrend=%v", t.cfg.APIKey, instId, time.Now().Format(time.RFC3339), price, data.Supertrend, data.ATR, data.IsUptrend)
	t.lastIndicatorAt[instId] = time.Now()

	tfIsUptrend := data.IsUptrend

	if t.lastIsUptrend[instId] == nil {
		t.lastIsUptrend[instId] = new(bool)
		*t.lastIsUptrend[instId] = tfIsUptrend
		log.Printf("[Trader %s][%s] Первая инициализация lastIsUptrend=%v", t.cfg.APIKey, instId, tfIsUptrend)
		return
	}

	prevTrend := *t.lastIsUptrend[instId]
	trendChanged := tfIsUptrend != prevTrend
	if trendChanged {
		log.Printf("[Trader %s][%s] Обнаружена смена тренда: было %v → стало %v", t.cfg.APIKey, instId, prevTrend, tfIsUptrend)
	}

	if trendChanged && t.isPositionOpen[instId] {
		log.Printf("[Trader %s][%s] Закрываем позицию перед сменой тренда", t.cfg.APIKey, instId)
		t.closePosition(instId)
	}

	if trendChanged {
		// Входим сразу на первой свече нового тренда
		tradeSize, err := t.Client.GetTradeSize(instId, "USDT", configs.BotCurrentConfig.RiskPercent, price)
		if err != nil {
			log.Printf("[Trader %s][%s] Не удалось получить tradeSize: %v", t.cfg.APIKey, instId, err)
		} else {
			if tfIsUptrend {
				stopLossPrice := t.calculateStopLoss(instId, price, true)
				log.Printf("[Trader %s][%s] Попытка открыть LONG (1-я свеча нового тренда): Size=%.6f Entry=%.6f StopLoss=%.6f", t.cfg.APIKey, instId, tradeSize, price, stopLossPrice)
				if err := t.Client.PlaceOrder(instId, "buy", "long", tradeSize); err != nil {
					log.Printf("[Trader %s][%s] Ошибка при открытии LONG: %v", t.cfg.APIKey, instId, err)
				} else {
					t.trailingActivated[instId] = false
					t.extremePrices[instId] = price
					t.positions[instId] = models.Position{
						InstId:        instId,
						PosSide:       "long",
						TradeSize:     tradeSize,
						EntryPrice:    price,
						StopLossPrice: stopLossPrice,
					}
					t.isPositionOpen[instId] = true
					b := tfIsUptrend
					t.actedOnTrend[instId] = new(bool)
					*t.actedOnTrend[instId] = b
					log.Printf("[Trader %s][%s] Открыт LONG: Entry=%.6f StopLoss=%.6f", t.cfg.APIKey, instId, price, stopLossPrice)
				}
			} else {
				stopLossPrice := t.calculateStopLoss(instId, price, false)
				log.Printf("[Trader %s][%s] Попытка открыть SHORT (1-я свеча нового тренда): Size=%.6f Entry=%.6f StopLoss=%.6f", t.cfg.APIKey, instId, tradeSize, price, stopLossPrice)
				if err := t.Client.PlaceOrder(instId, "sell", "short", tradeSize); err != nil {
					log.Printf("[Trader %s][%s] Ошибка при открытии SHORT: %v", t.cfg.APIKey, instId, err)
				} else {
					t.trailingActivated[instId] = false
					t.extremePrices[instId] = price
					t.positions[instId] = models.Position{
						InstId:        instId,
						PosSide:       "short",
						TradeSize:     tradeSize,
						EntryPrice:    price,
						StopLossPrice: stopLossPrice,
					}
					t.isPositionOpen[instId] = true
					b := tfIsUptrend
					t.actedOnTrend[instId] = new(bool)
					*t.actedOnTrend[instId] = b
					log.Printf("[Trader %s][%s] Открыт SHORT: Entry=%.6f StopLoss=%.6f", t.cfg.APIKey, instId, price, stopLossPrice)
				}
			}
		}
	}

	*t.lastIsUptrend[instId] = tfIsUptrend
	t.lastIndicatorAt[instId] = time.Now()
}

func (t *Trader) monitorTrailingStop() {
	for _, instId := range configs.BotCurrentConfig.TradingPairs {
		if !t.isPositionOpen[instId] {
			continue
		}

		price, ok := cache.Get().GetPrice(instId)
		if !ok {
			log.Printf("[Trader %s][%s] Нет цены для monitorTrailingStop", t.cfg.APIKey, instId)
			continue
		}

		data, ok := cache.Get().GetIndicatorData(instId, configs.BotCurrentConfig.Timeframes[0])
		if !ok {
			log.Printf("[Trader %s][%s] Нет данных индикаторов в monitorTrailingStop", t.cfg.APIKey, instId)
			continue
		}

		atr := data.ATR
		dir := 1.0
		if t.positions[instId].PosSide == "short" {
			dir = -1
		}

		log.Printf("[Trader %s][%s] monitorTrailingStop(): time=%s Price=%.6f Entry=%.6f ATR=%.6f PosSide=%s trailingActive=%v", t.cfg.APIKey, instId, time.Now().Format(time.RFC3339), price, t.positions[instId].EntryPrice, atr, t.positions[instId].PosSide, t.trailingActivated[instId])

		if !t.trailingActivated[instId] && dir*(price-t.positions[instId].EntryPrice) > atr*1.2 {
			t.trailingActivated[instId] = true
			t.extremePrices[instId] = price
			t.trailingStopPrices[instId] = price - dir*atr*configs.BotCurrentConfig.ATRMultiplierTrailing
			log.Printf("[Trader %s][%s] Активация трейлинга: Extreme=%.6f TrailingStop=%.6f", t.cfg.APIKey, instId, price, t.trailingStopPrices[instId])
		}

		if t.trailingActivated[instId] {
			oldExtreme := t.extremePrices[instId]
			oldTrailing := t.trailingStopPrices[instId]

			if dir*(price-t.extremePrices[instId]) > 0 {
				t.extremePrices[instId] = price
				t.trailingStopPrices[instId] = t.extremePrices[instId] - dir*atr*configs.BotCurrentConfig.ATRMultiplierTrailing
				log.Printf("[Trader %s][%s] Обновлён трейлинг: Extreme %.6f→%.6f Stop %.6f→%.6f (ΔStop=%.6f)", t.cfg.APIKey, instId, oldExtreme, t.extremePrices[instId], oldTrailing, t.trailingStopPrices[instId], t.trailingStopPrices[instId]-oldTrailing)
			}

			if dir*(price-t.trailingStopPrices[instId]) <= 0 {
				log.Printf("[Trader %s][%s] Закрыт %s по трейлинг-стопу: Entry=%.6f Stop=%.6f Price=%.6f", t.cfg.APIKey, instId, t.positions[instId].PosSide, t.positions[instId].EntryPrice, t.trailingStopPrices[instId], price)
				t.closePosition(instId)
				continue
			}
		}

		if dir*(price-t.positions[instId].EntryPrice) <= -atr*configs.BotCurrentConfig.ATRMultiplierStopLoss {
			log.Printf("[Trader %s][%s] Сработал стоп-лосс %s: Entry=%.6f Stop=%.6f Price=%.6f", t.cfg.APIKey, instId, t.positions[instId].PosSide, t.positions[instId].EntryPrice, t.positions[instId].StopLossPrice, price)
			t.closePosition(instId)
		}
	}
}

func (t *Trader) calculateStopLoss(instId string, price float64, isUptrend bool) float64 {
	log.Printf("[Trader %s][%s] Расчёт стоп-лосса: Price=%.6f IsUptrend=%v", t.cfg.APIKey, instId, price, isUptrend)
	data, ok := cache.Get().GetIndicatorData(instId, configs.BotCurrentConfig.Timeframes[0])
	if !ok || data.ATR == 0 {
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
		log.Printf("[Trader %s][%s] closePosition вызван, но позиции нет", t.cfg.APIKey, instId)
		return
	}

	side := "sell"
	dir := 1.0
	if t.positions[instId].PosSide == "short" {
		side = "buy"
		dir = -1
	}

	entry := t.positions[instId].EntryPrice
	size := t.positions[instId].TradeSize

	price, ok := cache.Get().GetPrice(instId)
	if !ok {
		log.Printf("[Trader %s][%s] Не удалось получить цену для расчёта PnL при закрытии, используем entry", t.cfg.APIKey, instId)
		price = entry
	}

	pnl := 100.0 * (price - entry) / entry * dir
	log.Printf("[Trader %s][%s] Закрытие позиции: PosSide=%s Entry=%.6f Current=%.6f Size=%.6f PnL=%.3f%%", t.cfg.APIKey, instId, t.positions[instId].PosSide, entry, price, size, pnl)

	if err := t.Client.PlaceOrder(t.positions[instId].InstId, side, t.positions[instId].PosSide, size); err == nil {
		log.Printf("[Trader %s][%s] Позиция закрыта успешно", t.cfg.APIKey, instId)
		t.isPositionOpen[instId] = false
		t.trailingActivated[instId] = false
		delete(t.positions, instId)
		t.actedOnTrend[instId] = nil
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
