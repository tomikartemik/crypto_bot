package trader

import (
	"context"
	"fmt"
	"time"

	"github.com/kuromii5/supertrend_trade_bot/configs"
	"github.com/kuromii5/supertrend_trade_bot/internal/cache"
	"github.com/kuromii5/supertrend_trade_bot/internal/exchanger"
	"github.com/kuromii5/supertrend_trade_bot/internal/exchanger/okx"
	"github.com/kuromii5/supertrend_trade_bot/internal/log"
	"github.com/kuromii5/supertrend_trade_bot/internal/models"
)

type Trader struct {
	cfg                configs.TraderConfig
	Client             exchanger.TradingAccount
	lastIsUptrend      map[string]*bool
	actedOnTrend       map[string]*bool
	isPositionOpen     map[string]bool
	positions          map[string]models.Position
	bestStopPrice      map[string]float64
	updateCh           chan string
	lastIndicatorAt    map[string]time.Time
	trendChangeCounter map[string]int
}

func NewTrader(cfg configs.TraderConfig) *Trader {
	client := okx.NewClient(cfg.APIKey, cfg.APISecret, cfg.Passphrase)

	return &Trader{
		cfg:                cfg,
		Client:             client,
		lastIsUptrend:      make(map[string]*bool),
		actedOnTrend:       make(map[string]*bool),
		isPositionOpen:     make(map[string]bool),
		positions:          make(map[string]models.Position),
		bestStopPrice:      make(map[string]float64),
		updateCh:           make(chan string, 128),
		lastIndicatorAt:    make(map[string]time.Time),
		trendChangeCounter: make(map[string]int),
	}
}

func (t *Trader) Run(ctx context.Context, interval time.Duration) {
	log.Log.Debug(fmt.Sprintf("[Trader %s] Начало торговли (tf=%s)", t.cfg.APIKey, interval))

	for _, instId := range configs.BotCurrentConfig.TradingPairs {
		if data, ok := cache.Get().GetIndicatorData(instId, configs.BotCurrentConfig.Timeframes[0]); ok {
			b := data.IsUptrend
			t.lastIsUptrend[instId] = new(bool)
			*t.lastIsUptrend[instId] = b
			log.Log.Debug(fmt.Sprintf("[Trader %s][%s] Инициализация lastIsUptrend=%v", t.cfg.APIKey, instId, b))
		} else {
			t.lastIsUptrend[instId] = nil
			log.Log.Debug(fmt.Sprintf("[Trader %s][%s] lastIsUptrend оставляем nil (нет данных в кэше)", t.cfg.APIKey, instId))
		}
		t.actedOnTrend[instId] = nil
	}

	log.Log.Info(fmt.Sprintf("[Trader %s] Выполнение initial trade() для всех пар", t.cfg.APIKey))
	// Первичный проход (может не открыть сделок, если индикаторы ещё не посчитаны)
	//! он никогда не откроет, т.к. strategyUpdater апдейтится всегда позже чем вызов этого метода.
	//! Можно поставить мьютекс и локаться, пока первый update не отработает
	t.trade()

	// Выравниваемся по границе, чтобы следующее действие было ровно по интервалу
	now := time.Now()
	next := now.Truncate(interval).Add(interval)
	time.Sleep(next.Sub(now))

	//! Тикер надо запускать перед трейдом, чтобы не ждать пока t.trade() закончит выполняться
	tickerTrade := time.NewTicker(interval)
	defer tickerTrade.Stop()

	// Сразу обрабатываем сигнал на границе свечи
	t.trade()

	tickerTrailing := time.NewTicker(10 * time.Second)
	defer tickerTrailing.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Log.Info(fmt.Sprintf("[Trader %s] Получен сигнал, выключение трейдера", t.cfg.APIKey))
			return
		case inst := <-t.updateCh:
			log.Log.Info(fmt.Sprintf("[Trader %s][%s] NotifyUpdate received — немедленная обработка", t.cfg.APIKey, inst))
			t.tradeFor(inst)
			// Дополнительная проверка MACD при обновлении данных
			if t.isPositionOpen[inst] && configs.BotCurrentConfig.MacdTimeframe != "" {
				t.checkMACDSignals(inst)
			}
		case <-tickerTrade.C:
			t.trade()
		case <-tickerTrailing.C:
			t.monitorStop()
		}
	}
}

func (t *Trader) trade() {
	for _, instId := range configs.BotCurrentConfig.TradingPairs {
		if err := t.tradeFor(instId); err != nil {
			log.Log.Error("[tradeFor] Ошибка в трейде", "error", err)
		}
	}
}

func (t *Trader) tradeFor(instId string) error {
	data, ok := cache.Get().GetIndicatorData(instId, configs.BotCurrentConfig.Timeframes[0])
	if !ok {
		return fmt.Errorf("[Trader %s][%s] Нет данных индикаторов в кэше", t.cfg.APIKey, instId)
	}

	price, ok := cache.Get().GetPrice(instId)
	if !ok {
		return fmt.Errorf("[Trader %s][%s] Нет актуальной цены в кэше — пропуск шага", t.cfg.APIKey, instId)
	}

	log.Log.Debug(fmt.Sprintf("[Trader %s][%s] tradeFor(): time=%s Price=%.6f Supertrend=%.6f ATR=%.6f IsUptrend=%v", t.cfg.APIKey, instId, time.Now().Format(time.RFC3339), price, data.Supertrend, data.ATR, data.IsUptrend))
	t.lastIndicatorAt[instId] = time.Now()

	tfIsUptrend := data.IsUptrend

	if t.lastIsUptrend[instId] == nil {
		t.lastIsUptrend[instId] = new(bool)
		*t.lastIsUptrend[instId] = tfIsUptrend
		log.Log.Debug(fmt.Sprintf("[Trader %s][%s] Первая инициализация lastIsUptrend=%v", t.cfg.APIKey, instId, tfIsUptrend))
		return nil
	}

	prevTrend := *t.lastIsUptrend[instId]
	trendChanged := tfIsUptrend != prevTrend
	if trendChanged {
		log.Log.Info(fmt.Sprintf("[Trader %s][%s] Обнаружена смена тренда: было %v → стало %v", t.cfg.APIKey, instId, prevTrend, tfIsUptrend))
	}

		// Проверяем MACD сигналы для закрытия позиции (если позиция открыта)
		if t.isPositionOpen[instId] && configs.BotCurrentConfig.MacdTimeframe != "" {
			macdData, ok := cache.Get().GetMACDData(instId)
			if ok {
				// Если у нас открыт LONG, а MACD дает сигнал на продажу
				if t.positions[instId].PosSide == "long" && macdData.SellSignal {
					log.Log.Info(fmt.Sprintf("[Trader %s][%s] Закрыт LONG по MACD сигналу на продажу (основной цикл): Entry=%.6f Price=%.6f", t.cfg.APIKey, instId, t.positions[instId].EntryPrice, price))
					t.closePosition(instId)
					*t.lastIsUptrend[instId] = tfIsUptrend
					t.lastIndicatorAt[instId] = time.Now()
					return nil
				}
				// Если у нас открыт SHORT, а MACD дает сигнал на покупку
				if t.positions[instId].PosSide == "short" && macdData.BuySignal {
					log.Log.Info(fmt.Sprintf("[Trader %s][%s] Закрыт SHORT по MACD сигналу на покупку (основной цикл): Entry=%.6f Price=%.6f", t.cfg.APIKey, instId, t.positions[instId].EntryPrice, price))
					t.closePosition(instId)
					*t.lastIsUptrend[instId] = tfIsUptrend
					t.lastIndicatorAt[instId] = time.Now()
					return nil
				}
			}
		}

		if trendChanged && t.isPositionOpen[instId] {
			log.Log.Info(fmt.Sprintf("[Trader %s][%s] Закрываем позицию перед сменой тренда", t.cfg.APIKey, instId))
			t.closePosition(instId)
		}

	if trendChanged {
		tradeSize, err := t.Client.GetTradeSize(instId, "USDT", configs.BotCurrentConfig.RiskPercent, price)
		if err != nil {
			log.Log.Error(fmt.Sprintf("[Trader %s][%s] Не удалось получить tradeSize: %v", t.cfg.APIKey, instId, err))
		} else {
			if tfIsUptrend {
				stopLossPrice := t.calculateStopLoss(instId, price, true)
				log.Log.Info(fmt.Sprintf("[Trader %s][%s] Попытка открыть LONG (1-я свеча нового тренда): Size=%.6f Entry=%.6f StopLoss=%.6f", t.cfg.APIKey, instId, tradeSize, price, stopLossPrice))
				if err := t.Client.PlaceOrder(instId, "buy", "long", tradeSize); err != nil {
					log.Log.Error(fmt.Sprintf("[Trader %s][%s] Ошибка при открытии LONG: %v", t.cfg.APIKey, instId, err))
				} else {
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
					log.Log.Info(fmt.Sprintf("[Trader %s][%s] Открыт LONG: Entry=%.6f StopLoss=%.6f", t.cfg.APIKey, instId, price, stopLossPrice))
				}
			} else {
				stopLossPrice := t.calculateStopLoss(instId, price, false)
				log.Log.Info(fmt.Sprintf("[Trader %s][%s] Попытка открыть SHORT (1-я свеча нового тренда): Size=%.6f Entry=%.6f StopLoss=%.6f", t.cfg.APIKey, instId, tradeSize, price, stopLossPrice))
				if err := t.Client.PlaceOrder(instId, "sell", "short", tradeSize); err != nil {
					log.Log.Error(fmt.Sprintf("[Trader %s][%s] Ошибка при открытии SHORT: %v", t.cfg.APIKey, instId, err))
				} else {
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
					log.Log.Info(fmt.Sprintf("[Trader %s][%s] Открыт SHORT: Entry=%.6f StopLoss=%.6f", t.cfg.APIKey, instId, price, stopLossPrice))
				}
			}
		}
	}

	*t.lastIsUptrend[instId] = tfIsUptrend
	t.lastIndicatorAt[instId] = time.Now()
	return nil
}

func (t *Trader) monitorStop() {
	for _, instId := range configs.BotCurrentConfig.TradingPairs {
		if !t.isPositionOpen[instId] {
			continue
		}

		price, ok := cache.Get().GetPrice(instId)
		if !ok {
			log.Log.Error(fmt.Sprintf("[Trader %s][%s] Нет цены для monitorStop", t.cfg.APIKey, instId))
			continue
		}

		// ПРИОРИТЕТ 1: Проверяем MACD сигналы для закрытия позиции (самый высокий приоритет)
		if configs.BotCurrentConfig.MacdTimeframe != "" {
			log.Log.Debug(fmt.Sprintf("[Trader %s][%s] Проверяем MACD данные, MacdTimeframe=%s", t.cfg.APIKey, instId, configs.BotCurrentConfig.MacdTimeframe))
			
			macdData, ok := cache.Get().GetMACDData(instId)
			if ok {
				log.Log.Info(fmt.Sprintf("[Trader %s][%s] MACD данные найдены: MACD=%.6f Signal=%.6f BuySignal=%v SellSignal=%v PosSide=%s", 
					t.cfg.APIKey, instId, macdData.MACD, macdData.Signal, macdData.BuySignal, macdData.SellSignal, t.positions[instId].PosSide))
				
				// Если у нас открыт LONG, а MACD дает сигнал на продажу
				if t.positions[instId].PosSide == "long" && macdData.SellSignal {
					log.Log.Info(fmt.Sprintf("[Trader %s][%s] Закрыт LONG по MACD сигналу на продажу: Entry=%.6f Price=%.6f", t.cfg.APIKey, instId, t.positions[instId].EntryPrice, price))
					t.closePosition(instId)
					continue
				}
				// Если у нас открыт SHORT, а MACD дает сигнал на покупку
				if t.positions[instId].PosSide == "short" && macdData.BuySignal {
					log.Log.Info(fmt.Sprintf("[Trader %s][%s] Закрыт SHORT по MACD сигналу на покупку: Entry=%.6f Price=%.6f", t.cfg.APIKey, instId, t.positions[instId].EntryPrice, price))
					t.closePosition(instId)
					continue
				}
			} else {
				log.Log.Warn(fmt.Sprintf("[Trader %s][%s] Нет MACD данных в кэше для %s", t.cfg.APIKey, instId, instId))
			}
		} else {
			log.Log.Warn(fmt.Sprintf("[Trader %s][%s] MacdTimeframe не установлен в конфигурации", t.cfg.APIKey, instId))
		}

		// ПРИОРИТЕТ 2: Проверяем трейлинг-стоп
		data, ok := cache.Get().GetIndicatorData(instId, configs.BotCurrentConfig.Timeframes[0])
		if !ok {
			log.Log.Error(fmt.Sprintf("[Trader %s][%s] Нет данных индикаторов в monitorStop", t.cfg.APIKey, instId))
			continue
		}

		atr := data.ATR
		dir := 1.0
		if t.positions[instId].PosSide == "short" {
			dir = -1
		}

		stopDistance := atr * configs.BotCurrentConfig.ATRMultiplierStop
		newStopPrice := price - dir*stopDistance

		if t.bestStopPrice[instId] == 0 {
			t.bestStopPrice[instId] = newStopPrice
			log.Log.Debug(fmt.Sprintf("[Trader %s][%s] Инициализация стоп-лосса: %.6f", t.cfg.APIKey, instId, newStopPrice))
		} else {
			shouldUpdate := false
			if t.positions[instId].PosSide == "long" {
				shouldUpdate = newStopPrice > t.bestStopPrice[instId]
			} else {
				shouldUpdate = newStopPrice < t.bestStopPrice[instId]
			}

			if shouldUpdate {
				oldStop := t.bestStopPrice[instId]
				t.bestStopPrice[instId] = newStopPrice
				log.Log.Info(fmt.Sprintf("[Trader %s][%s] Улучшен стоп-лосс: %.6f → %.6f", t.cfg.APIKey, instId, oldStop, newStopPrice))
			}
		}

		log.Log.Debug(fmt.Sprintf("[Trader %s][%s] monitorStop(): time=%s Price=%.6f Entry=%.6f ATR=%.6f PosSide=%s BestStop=%.6f NewStop=%.6f",
			t.cfg.APIKey, instId, time.Now().Format(time.RFC3339), price, t.positions[instId].EntryPrice, atr, t.positions[instId].PosSide, t.bestStopPrice[instId], newStopPrice))

		if dir*(price-t.bestStopPrice[instId]) <= 0 {
			log.Log.Info(fmt.Sprintf("[Trader %s][%s] Закрыт %s по стоп-лоссу: Entry=%.6f Stop=%.6f Price=%.6f", t.cfg.APIKey, instId, t.positions[instId].PosSide, t.positions[instId].EntryPrice, t.bestStopPrice[instId], price))
			t.closePosition(instId)
		}
	}
}

func (t *Trader) calculateStopLoss(instId string, price float64, isUptrend bool) float64 {
	log.Log.Debug(fmt.Sprintf("[Trader %s][%s] Расчёт стоп-лосса: Price=%.6f IsUptrend=%v", t.cfg.APIKey, instId, price, isUptrend))
	data, ok := cache.Get().GetIndicatorData(instId, configs.BotCurrentConfig.Timeframes[0])
	if !ok || data.ATR == 0 {
		if isUptrend {
			return price * 0.992
		}
		return price * 1.008
	}
	stopLossDistance := data.ATR * configs.BotCurrentConfig.ATRMultiplierStop
	if isUptrend {
		return price - stopLossDistance
	}
	return price + stopLossDistance
}

func (t *Trader) closePosition(instId string) {
	if !t.isPositionOpen[instId] {
		log.Log.Warn(fmt.Sprintf("[Trader %s][%s] closePosition вызван, но позиции нет", t.cfg.APIKey, instId))
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
		log.Log.Warn(fmt.Sprintf("[Trader %s][%s] Не удалось получить цену для расчёта PnL при закрытии, используем entry", t.cfg.APIKey, instId))
		price = entry
	}

	pnl := 100.0 * (price - entry) / entry * dir
	log.Log.Info(fmt.Sprintf("[Trader %s][%s] Закрытие позиции: PosSide=%s Entry=%.6f Current=%.6f Size=%.6f PnL=%.3f%%", t.cfg.APIKey, instId, t.positions[instId].PosSide, entry, price, size, pnl))

	if err := t.Client.PlaceOrder(t.positions[instId].InstId, side, t.positions[instId].PosSide, size); err == nil {
		log.Log.Debug(fmt.Sprintf("[Trader %s][%s] Позиция закрыта успешно", t.cfg.APIKey, instId))
		t.isPositionOpen[instId] = false
		delete(t.positions, instId)
		delete(t.bestStopPrice, instId)
		t.actedOnTrend[instId] = nil
	} else {
		log.Log.Error(fmt.Sprintf("[Trader %s][%s] Ошибка при закрытии позиции: %v", t.cfg.APIKey, instId, err))
	}
}

func (t *Trader) checkMACDSignals(instId string) {
	if !t.isPositionOpen[instId] {
		return
	}

	price, ok := cache.Get().GetPrice(instId)
	if !ok {
		return
	}

	macdData, ok := cache.Get().GetMACDData(instId)
	if !ok {
		return
	}

	// Если у нас открыт LONG, а MACD дает сигнал на продажу
	if t.positions[instId].PosSide == "long" && macdData.SellSignal {
		log.Log.Info(fmt.Sprintf("[Trader %s][%s] Закрыт LONG по MACD сигналу на продажу (проверка): Entry=%.6f Price=%.6f", t.cfg.APIKey, instId, t.positions[instId].EntryPrice, price))
		t.closePosition(instId)
		return
	}
	// Если у нас открыт SHORT, а MACD дает сигнал на покупку
	if t.positions[instId].PosSide == "short" && macdData.BuySignal {
		log.Log.Info(fmt.Sprintf("[Trader %s][%s] Закрыт SHORT по MACD сигналу на покупку (проверка): Entry=%.6f Price=%.6f", t.cfg.APIKey, instId, t.positions[instId].EntryPrice, price))
		t.closePosition(instId)
		return
	}
}

func (t *Trader) Stop() {
	for _, instId := range configs.BotCurrentConfig.TradingPairs {
		t.closePosition(instId)
	}
}
