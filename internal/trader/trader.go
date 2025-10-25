package trader

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"time"

	"github.com/kuromii5/supertrend_trade_bot/configs"
	"github.com/kuromii5/supertrend_trade_bot/internal/cache"
	"github.com/kuromii5/supertrend_trade_bot/internal/exchanger"
	"github.com/kuromii5/supertrend_trade_bot/internal/exchanger/okx"
	"github.com/kuromii5/supertrend_trade_bot/internal/models"
)

type direction string

const (
	directionNone  direction = "none"
	directionLong  direction = "long"
	directionShort direction = "short"
)

type instrumentState struct {
	lastProcessed     map[string]int64
	armedDirection    direction
	armedBarsLeft     int
	armedMin          float64
	armedMax          float64
	cooldownBars      int
	position          *models.Position
	trailingActive    bool
	reentryBlockDir   direction
	reentryBlockPrice float64
	lastExitReason    string
	lastExitPrice     float64
}

type Trader struct {
	cfg    configs.TraderConfig
	Client exchanger.TradingAccount
	states map[string]*instrumentState
}

func NewTrader(cfg configs.TraderConfig) *Trader {
	client := okx.NewClient(cfg.APIKey, cfg.APISecret, cfg.Passphrase)

	return &Trader{
		cfg:    cfg,
		Client: client,
		states: make(map[string]*instrumentState),
	}
}

func (t *Trader) Run(ctx context.Context, interval time.Duration) {
	tickerTrade := time.NewTicker(interval)
	defer tickerTrade.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tickerTrade.C:
			t.trade()
		}
	}
}

func (t *Trader) trade() {
	entryTF := configs.BotCurrentConfig.EntryTimeframe
	trendTF := configs.BotCurrentConfig.TrendTimeframe

	for _, instId := range configs.BotCurrentConfig.TradingPairs {
		state := t.getState(instId)

		entryData, ok := cache.Get().GetIndicatorData(instId, entryTF)
		if !ok || entryData.Timestamp == 0 {
			log.Printf("[Trader %s][%s] Нет данных по таймфрейму %s", t.cfg.APIKey, instId, entryTF)
			continue
		}

		if state.lastProcessed[entryTF] != 0 && entryData.Timestamp <= state.lastProcessed[entryTF] {
			continue
		}
		state.lastProcessed[entryTF] = entryData.Timestamp

		var trendData *cache.IndicatorData
		if configs.BotCurrentConfig.UseMTF && trendTF != "" {
			trendSnapshot, ok := cache.Get().GetIndicatorData(instId, trendTF)
			if !ok || trendSnapshot.Timestamp == 0 {
				log.Printf("[Trader %s][%s] Нет данных по старшему ТФ %s", t.cfg.APIKey, instId, trendTF)
				continue
			}
			trendData = &trendSnapshot
			state.lastProcessed[trendTF] = trendSnapshot.Timestamp
		}

		t.advanceCounters(instId, state)
		t.handleBar(instId, state, entryData, trendData)
	}
}

func (t *Trader) handleBar(instId string, state *instrumentState, entry cache.IndicatorData, trend *cache.IndicatorData) {
	t.syncArming(instId, state, entry)

	if state.position != nil {
		t.managePosition(instId, state, entry)
		return
	}

	t.evaluateEntries(instId, state, entry, trend)
}

func (t *Trader) advanceCounters(instId string, state *instrumentState) {
	if state.cooldownBars > 0 {
		state.cooldownBars--
	}

	if state.armedDirection != directionNone && state.armedBarsLeft > 0 {
		state.armedBarsLeft--
		if state.armedBarsLeft == 0 {
			t.clearArmed(instId, state, "armed-expired")
		}
	}
}

func (t *Trader) syncArming(instId string, state *instrumentState, entry cache.IndicatorData) {
	dir := directionFromTrend(entry.IsUptrend)
	if entry.SupertrendFlipped {
		t.arm(instId, state, dir, entry.Close)
	}

	if state.armedDirection == directionNone {
		return
	}

	if dir != state.armedDirection {
		t.clearArmed(instId, state, "armed-flip")
		return
	}

	if entry.Close < state.armedMin || entry.Close > state.armedMax {
		t.clearArmed(instId, state, "armed-price-window")
	}
}

func (t *Trader) arm(instId string, state *instrumentState, dir direction, price float64) {
	state.armedDirection = dir
	state.armedBarsLeft = configs.BotCurrentConfig.ArmedBarsToLive
	if state.armedBarsLeft == 0 {
		state.armedBarsLeft = 1
	}
	tolerance := configs.BotCurrentConfig.ReentryTolerance
	state.armedMin = price * (1 - tolerance)
	state.armedMax = price * (1 + tolerance)
	t.logEvent(instId, map[string]interface{}{
		"action":    "armed_set",
		"direction": dir,
		"reason":    "supertrend-flip",
		"price":     price,
	})
}

func (t *Trader) clearArmed(instId string, state *instrumentState, reason string) {
	if state.armedDirection == directionNone {
		return
	}
	t.logEvent(instId, map[string]interface{}{
		"action":    "armed_clear",
		"direction": state.armedDirection,
		"reason":    reason,
	})
	state.armedDirection = directionNone
	state.armedBarsLeft = 0
	state.armedMin = 0
	state.armedMax = 0
}

func (t *Trader) managePosition(instId string, state *instrumentState, entry cache.IndicatorData) {
	t.updateTrailing(instId, state, entry)

	pos := state.position
	if pos == nil {
		return
	}

	switch {
	case hitHardSL(pos, entry.Close):
		t.exitPosition(instId, state, entry, "hardSL")
	case configs.BotCurrentConfig.AllowSupertrendFlipExit && trendAgainstPosition(pos, entry.IsUptrend):
		t.exitPosition(instId, state, entry, "stFlip")
	case configs.BotCurrentConfig.AllowMacdExit && macdZeroAgainstPosition(pos, entry) && emaConfirmsExit(pos, entry):
		t.exitPosition(instId, state, entry, "macdZero")
	case configs.BotCurrentConfig.AllowTrailingExit && state.trailingActive && trailHit(pos, entry.Close):
		t.exitPosition(instId, state, entry, "trailing")
	}
}

func (t *Trader) evaluateEntries(instId string, state *instrumentState, entry cache.IndicatorData, trend *cache.IndicatorData) {
	if state.cooldownBars > 0 {
		return
	}

	metrics := defaultMetrics(entry)

	longOk := baseLongOk(entry, trend)
	shortOk := baseShortOk(entry, trend)

	if longOk && t.canEnter(instId, state, directionLong, entry) {
		if t.enterPosition(instId, state, entry, directionLong, "st+ema+rsi+adx", metrics) {
			return
		}
	}

	if shortOk && t.canEnter(instId, state, directionShort, entry) {
		if t.enterPosition(instId, state, entry, directionShort, "st+ema+rsi+adx", metrics) {
			return
		}
	}

	if configs.BotCurrentConfig.EnableAlternativeEntry {
		if altLongOk(entry) && t.canEnter(instId, state, directionLong, entry) {
			if t.enterPosition(instId, state, entry, directionLong, "ema-rsi-macd", metrics) {
				return
			}
		}
		if altShortOk(entry) && t.canEnter(instId, state, directionShort, entry) {
			if t.enterPosition(instId, state, entry, directionShort, "ema-rsi-macd", metrics) {
				return
			}
		}
	}

	if state.armedDirection != directionNone && state.armedBarsLeft <= 0 {
		t.clearArmed(instId, state, "armed-expired")
	}
}

func (t *Trader) canEnter(instId string, state *instrumentState, dir direction, entry cache.IndicatorData) bool {
	if configs.BotCurrentConfig.UseArmedOnly && state.armedDirection != dir {
		return false
	}

	if state.reentryBlockDir == dir && state.reentryBlockPrice > 0 {
		move := math.Abs((entry.Close - state.reentryBlockPrice) / state.reentryBlockPrice)
		if move < configs.BotCurrentConfig.MinReentryPct {
			return false
		}
		state.reentryBlockDir = directionNone
		state.reentryBlockPrice = 0
	}

	if state.armedDirection == dir {
		if entry.Close < state.armedMin || entry.Close > state.armedMax {
			return false
		}
	}

	return true
}

func (t *Trader) enterPosition(instId string, state *instrumentState, entry cache.IndicatorData, dir direction, reason string, metrics map[string]interface{}) bool {
	orderSide := "buy"
	posSide := "long"
	if dir == directionShort {
		orderSide = "sell"
		posSide = "short"
	}

	tradeSize, err := t.Client.GetTradeSize(instId, configs.BotCurrentConfig.CCY, configs.BotCurrentConfig.RiskPercent, entry.Close)
	if err != nil {
		log.Printf("[Trader %s][%s] Не удалось рассчитать размер позиции: %v", t.cfg.APIKey, instId, err)
		return false
	}

	if err = t.Client.PlaceOrder(instId, orderSide, posSide, tradeSize); err != nil {
		log.Printf("[Trader %s][%s] Ошибка при открытии %s: %v", t.cfg.APIKey, instId, posSide, err)
		return false
	}

	pos := &models.Position{
		InstId:       instId,
		PosSide:      posSide,
		TradeSize:    tradeSize,
		EntryPrice:   entry.Close,
		EntryTime:    time.UnixMilli(entry.Timestamp),
		HardSL:       calcHardSL(entry.Close, dir),
		TPTrigger:    calcTP(entry.Close, dir),
		TrailStop:    0,
		TrailExtreme: entry.Close,
	}

	state.position = pos
	state.trailingActive = false
	state.reentryBlockDir = directionNone
	state.reentryBlockPrice = 0
	state.lastExitReason = ""
	state.lastExitPrice = 0
	state.cooldownBars = configs.BotCurrentConfig.CooldownBars
	t.clearArmed(instId, state, "entered")

	payload := cloneMetrics(metrics)
	payload["action"] = "enter"
	payload["side"] = posSide
	payload["reason"] = reason
	payload["time"] = time.UnixMilli(entry.Timestamp).UTC().Format(time.RFC3339)
	payload["price"] = entry.Close
	payload["sl"] = pos.HardSL
	payload["tp"] = pos.TPTrigger
	t.logEvent(instId, payload)

	return true
}

func (t *Trader) updateTrailing(instId string, state *instrumentState, entry cache.IndicatorData) {
	pos := state.position
	if pos == nil {
		return
	}

	move := pnlSinceEntry(pos, entry.Close)
	if !state.trailingActive && move >= configs.BotCurrentConfig.TrailActivatePct {
		state.trailingActive = true
		pos.TrailExtreme = entry.Close
		pos.TrailStop = computeTrailStop(pos, entry)
		t.logEvent(instId, map[string]interface{}{
			"action": "trailing_armed",
			"side":   pos.PosSide,
			"price":  entry.Close,
			"stop":   pos.TrailStop,
		})
		return
	}

	if !state.trailingActive {
		return
	}

	updated := false
	if pos.PosSide == "long" {
		threshold := pos.TrailExtreme * (1 + configs.BotCurrentConfig.TrailStepPct)
		if entry.Close >= threshold {
			pos.TrailExtreme = entry.Close
			newStop := computeTrailStop(pos, entry)
			if newStop > pos.TrailStop {
				pos.TrailStop = newStop
				updated = true
			}
		}
	} else {
		threshold := pos.TrailExtreme * (1 - configs.BotCurrentConfig.TrailStepPct)
		if entry.Close <= threshold {
			pos.TrailExtreme = entry.Close
			newStop := computeTrailStop(pos, entry)
			if newStop < pos.TrailStop || pos.TrailStop == 0 {
				pos.TrailStop = newStop
				updated = true
			}
		}
	}

	if updated {
		t.logEvent(instId, map[string]interface{}{
			"action": "trailing_update",
			"side":   pos.PosSide,
			"stop":   pos.TrailStop,
		})
	}
}

func (t *Trader) exitPosition(instId string, state *instrumentState, entry cache.IndicatorData, reason string) {
	pos := state.position
	if pos == nil {
		return
	}

	side := oppositeOrderSide(pos.PosSide)
	if err := t.Client.PlaceOrder(instId, side, pos.PosSide, pos.TradeSize); err != nil {
		log.Printf("[Trader %s][%s] Ошибка при закрытии позиции: %v", t.cfg.APIKey, instId, err)
		return
	}

	pnl := pnlSinceEntry(pos, entry.Close)

	payload := defaultMetrics(entry)
	payload["action"] = "exit"
	payload["side"] = pos.PosSide
	payload["reason"] = reason
	payload["time"] = time.UnixMilli(entry.Timestamp).UTC().Format(time.RFC3339)
	payload["price"] = entry.Close
	payload["pnl"] = pnl * 100
	payload["sl"] = pos.HardSL
	payload["tp"] = pos.TPTrigger
	t.logEvent(instId, payload)

	state.position = nil
	state.trailingActive = false
	state.lastExitReason = reason
	state.lastExitPrice = entry.Close

	if reason == "trailing" || reason == "macdZero" {
		state.reentryBlockDir = directionFromPosSide(pos.PosSide)
		state.reentryBlockPrice = entry.Close
	} else {
		state.reentryBlockDir = directionNone
		state.reentryBlockPrice = 0
	}

	if reason != "hardSL" {
		state.cooldownBars = configs.BotCurrentConfig.CooldownBars
	}
}

func (t *Trader) Stop() {
	for instId, state := range t.states {
		if state.position != nil {
			log.Printf("[Trader %s][%s] Закрытие позиции", t.cfg.APIKey, instId)
			t.exitPosition(instId, state, cache.IndicatorData{Close: state.position.EntryPrice, Timestamp: time.Now().UnixMilli()}, "shutdown")
		}
	}
}

func (t *Trader) getState(instId string) *instrumentState {
	state, ok := t.states[instId]
	if !ok {
		state = &instrumentState{lastProcessed: make(map[string]int64)}
		t.states[instId] = state
	}
	return state
}

func (t *Trader) logEvent(instId string, payload map[string]interface{}) {
	payload["instId"] = instId
	payload["account"] = t.cfg.APIKey
	data, _ := json.Marshal(payload)
	log.Printf("[Trader %s][%s] %s", t.cfg.APIKey, instId, string(data))
}

func defaultMetrics(entry cache.IndicatorData) map[string]interface{} {
	trend := "short"
	if entry.IsUptrend {
		trend = "long"
	}
	return map[string]interface{}{
		"price":    entry.Close,
		"rsi":      entry.RSI,
		"adx":      entry.ADX,
		"fastEma":  entry.EMAFast,
		"slowEma":  entry.EMASlow,
		"macdHist": entry.MACD.SmoothedHistogram,
		"stTrend":  trend,
	}
}

func cloneMetrics(m map[string]interface{}) map[string]interface{} {
	copy := make(map[string]interface{}, len(m))
	for k, v := range m {
		copy[k] = v
	}
	return copy
}

func baseLongOk(entry cache.IndicatorData, trend *cache.IndicatorData) bool {
	if !entry.IsUptrend {
		return false
	}
	if entry.EMAFast <= entry.EMASlow {
		return false
	}
	if entry.RSI > configs.BotCurrentConfig.RSIMaxLong {
		return false
	}
	if entry.ADX < configs.BotCurrentConfig.ADXMin {
		return false
	}
	return mtfAllows(directionLong, trend)
}

func baseShortOk(entry cache.IndicatorData, trend *cache.IndicatorData) bool {
	if entry.IsUptrend {
		return false
	}
	if entry.EMAFast >= entry.EMASlow {
		return false
	}
	if entry.RSI < configs.BotCurrentConfig.RSIMinShort {
		return false
	}
	if entry.ADX < configs.BotCurrentConfig.ADXMin {
		return false
	}
	return mtfAllows(directionShort, trend)
}

func altLongOk(entry cache.IndicatorData) bool {
	if entry.EMAFast <= entry.EMASlow {
		return false
	}
	if entry.RSI > 65 {
		return false
	}
	adxThreshold := math.Max(configs.BotCurrentConfig.ADXMin, 20)
	if entry.ADX < adxThreshold {
		return false
	}
	if configs.BotCurrentConfig.AlternativeRequireSupertrend && !entry.IsUptrend {
		return false
	}
	crossUp := entry.MACD.Histogram > 0 && entry.MACD.PrevHistogram <= 0
	if configs.BotCurrentConfig.MACDRequireTwoBars {
		crossUp = crossUp && entry.MACD.SmoothedHistogram > 0 && entry.MACD.PrevSmoothedHistogram > 0
	}
	return crossUp
}

func altShortOk(entry cache.IndicatorData) bool {
	if entry.EMAFast >= entry.EMASlow {
		return false
	}
	if entry.RSI < 35 {
		return false
	}
	adxThreshold := math.Max(configs.BotCurrentConfig.ADXMin, 20)
	if entry.ADX < adxThreshold {
		return false
	}
	if configs.BotCurrentConfig.AlternativeRequireSupertrend && entry.IsUptrend {
		return false
	}
	crossDown := entry.MACD.Histogram < 0 && entry.MACD.PrevHistogram >= 0
	if configs.BotCurrentConfig.MACDRequireTwoBars {
		crossDown = crossDown && entry.MACD.SmoothedHistogram < 0 && entry.MACD.PrevSmoothedHistogram < 0
	}
	return crossDown
}

func mtfAllows(dir direction, trend *cache.IndicatorData) bool {
	if !configs.BotCurrentConfig.UseMTF || trend == nil {
		return true
	}
	if dir == directionLong {
		return trend.IsUptrend
	}
	return !trend.IsUptrend
}

func calcHardSL(price float64, dir direction) float64 {
	if dir == directionLong {
		return price * (1 - configs.BotCurrentConfig.InitSLPct)
	}
	return price * (1 + configs.BotCurrentConfig.InitSLPct)
}

func calcTP(price float64, dir direction) float64 {
	if dir == directionLong {
		return price * (1 + configs.BotCurrentConfig.TPPct)
	}
	return price * (1 - configs.BotCurrentConfig.TPPct)
}

func pnlSinceEntry(pos *models.Position, price float64) float64 {
	if pos.PosSide == "long" {
		return (price - pos.EntryPrice) / pos.EntryPrice
	}
	return (pos.EntryPrice - price) / pos.EntryPrice
}

func computeTrailStop(pos *models.Position, entry cache.IndicatorData) float64 {
	atrValue := entry.ATRTrailing
	if atrValue == 0 {
		atrValue = entry.ATR
	}
	switch configs.BotCurrentConfig.TrailMode {
	case "nbar_low_high":
		if pos.PosSide == "long" {
			return entry.NBarLow
		}
		return entry.NBarHigh
	default:
		if pos.PosSide == "long" {
			return entry.Close - atrValue*configs.BotCurrentConfig.ATRMultiplierTrailing
		}
		return entry.Close + atrValue*configs.BotCurrentConfig.ATRMultiplierTrailing
	}
}

func hitHardSL(pos *models.Position, price float64) bool {
	if pos.PosSide == "long" {
		return price <= pos.HardSL
	}
	return price >= pos.HardSL
}

func trendAgainstPosition(pos *models.Position, isUp bool) bool {
	if pos.PosSide == "long" {
		return !isUp
	}
	return isUp
}

func macdZeroAgainstPosition(pos *models.Position, entry cache.IndicatorData) bool {
	if pos.PosSide == "long" {
		return entry.MACD.SmoothedHistogram <= 0 && entry.MACD.PrevSmoothedHistogram > 0
	}
	return entry.MACD.SmoothedHistogram >= 0 && entry.MACD.PrevSmoothedHistogram < 0
}

func emaConfirmsExit(pos *models.Position, entry cache.IndicatorData) bool {
	if pos.PosSide == "long" {
		return entry.Close < entry.EMAFast
	}
	return entry.Close > entry.EMAFast
}

func trailHit(pos *models.Position, price float64) bool {
	if pos.TrailStop == 0 {
		return false
	}
	if pos.PosSide == "long" {
		return price <= pos.TrailStop
	}
	return price >= pos.TrailStop
}

func oppositeOrderSide(posSide string) string {
	if posSide == "long" {
		return "sell"
	}
	return "buy"
}

func directionFromTrend(isUp bool) direction {
	if isUp {
		return directionLong
	}
	return directionShort
}

func directionFromPosSide(posSide string) direction {
	if posSide == "long" {
		return directionLong
	}
	return directionShort
}
