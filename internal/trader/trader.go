package trader

import (
	"context"
	"fmt"
	"time"

	"github.com/kuromii5/supertrend_trade_bot/configs"
	"github.com/kuromii5/supertrend_trade_bot/internal/cache"
	"github.com/kuromii5/supertrend_trade_bot/internal/exchanger"
	"github.com/kuromii5/supertrend_trade_bot/internal/exchanger/okx"
	"github.com/kuromii5/supertrend_trade_bot/internal/indicators"
	"github.com/kuromii5/supertrend_trade_bot/internal/log"
	"github.com/kuromii5/supertrend_trade_bot/internal/models"
)

type Trader struct {
	cfg                configs.TraderConfig
	Client             exchanger.TradingAccount
	marketClient       exchanger.MarketProvider
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
	marketClient := okx.NewBotClient()

	return &Trader{
		cfg:                cfg,
		Client:             client,
		marketClient:       marketClient,
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

	// Обновляем MACD данные для всех пар
	for _, instId := range configs.BotCurrentConfig.TradingPairs {
		t.updateMACDData(instId)
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
			// Обновляем MACD данные перед торговлей
			for _, instId := range configs.BotCurrentConfig.TradingPairs {
				t.updateMACDData(instId)
			}
			t.trade()
		case <-tickerTrailing.C:
			t.monitorStop()
		}
	}
}

func (t *Trader) trade() {
	// Создаем сводку трендов для всех монет
	t.logTrendsSummary()
	
	for _, instId := range configs.BotCurrentConfig.TradingPairs {
		if err := t.tradeFor(instId); err != nil {
			log.Log.Error("[tradeFor] Ошибка в трейде", "error", err)
		}
	}
}

func (t *Trader) logTrendsSummary() {
	var trends []string
	var macdSignals []string
	
	for _, instId := range configs.BotCurrentConfig.TradingPairs {
		// Supertrend тренд
		if data, ok := cache.Get().GetIndicatorData(instId, configs.BotCurrentConfig.Timeframes[0]); ok {
			trend := "DOWN"
			if data.IsUptrend {
				trend = "UP"
			}
			trends = append(trends, fmt.Sprintf("%s:%s", instId, trend))
		}
		
		// MACD сигналы
		if macdData, ok := cache.Get().GetMACDData(instId); ok {
			signal := "NEUTRAL"
			if macdData.BuySignal {
				signal = "BUY"
			} else if macdData.SellSignal {
				signal = "SELL"
			}
			macdSignals = append(macdSignals, fmt.Sprintf("%s:%s", instId, signal))
		}
	}
	
	log.Log.Info("Тренды", "Supertrend", trends, "MACD", macdSignals)
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

	tfIsUptrend := data.IsUptrend

	if t.lastIsUptrend[instId] == nil {
		t.lastIsUptrend[instId] = new(bool)
		*t.lastIsUptrend[instId] = tfIsUptrend
		return nil
	}

	prevTrend := *t.lastIsUptrend[instId]
	trendChanged := tfIsUptrend != prevTrend
	if trendChanged {
		log.Log.Info("Смена тренда", "pair", instId, "было", prevTrend, "стало", tfIsUptrend)
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
			log.Log.Info("Закрываем позицию перед сменой тренда", "pair", instId)
			t.closePosition(instId)
		}

		if trendChanged {
			tradeSize, err := t.Client.GetTradeSize(instId, "USDT", configs.BotCurrentConfig.RiskPercent, price)
			if err != nil {
				log.Log.Error("Ошибка получения размера сделки", "pair", instId, "error", err)
			} else {
				if tfIsUptrend {
					stopLossPrice := t.calculateStopLoss(instId, price, true)
					if err := t.Client.PlaceOrder(instId, "buy", "long", tradeSize); err != nil {
						log.Log.Error("Ошибка открытия LONG", "pair", instId, "error", err)
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
						log.Log.Info("Открыт LONG", "pair", instId, "entry", price, "stop", stopLossPrice, "size", tradeSize)
					}
				} else {
					stopLossPrice := t.calculateStopLoss(instId, price, false)
					if err := t.Client.PlaceOrder(instId, "sell", "short", tradeSize); err != nil {
						log.Log.Error("Ошибка открытия SHORT", "pair", instId, "error", err)
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
						log.Log.Info("Открыт SHORT", "pair", instId, "entry", price, "stop", stopLossPrice, "size", tradeSize)
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
			continue
		}

		// ПРИОРИТЕТ 1: Проверяем MACD сигналы для закрытия позиции (самый высокий приоритет)
		if configs.BotCurrentConfig.MacdTimeframe != "" {
			macdData, ok := cache.Get().GetMACDData(instId)
			if ok {
				// Если у нас открыт LONG, а MACD дает сигнал на продажу
				if t.positions[instId].PosSide == "long" && macdData.SellSignal {
					log.Log.Info("Закрыт LONG по MACD", "pair", instId, "entry", t.positions[instId].EntryPrice, "price", price)
					t.closePosition(instId)
					continue
				}
				// Если у нас открыт SHORT, а MACD дает сигнал на покупку
				if t.positions[instId].PosSide == "short" && macdData.BuySignal {
					log.Log.Info("Закрыт SHORT по MACD", "pair", instId, "entry", t.positions[instId].EntryPrice, "price", price)
					t.closePosition(instId)
					continue
				}
			}
		}

		// ПРИОРИТЕТ 2: Проверяем трейлинг-стоп
		data, ok := cache.Get().GetIndicatorData(instId, configs.BotCurrentConfig.Timeframes[0])
		if !ok {
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
				log.Log.Info("Улучшен стоп-лосс", "pair", instId, "old", oldStop, "new", newStopPrice)
			}
		}

		// Логируем информацию о позиции каждые 10 секунд
		pnl := 100.0 * (price - t.positions[instId].EntryPrice) / t.positions[instId].EntryPrice * dir
		log.Log.Info("Позиция", "pair", instId, "side", t.positions[instId].PosSide, "entry", t.positions[instId].EntryPrice, 
			"current", price, "stop", t.bestStopPrice[instId], "pnl", fmt.Sprintf("%.2f%%", pnl))

		if dir*(price-t.bestStopPrice[instId]) <= 0 {
			log.Log.Info("Закрыт по стоп-лоссу", "pair", instId, "side", t.positions[instId].PosSide, "entry", t.positions[instId].EntryPrice, "stop", t.bestStopPrice[instId], "price", price)
			t.closePosition(instId)
		}
	}
}

func (t *Trader) calculateStopLoss(instId string, price float64, isUptrend bool) float64 {
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
		price = entry
	}

	pnl := 100.0 * (price - entry) / entry * dir
	log.Log.Info("Закрытие позиции", "pair", instId, "side", t.positions[instId].PosSide, "entry", entry, "current", price, "size", size, "pnl", fmt.Sprintf("%.2f%%", pnl))

	if err := t.Client.PlaceOrder(t.positions[instId].InstId, side, t.positions[instId].PosSide, size); err == nil {
		t.isPositionOpen[instId] = false
		delete(t.positions, instId)
		delete(t.bestStopPrice, instId)
		t.actedOnTrend[instId] = nil
	} else {
		log.Log.Error("Ошибка закрытия позиции", "pair", instId, "error", err)
	}
}

func (t *Trader) updateMACDData(instId string) {
	if configs.BotCurrentConfig.MacdTimeframe == "" {
		return
	}

	// Получаем свечи для MACD таймфрейма
	macdCandles, err := t.marketClient.GetCandlesticks(instId, configs.BotCurrentConfig.MacdTimeframe, configs.BotCurrentConfig.CandlesAmount)
	if err != nil {
		log.Log.Error("Ошибка получения свечей MACD", "pair", instId, "error", err)
		return
	}

	if len(macdCandles) == 0 {
		log.Log.Warn("Нет свечей для MACD", "pair", instId)
		return
	}

	// Рассчитываем MACD
	macdData := indicators.CalculateMACD(macdCandles, configs.BotCurrentConfig.MacdFastPeriod, configs.BotCurrentConfig.MacdSlowPeriod, configs.BotCurrentConfig.MacdSignalPeriod)
	
	if len(macdData) > 0 {
		lastMacd := macdData[len(macdData)-1]
		buySignal, sellSignal := indicators.GetMACDSignal(macdData)

		cache.Get().SetMACDData(instId, cache.MACDIndicatorData{
			MACD:      lastMacd.MACD,
			Signal:    lastMacd.Signal,
			Histogram: lastMacd.Histogram,
			BuySignal:  buySignal,
			SellSignal: sellSignal,
		})

		log.Log.Debug("MACD обновлен", "pair", instId, "macd", lastMacd.MACD, "signal", lastMacd.Signal, "buy", buySignal, "sell", sellSignal)
	} else {
		log.Log.Warn("MACD данные пусты", "pair", instId, "required", configs.BotCurrentConfig.MacdSlowPeriod+configs.BotCurrentConfig.MacdSignalPeriod)
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
		log.Log.Info("Закрыт LONG по MACD", "pair", instId, "entry", t.positions[instId].EntryPrice, "price", price)
		t.closePosition(instId)
		return
	}
	// Если у нас открыт SHORT, а MACD дает сигнал на покупку
	if t.positions[instId].PosSide == "short" && macdData.BuySignal {
		log.Log.Info("Закрыт SHORT по MACD", "pair", instId, "entry", t.positions[instId].EntryPrice, "price", price)
		t.closePosition(instId)
		return
	}
}

func (t *Trader) Stop() {
	for _, instId := range configs.BotCurrentConfig.TradingPairs {
		t.closePosition(instId)
	}
}
