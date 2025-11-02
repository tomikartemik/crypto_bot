package trader

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tomikartemik/crypto_bot/configs"
	"github.com/tomikartemik/crypto_bot/internal/cache"
	"github.com/tomikartemik/crypto_bot/internal/exchanger"
	"github.com/tomikartemik/crypto_bot/internal/exchanger/okx"
	"github.com/tomikartemik/crypto_bot/internal/log"
	"github.com/tomikartemik/crypto_bot/internal/models"
	"github.com/tomikartemik/crypto_bot/internal/notifier"
)

type Trader struct {
	cfg                configs.TraderConfig
	Client             exchanger.TradingAccount
	lastIsUptrend      map[string]*bool
	actedOnTrend       map[string]*bool
	isPositionOpen     map[string]bool
	positions          map[string]models.Position
	positionState      map[string]*positionState
	bestStopPrice      map[string]float64
	updateCh           chan string
	trendChangeCh      chan string // Канал для мгновенных уведомлений о смене тренда
	lastIndicatorAt    map[string]time.Time
	trendChangeCounter map[string]int
	statsManager       *StatsManager
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
		positionState:      make(map[string]*positionState),
		bestStopPrice:      make(map[string]float64),
		updateCh:           make(chan string, 128),
		trendChangeCh:      make(chan string, 128), // Канал для мгновенных уведомлений о смене тренда
		lastIndicatorAt:    make(map[string]time.Time),
		trendChangeCounter: make(map[string]int),
	}
}

func (t *Trader) Run(ctx context.Context, interval time.Duration) {
	log.Log.Debug(fmt.Sprintf("[Trader %s] Начало торговли (tf=%s)", t.cfg.APIKey, interval))

	if manager, err := initStatsManager(configs.BotCurrentConfig.StatsFile, configs.BotCurrentConfig.InitialBank); err != nil {
		log.Log.Error("Не удалось инициализировать статистику", "error", err)
	} else {
		t.statsManager = manager
		t.startDailyReporter(ctx)
	}

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
			t.actedOnTrend[instId] = new(bool)
			*t.actedOnTrend[instId] = b
		} else {
			t.lastIsUptrend[instId] = nil
			log.Log.Debug(fmt.Sprintf("[Trader %s][%s] lastIsUptrend оставляем nil (нет данных в кэше)", t.cfg.APIKey, instId))
			t.actedOnTrend[instId] = nil
		}
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

	exitCfg := resolveExitSettingsForTF(ltfTF)

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
		if t.actedOnTrend[instId] == nil {
			t.actedOnTrend[instId] = new(bool)
		}
		*t.actedOnTrend[instId] = ltfData.IsUptrend
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
		t.closePosition(instId, "расхождение трендов LTF/HTF")
	}

	if t.isPositionOpen[instId] {
		t.manageOpenPosition(instId, price, ltfData, ltfTrend)
	}

	aligned := ltfTrend == htfTrend
	if aligned && !t.isPositionOpen[instId] {
		dirUp := ltfTrend == 1
		alreadyActed := t.actedOnTrend[instId] != nil && *t.actedOnTrend[instId] == dirUp
		if ltfChanged || !alreadyActed {
			riskPercent := configs.BotCurrentConfig.RiskPercent
			fixedUSDT := configs.BotCurrentConfig.PositionSizeUSDT
			if riskPercent <= 0 && fixedUSDT <= 0 {
				log.Log.Error(fmt.Sprintf("[Trader %s][%s] Не задан размер позиции (risk_percent/position_size_usdt)", t.cfg.APIKey, instId))
				return nil
			}

			tradeSize, err := t.Client.GetTradeSize(instId, configs.BotCurrentConfig.CCY, riskPercent, fixedUSDT, price)
			if err != nil {
				log.Log.Error(fmt.Sprintf("[Trader %s][%s] Не удалось получить tradeSize: %v", t.cfg.APIKey, instId, err))
			} else {
				if dirUp {
					t.openLong(instId, tradeSize, price, ltfData, exitCfg)
				} else {
					t.openShort(instId, tradeSize, price, ltfData, exitCfg)
				}
			}
		}
	}

	*t.lastIsUptrend[instId] = ltfData.IsUptrend
	t.lastIndicatorAt[instId] = time.Now()
	return nil
}

func (t *Trader) manageOpenPosition(instId string, price float64, ltfData cache.IndicatorData, ltfTrend int) {
	state, ok := t.positionState[instId]
	if !ok || state == nil {
		return
	}

	pos := t.positions[instId]
	exitCfg := state.Exit
	atr := ltfData.ATR

	if pos.PosSide == "long" {
		if price > state.MaxPrice {
			state.MaxPrice = price
		}
		if state.MinPrice == 0 || price < state.MinPrice {
			state.MinPrice = price
		}
	} else {
		if state.MaxPrice == 0 || price > state.MaxPrice {
			state.MaxPrice = price
		}
		if state.MinPrice == 0 || price < state.MinPrice {
			state.MinPrice = price
		}
	}

	// Частичный выход и перевод стопа в безубыток
    simpleMode := exitCfg.SimpleMode

    if !simpleMode && exitCfg.PartialTakeProfitR > 0 && state.RiskPerUnit > 0 && !state.BreakEvenActivated {
        target := exitCfg.PartialTakeProfitR * state.RiskPerUnit
        achieved := false
        if pos.PosSide == "long" && price-state.EntryPrice >= target {
            achieved = true
        }
        if pos.PosSide == "short" && state.EntryPrice-price >= target {
            achieved = true
        }
        if achieved {
            t.moveStopToBreakEven(instId, state)
            log.Log.Info(fmt.Sprintf("[Trader %s][%s] Стоп перенесён в безубыток по правилу +%.2fR", t.cfg.APIKey, instId, exitCfg.PartialTakeProfitR))
        }
    }

    if !t.isPositionOpen[instId] {
        return
    }

    // Правило отдачи прибыли (только для расширенного режима)
    if !simpleMode && exitCfg.GivebackTriggerR > 0 && exitCfg.GivebackAmountR > 0 && state.RiskPerUnit > 0 {
        var mfe, giveback float64
        if pos.PosSide == "long" {
            mfe = state.MaxPrice - state.EntryPrice
            giveback = state.MaxPrice - price
        } else {
			mfe = state.EntryPrice - state.MinPrice
			giveback = price - state.MinPrice
		}

		if mfe >= exitCfg.GivebackTriggerR*state.RiskPerUnit && giveback >= exitCfg.GivebackAmountR*state.RiskPerUnit {
			t.closePosition(instId, "правило отдачи прибыли")
			return
        }
    }

    // Выход по LTF flip + буферу
    if atr > 0 && exitCfg.FlipBufferATR > 0 {
		buffer := atr * exitCfg.FlipBufferATR
		if pos.PosSide == "long" {
			if ltfTrend < 0 || price < ltfData.Supertrend-buffer {
				t.closePosition(instId, "LTF flip + buffer")
				return
			}
		} else {
			if ltfTrend > 0 || price > ltfData.Supertrend+buffer {
				t.closePosition(instId, "LTF flip + buffer")
				return
			}
		}
	} else {
		if pos.PosSide == "long" && ltfTrend < 0 {
			t.closePosition(instId, "LTF flip")
			return
		}
		if pos.PosSide == "short" && ltfTrend > 0 {
			t.closePosition(instId, "LTF flip")
			return
        }
    }

    if simpleMode {
        return
    }

    // Временной стоп
    if shouldTriggerTimeStop(state, exitCfg.TimeStopUTC) {
        t.closePosition(instId, "временной стоп")
        return
    }

    if exitCfg.TimeStopHours > 0 && exitCfg.TimeStopMinR > 0 && state.RiskPerUnit > 0 {
        duration := time.Since(state.EntryTime).Hours()
        var unrealized float64
        if pos.PosSide == "long" {
            unrealized = price - state.EntryPrice
        } else {
            unrealized = state.EntryPrice - price
        }
        if duration >= exitCfg.TimeStopHours && unrealized < exitCfg.TimeStopMinR*state.RiskPerUnit {
            t.closePosition(instId, fmt.Sprintf("позиция не достигла %.2fR за %.1fч", exitCfg.TimeStopMinR, exitCfg.TimeStopHours))
            return
        }
    }
}

func (t *Trader) openLong(instId string, tradeSize, price float64, ltfData cache.IndicatorData, exitCfg configs.ExitSettings) {
	stopLossPrice := t.calculateInitialStop(price, ltfData.ATR, true, exitCfg)
	riskPerUnit := price - stopLossPrice
	if riskPerUnit <= 0 {
		riskPerUnit = price * 0.01
	}
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
	t.positionState[instId] = newPositionState(price, stopLossPrice, riskPerUnit, exitCfg, "long")

	t.isPositionOpen[instId] = true
	t.bestStopPrice[instId] = stopLossPrice
	t.actedOnTrend[instId] = new(bool)
	*t.actedOnTrend[instId] = true

	log.Log.Info(fmt.Sprintf("[Trader %s][%s] Открыт LONG: Entry=%.6f StopLoss=%.6f", t.cfg.APIKey, instId, price, stopLossPrice))
	t.notifyPositionOpened(instId, "long", tradeSize, price, stopLossPrice)
}

func (t *Trader) openShort(instId string, tradeSize, price float64, ltfData cache.IndicatorData, exitCfg configs.ExitSettings) {
	stopLossPrice := t.calculateInitialStop(price, ltfData.ATR, false, exitCfg)
	riskPerUnit := stopLossPrice - price
	if riskPerUnit <= 0 {
		riskPerUnit = price * 0.01
	}
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
	t.positionState[instId] = newPositionState(price, stopLossPrice, riskPerUnit, exitCfg, "short")

	t.isPositionOpen[instId] = true
	t.bestStopPrice[instId] = stopLossPrice
	t.actedOnTrend[instId] = new(bool)
	*t.actedOnTrend[instId] = false

	log.Log.Info(fmt.Sprintf("[Trader %s][%s] Открыт SHORT: Entry=%.6f StopLoss=%.6f", t.cfg.APIKey, instId, price, stopLossPrice))
	t.notifyPositionOpened(instId, "short", tradeSize, price, stopLossPrice)
}

func (t *Trader) moveStopToBreakEven(instId string, state *positionState) {
	pos := t.positions[instId]
	buffer := state.Exit.BreakEvenBuffer
	if buffer < 0 {
		buffer = 0
	}

	var newStop float64
	if pos.PosSide == "long" {
		newStop = state.EntryPrice + buffer
		if newStop < state.InitialStop {
			newStop = state.InitialStop
		}
	} else {
		newStop = state.EntryPrice - buffer
		if newStop > state.InitialStop {
			newStop = state.InitialStop
		}
	}

	t.bestStopPrice[instId] = newStop
	pos.StopLossPrice = newStop
	t.positions[instId] = pos
	state.InitialStop = newStop
	state.BreakEvenActivated = true

	log.Log.Info(fmt.Sprintf("[Trader %s][%s] Стоп перенесён в безубыток: %.6f", t.cfg.APIKey, instId, newStop))
}

func shouldTriggerTimeStop(state *positionState, times []string) bool {
	if len(times) == 0 {
		return false
	}

	now := time.Now().UTC()
	datePrefix := now.Format("2006-01-02")

	for _, ts := range times {
		parsed, err := time.Parse("15:04", ts)
		if err != nil {
			continue
		}

		stopTime := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, time.UTC)
		if now.Before(stopTime) || state.EntryTime.After(stopTime) {
			continue
		}

		key := datePrefix + "_" + ts
		if state.LastTimeStopKey == key {
			continue
		}

		state.LastTimeStopKey = key
		return true
	}

	return false
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

		ltfTrend := data.Trend
		if ltfTrend == 0 {
			ltfTrend = boolToTrend(data.IsUptrend)
		}

		t.manageOpenPosition(instId, price, data, ltfTrend)
		if !t.isPositionOpen[instId] {
			continue
		}

		state := t.positionState[instId]
		if state == nil {
			continue
		}

		atr := data.ATR
		if atr <= 0 {
			continue
		}

		pos := t.positions[instId]
		dir := 1.0
		if pos.PosSide == "short" {
			dir = -1
		}

		if !state.Exit.SimpleMode {
			trailMultiplier := state.Exit.TrailingATR
			if trailMultiplier <= 0 {
				trailMultiplier = configs.BotCurrentConfig.ATRMultiplierStop
				if trailMultiplier <= 0 {
					trailMultiplier = 1.5
				}
			}

			if state.BreakEvenActivated {
				var newStopPrice float64
				if pos.PosSide == "long" {
					newStopPrice = price - atr*trailMultiplier
					if newStopPrice > t.bestStopPrice[instId] {
						oldStop := t.bestStopPrice[instId]
						t.bestStopPrice[instId] = newStopPrice
						pos.StopLossPrice = newStopPrice
						t.positions[instId] = pos
						log.Log.Info(fmt.Sprintf("[Trader %s][%s] Трейлинг стоп поднят: %.6f → %.6f", t.cfg.APIKey, instId, oldStop, newStopPrice))
					}
				} else {
					newStopPrice = price + atr*trailMultiplier
					if newStopPrice < t.bestStopPrice[instId] || t.bestStopPrice[instId] == 0 {
						oldStop := t.bestStopPrice[instId]
						t.bestStopPrice[instId] = newStopPrice
						pos.StopLossPrice = newStopPrice
						t.positions[instId] = pos
						log.Log.Info(fmt.Sprintf("[Trader %s][%s] Трейлинг стоп опущен: %.6f → %.6f", t.cfg.APIKey, instId, oldStop, newStopPrice))
					}
				}
			}
		}

		if dir*(price-t.bestStopPrice[instId]) <= 0 {
			log.Log.Info(fmt.Sprintf("[Trader %s][%s] Закрыт %s по стоп-лоссу: Entry=%.6f Stop=%.6f Price=%.6f", t.cfg.APIKey, instId, pos.PosSide, pos.EntryPrice, t.bestStopPrice[instId], price))
			t.closePosition(instId, "сработал трейлинг-стоп")
		}
	}
}

func (t *Trader) calculateInitialStop(price, atr float64, isUptrend bool, exitCfg configs.ExitSettings) float64 {
	multiplier := exitCfg.InitialSLATR
	if multiplier <= 0 {
		multiplier = configs.BotCurrentConfig.ATRMultiplierStop
		if multiplier <= 0 {
			multiplier = 1.5
		}
	}

	if atr <= 0 {
		if isUptrend {
			return price * 0.992
		}
		return price * 1.008
	}

	distance := atr * multiplier
	if isUptrend {
		return price - distance
	}
	return price + distance
}

func (t *Trader) closePosition(instId string, reason string) {
	if !t.isPositionOpen[instId] {
		log.Log.Warn(fmt.Sprintf("[Trader %s][%s] closePosition вызван, но позиции нет", t.cfg.APIKey, instId))
		return
	}

	position := t.positions[instId]

	side := "sell"
	dir := 1.0
	if position.PosSide == "short" {
		side = "buy"
		dir = -1
	}

	entry := position.EntryPrice
	size := position.TradeSize

	price, ok := cache.Get().GetPrice(instId)
	if !ok {
		log.Log.Warn(fmt.Sprintf("[Trader %s][%s] Не удалось получить цену для расчёта PnL при закрытии, используем entry", t.cfg.APIKey, instId))
		price = entry
	}

	pnl := 100.0 * (price - entry) / entry * dir
	log.Log.Info(fmt.Sprintf("[Trader %s][%s] Закрытие позиции: PosSide=%s Entry=%.6f Current=%.6f Size=%.6f PnL=%.3f%% Reason=%s", t.cfg.APIKey, instId, position.PosSide, entry, price, size, pnl, reason))

	if err := t.Client.PlaceOrder(position.InstId, side, position.PosSide, size); err == nil {
		log.Log.Debug(fmt.Sprintf("[Trader %s][%s] Позиция закрыта успешно", t.cfg.APIKey, instId))
		t.notifyPositionClosed(instId, position.PosSide, size, entry, price, pnl, reason)
		if t.statsManager != nil {
			if pnl > 0 {
				t.statsManager.RecordTrade(true)
			} else if pnl < 0 {
				t.statsManager.RecordTrade(false)
			}
		}
		t.isPositionOpen[instId] = false
		delete(t.positions, instId)
		delete(t.bestStopPrice, instId)
		delete(t.positionState, instId)
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
		if t.isPositionOpen[instId] {
			t.closePosition(instId, "остановка бота")
		}
	}
}

func (t *Trader) startDailyReporter(ctx context.Context) {
	if t.statsManager == nil {
		return
	}

	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		loc = time.FixedZone("MSK", 3*60*60)
	}

	ticker := time.NewTicker(time.Minute)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				moscowNow := now.In(loc)
				if moscowNow.Hour() == 1 && moscowNow.Minute() == 0 {
					t.sendDailyReport(moscowNow)
				}
			}
		}
	}()
}

func (t *Trader) sendDailyReport(now time.Time) {
	if t.statsManager == nil {
		return
	}

	balance, err := t.Client.GetAccountBalance(configs.BotCurrentConfig.CCY)
	if err != nil {
		log.Log.Error(fmt.Sprintf("[Trader %s] Не удалось получить баланс для дневного отчёта: %v", t.cfg.APIKey, err))
		return
	}

	message, produced, err := t.statsManager.BuildDailyReport(now, balance, configs.BotCurrentConfig.CCY)
	if err != nil {
		log.Log.Error(fmt.Sprintf("[Trader %s] Ошибка при подготовке дневного отчёта: %v", t.cfg.APIKey, err))
		return
	}

	if !produced || message == "" {
		return
	}

	log.Log.Info(fmt.Sprintf("[Trader %s] Дневной отчёт отправлен", t.cfg.APIKey), "balance", balance)
	if t.shouldNotify() {
		notifier.SendTelegramMessage(message)
	}
}

type positionState struct {
	Exit               configs.ExitSettings
	RiskPerUnit        float64
	EntryPrice         float64
	InitialStop        float64
	BreakEvenBuffer    float64
	BreakEvenActivated bool
	MaxPrice           float64
	MinPrice           float64
	EntryTime          time.Time
	Side               string
	LastTimeStopKey    string
}

func newPositionState(entry, stop, risk float64, exitCfg configs.ExitSettings, side string) *positionState {
	st := &positionState{
		Exit:            exitCfg,
		RiskPerUnit:     risk,
		EntryPrice:      entry,
		InitialStop:     stop,
		BreakEvenBuffer: exitCfg.BreakEvenBuffer,
		EntryTime:       time.Now().UTC(),
		Side:            side,
	}

	st.MaxPrice = entry
	st.MinPrice = entry
	return st
}

func resolveExitSettingsForTF(tf string) configs.ExitSettings {
	if configs.BotCurrentConfig.TimeframeSettings != nil {
		if setting, ok := configs.BotCurrentConfig.TimeframeSettings[tf]; ok && setting.ExitSettings != nil {
			return sanitizeExitSettings(*setting.ExitSettings)
		}
	}
	return sanitizeExitSettings(configs.ExitSettings{})
}

func sanitizeExitSettings(exitCfg configs.ExitSettings) configs.ExitSettings {
	if exitCfg.InitialSLATR <= 0 {
		exitCfg.InitialSLATR = 1.8
	}
	if exitCfg.SimpleMode {
		exitCfg.PartialTakeProfitR = 0
		exitCfg.TrailingATR = 0
		exitCfg.GivebackTriggerR = 0
		exitCfg.GivebackAmountR = 0
		exitCfg.TimeStopUTC = nil
		exitCfg.TimeStopHours = 0
		exitCfg.TimeStopMinR = 0
		exitCfg.BreakEvenBuffer = 0
	} else {
		if exitCfg.PartialTakeProfitR <= 0 {
			exitCfg.PartialTakeProfitR = 1.0
		}
		if exitCfg.TrailingATR <= 0 {
			exitCfg.TrailingATR = 1.6
		}
		if exitCfg.GivebackTriggerR <= 0 {
			exitCfg.GivebackTriggerR = 1.2
		}
		if exitCfg.GivebackAmountR <= 0 {
			exitCfg.GivebackAmountR = 0.6
		}
		if exitCfg.BreakEvenBuffer < 0 {
			exitCfg.BreakEvenBuffer = 0
		}
	}
	if exitCfg.FlipBufferATR < 0 {
		exitCfg.FlipBufferATR = 0
	}
	return exitCfg
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

func (t *Trader) notifyPositionOpened(instId, posSide string, size, entry, stop float64) {
	if !t.shouldNotify() {
		return
	}

	sideLabel := strings.ToUpper(posSide)
	message := fmt.Sprintf("🔔 *Открыт %s*\n• Инструмент: %s\n• Размер: %.4f\n• Вход: %.6f\n• Стоп: %.6f",
		sideLabel, instId, size, entry, stop)

	if balanceLine, ok := t.balanceLine(); ok {
		message += "\n" + balanceLine
	}

	notifier.SendTelegramMessage(message)
}

func (t *Trader) notifyPositionClosed(instId, posSide string, size, entry, price, pnl float64, reason string) {
	if !t.shouldNotify() {
		return
	}

	sideLabel := strings.ToUpper(posSide)
	pnlStr := fmt.Sprintf("%+.3f%%", pnl)
	resultMarker := "⚖️"
	if pnl > 0 {
		resultMarker = "✅"
	} else if pnl < 0 {
		resultMarker = "❌"
	}

	message := fmt.Sprintf("%s *Закрыт %s*\n• Инструмент: %s\n• Размер: %.4f\n• Вход: %.6f\n• Выход: %.6f\n• Результат: %s",
		resultMarker, sideLabel, instId, size, entry, price, pnlStr)

	if reason != "" {
		message += "\n• Причина: " + reason
	}

	if balanceLine, ok := t.balanceLine(); ok {
		message += "\n" + balanceLine
	}

	notifier.SendTelegramMessage(message)
}

func (t *Trader) shouldNotify() bool {
	cfg := configs.BotCurrentConfig
	return cfg.TelegramEnabled && cfg.TelegramBotToken != "" && cfg.TelegramChatID != ""
}

func (t *Trader) balanceLine() (string, bool) {
	balance, err := t.Client.GetAccountBalance(configs.BotCurrentConfig.CCY)
	if err != nil {
		log.Log.Error("Не удалось получить баланс для уведомления",
			"trader", t.cfg.APIKey,
			"error", err,
		)
		return "", false
	}

	line := fmt.Sprintf("Баланс: %.2f %s", balance, configs.BotCurrentConfig.CCY)
	return line, true
}
