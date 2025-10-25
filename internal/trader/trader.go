package trader

import (
	"context"
	"fmt"
	"time"

	"github.com/tomikartemik/crypto_bot/configs"
	"github.com/tomikartemik/crypto_bot/internal/cache"
	"github.com/tomikartemik/crypto_bot/internal/exchanger"
	"github.com/tomikartemik/crypto_bot/internal/exchanger/okx"
	"github.com/tomikartemik/crypto_bot/internal/log"
	"github.com/tomikartemik/crypto_bot/internal/models"
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
	trendChangeCh      chan string // Канал для мгновенных уведомлений о смене тренда
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
		trendChangeCh:      make(chan string, 128), // Канал для мгновенных уведомлений о смене тренда
		lastIndicatorAt:    make(map[string]time.Time),
		trendChangeCounter: make(map[string]int),
	}
}

func (t *Trader) Run(ctx context.Context, interval time.Duration) {
	log.Log.Debug(fmt.Sprintf("[Trader %s] Начало торговли (tf=%s)", t.cfg.APIKey, interval))

	ltfTF, hasLTF := getLTFTimeframe()
	if !hasLTF {
		log.Log.Error(fmt.Sprintf("[Trader %s] Не задан ни один таймфрейм для торговли", t.cfg.APIKey))
		return
	}

	for _, instId := range configs.BotCurrentConfig.TradingPairs {
		if data, ok := cache.Get().GetIndicatorData(instId, ltfTF); ok {
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
		case inst := <-t.trendChangeCh:
			log.Log.Info(fmt.Sprintf("[Trader %s][%s] 🚨 TREND CHANGE SIGNAL — мгновенная обработка смены тренда", t.cfg.APIKey, inst))
			t.handleTrendChange(inst)
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

// handleTrendChange обрабатывает мгновенную смену тренда
func (t *Trader) handleTrendChange(instId string) {
	if err := t.tradeFor(instId); err != nil {
		log.Log.Error(fmt.Sprintf("[Trader %s][%s] Ошибка обработки смены тренда: %v", t.cfg.APIKey, instId, err))
	}
}

func (t *Trader) tradeFor(instId string) error {
	ltfTF, hasLTF := getLTFTimeframe()
	if !hasLTF {
		return fmt.Errorf("[Trader %s][%s] Не задан таймфрейм для LTF", t.cfg.APIKey, instId)
	}

	ltfData, ok := cache.Get().GetIndicatorData(instId, ltfTF)
	if !ok {
		return fmt.Errorf("[Trader %s][%s] Нет LTF данных индикаторов (%s)", t.cfg.APIKey, instId, ltfTF)
	}

	htfTF, hasHTF := getHTFTimeframe()
	var htfData cache.IndicatorData
	if hasHTF {
		var exists bool
		htfData, exists = cache.Get().GetIndicatorData(instId, htfTF)
		if !exists {
			return fmt.Errorf("[Trader %s][%s] Нет HTF данных индикаторов (%s)", t.cfg.APIKey, instId, htfTF)
		}
	} else {
		htfData = ltfData
	}

	price, ok := cache.Get().GetPrice(instId)
	if !ok {
		return fmt.Errorf("[Trader %s][%s] Нет актуальной цены в кэше — пропуск шага", t.cfg.APIKey, instId)
	}

	ltfTrend := ltfData.Trend
	if ltfTrend == 0 {
		ltfTrend = boolToTrend(ltfData.IsUptrend)
	}
	htfTrend := htfData.Trend
	if htfTrend == 0 {
		htfTrend = boolToTrend(htfData.IsUptrend)
	}

	htfLabel := ltfTF
	if hasHTF {
		htfLabel = htfTF
	}

	log.Log.Debug(fmt.Sprintf("[Trader %s][%s] tradeFor(): time=%s Price=%.6f LTF[%s]=%d HTF[%s]=%d SupertrendLTF=%.6f SupertrendHTF=%.6f",
		t.cfg.APIKey,
		instId,
		time.Now().Format(time.RFC3339),
		price,
		ltfTF,
		ltfTrend,
		htfLabel,
		htfTrend,
		ltfData.Supertrend,
		htfData.Supertrend,
	))
	t.lastIndicatorAt[instId] = time.Now()

	if t.lastIsUptrend[instId] == nil {
		t.lastIsUptrend[instId] = new(bool)
		*t.lastIsUptrend[instId] = ltfData.IsUptrend
		log.Log.Debug(fmt.Sprintf("[Trader %s][%s] Первая инициализация lastIsUptrend=%v", t.cfg.APIKey, instId, ltfData.IsUptrend))
		return nil
	}

	prevLTF := *t.lastIsUptrend[instId]
	ltfChanged := prevLTF != ltfData.IsUptrend
	if ltfChanged {
		log.Log.Info(fmt.Sprintf("[Trader %s][%s] LTF сменил направление: было %v → стало %v", t.cfg.APIKey, instId, prevLTF, ltfData.IsUptrend))
	}

	if t.isPositionOpen[instId] && ltfTrend != htfTrend {
		log.Log.Info(fmt.Sprintf("[Trader %s][%s] Закрываем позицию: LTF (%d) != HTF (%d)", t.cfg.APIKey, instId, ltfTrend, htfTrend))
		t.closePosition(instId)
	}

	aligned := ltfTrend == htfTrend
	if aligned && !t.isPositionOpen[instId] {
		dirUp := ltfTrend == 1
		alreadyActed := t.actedOnTrend[instId] != nil && *t.actedOnTrend[instId] == dirUp
		if ltfChanged || !alreadyActed {
			tradeSize, err := t.Client.GetTradeSize(instId, "USDT", configs.BotCurrentConfig.RiskPercent, price)
			if err != nil {
				log.Log.Error(fmt.Sprintf("[Trader %s][%s] Не удалось получить tradeSize: %v", t.cfg.APIKey, instId, err))
			} else {
				if dirUp {
					t.openLong(instId, tradeSize, price)
				} else {
					t.openShort(instId, tradeSize, price)
				}
			}
		}
	}

	*t.lastIsUptrend[instId] = ltfData.IsUptrend
	t.lastIndicatorAt[instId] = time.Now()
	return nil
}

func (t *Trader) openLong(instId string, tradeSize, price float64) {
	stopLossPrice := t.calculateStopLoss(instId, price, true)
	log.Log.Info(fmt.Sprintf("[Trader %s][%s] Попытка открыть LONG: Size=%.6f Entry=%.6f StopLoss=%.6f", t.cfg.APIKey, instId, tradeSize, price, stopLossPrice))
	if err := t.Client.PlaceOrder(instId, "buy", "long", tradeSize); err != nil {
		log.Log.Error(fmt.Sprintf("[Trader %s][%s] Ошибка при открытии LONG: %v", t.cfg.APIKey, instId, err))
		return
	}
	t.positions[instId] = models.Position{
		InstId:        instId,
		PosSide:       "long",
		TradeSize:     tradeSize,
		EntryPrice:    price,
		StopLossPrice: stopLossPrice,
	}
	t.isPositionOpen[instId] = true
	t.actedOnTrend[instId] = new(bool)
	*t.actedOnTrend[instId] = true
	log.Log.Info(fmt.Sprintf("[Trader %s][%s] Открыт LONG: Entry=%.6f StopLoss=%.6f", t.cfg.APIKey, instId, price, stopLossPrice))
}

func (t *Trader) openShort(instId string, tradeSize, price float64) {
	stopLossPrice := t.calculateStopLoss(instId, price, false)
	log.Log.Info(fmt.Sprintf("[Trader %s][%s] Попытка открыть SHORT: Size=%.6f Entry=%.6f StopLoss=%.6f", t.cfg.APIKey, instId, tradeSize, price, stopLossPrice))
	if err := t.Client.PlaceOrder(instId, "sell", "short", tradeSize); err != nil {
		log.Log.Error(fmt.Sprintf("[Trader %s][%s] Ошибка при открытии SHORT: %v", t.cfg.APIKey, instId, err))
		return
	}
	t.positions[instId] = models.Position{
		InstId:        instId,
		PosSide:       "short",
		TradeSize:     tradeSize,
		EntryPrice:    price,
		StopLossPrice: stopLossPrice,
	}
	t.isPositionOpen[instId] = true
	t.actedOnTrend[instId] = new(bool)
	*t.actedOnTrend[instId] = false
	log.Log.Info(fmt.Sprintf("[Trader %s][%s] Открыт SHORT: Entry=%.6f StopLoss=%.6f", t.cfg.APIKey, instId, price, stopLossPrice))
}

func (t *Trader) monitorStop() {
	ltfTF, hasLTF := getLTFTimeframe()
	if !hasLTF {
		log.Log.Error(fmt.Sprintf("[Trader %s] Нельзя сопровождать стопы: не задан LTF таймфрейм", t.cfg.APIKey))
		return
	}

	for _, instId := range configs.BotCurrentConfig.TradingPairs {
		if !t.isPositionOpen[instId] {
			continue
		}

		price, ok := cache.Get().GetPrice(instId)
		if !ok {
			log.Log.Error(fmt.Sprintf("[Trader %s][%s] Нет цены для monitorStop", t.cfg.APIKey, instId))
			continue
		}

		data, ok := cache.Get().GetIndicatorData(instId, ltfTF)
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
	if ltfTF, hasLTF := getLTFTimeframe(); hasLTF {
		if data, ok := cache.Get().GetIndicatorData(instId, ltfTF); ok && data.ATR != 0 {
			stopLossDistance := data.ATR * configs.BotCurrentConfig.ATRMultiplierStop
			if isUptrend {
				return price - stopLossDistance
			}
			return price + stopLossDistance
		}
	}

	if isUptrend {
		return price * 0.992
	}
	return price * 1.008
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

// NotifyTrendChange уведомляет трейдера о смене тренда
func (t *Trader) NotifyTrendChange(instId string) {
	select {
	case t.trendChangeCh <- instId:
		log.Log.Debug(fmt.Sprintf("[Trader %s][%s] Уведомление о смене тренда отправлено", t.cfg.APIKey, instId))
	default:
		log.Log.Warn(fmt.Sprintf("[Trader %s][%s] Канал уведомлений о смене тренда переполнен", t.cfg.APIKey, instId))
	}
}

func (t *Trader) Stop() {
	for _, instId := range configs.BotCurrentConfig.TradingPairs {
		t.closePosition(instId)
	}
}

func boolToTrend(isUp bool) int {
	if isUp {
		return 1
	}
	return -1
}

func getLTFTimeframe() (string, bool) {
	if len(configs.BotCurrentConfig.Timeframes) == 0 {
		return "", false
	}
	return configs.BotCurrentConfig.Timeframes[0], true
}

func getHTFTimeframe() (string, bool) {
	if len(configs.BotCurrentConfig.Timeframes) < 2 {
		return "", false
	}
	return configs.BotCurrentConfig.Timeframes[1], true
}
