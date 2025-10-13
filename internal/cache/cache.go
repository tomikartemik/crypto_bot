package cache

import (
	"sync"
)

type IndicatorData struct {
	Supertrend float64
	ATR        float64
	IsUptrend  bool
}

type MACDIndicatorData struct {
	MACD     float64
	Signal   float64
	Histogram float64
	BuySignal  bool
	SellSignal bool
}

type Cache struct {
	mu             sync.RWMutex
	prices         map[string]float64                  // instrumentId -> price
	lotSizes       map[string]float64                  // instrumentId -> lotSize
	contractValues map[string]float64                  // instrumentId -> ctVal
	indicators     map[string]map[string]IndicatorData // instrumentId -> TF -> данные
	macdIndicators  map[string]MACDIndicatorData        // instrumentId -> MACD данные
}

var (
	instance *Cache
	once     sync.Once
)

func Get() *Cache {
	once.Do(func() {
		instance = &Cache{
			prices:         make(map[string]float64),
			lotSizes:       make(map[string]float64),
			indicators:     make(map[string]map[string]IndicatorData),
			contractValues: make(map[string]float64),
			macdIndicators: make(map[string]MACDIndicatorData),
		}
	})
	return instance
}

func (c *Cache) SetPrice(instrumentId string, price float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prices[instrumentId] = price
}

func (c *Cache) GetPrice(instrumentId string) (float64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	price, exists := c.prices[instrumentId]
	return price, exists
}

func (c *Cache) SetLotSize(instrumentId string, size float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lotSizes[instrumentId] = size
}

func (c *Cache) GetLotSize(instrumentId string) (float64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	size, exists := c.lotSizes[instrumentId]
	return size, exists
}

func (c *Cache) SetContractValue(instrumentId string, size float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.contractValues[instrumentId] = size
}

func (c *Cache) GetContractValue(instrumentId string) (float64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	size, exists := c.contractValues[instrumentId]
	return size, exists
}

// ======== INDICATORS ========

func (c *Cache) SetIndicatorData(instrumentId string, tf string, data IndicatorData) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.indicators[instrumentId]; !exists {
		c.indicators[instrumentId] = make(map[string]IndicatorData)
	}
	c.indicators[instrumentId][tf] = data
}

func (c *Cache) GetIndicatorData(instrumentId string, tf string) (IndicatorData, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	tfs, exists := c.indicators[instrumentId]
	if !exists {
		return IndicatorData{}, false
	}
	data, exists := tfs[tf]
	return data, exists
}

// ======== MACD INDICATORS ========

func (c *Cache) SetMACDData(instrumentId string, data MACDIndicatorData) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.macdIndicators[instrumentId] = data
}

func (c *Cache) GetMACDData(instrumentId string) (MACDIndicatorData, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	data, exists := c.macdIndicators[instrumentId]
	return data, exists
}
