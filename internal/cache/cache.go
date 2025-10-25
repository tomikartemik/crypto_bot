package cache

import (
	"sync"
)

type MACDData struct {
	Value                 float64
	Signal                float64
	Histogram             float64
	SmoothedHistogram     float64
	PrevSmoothedHistogram float64
	PrevHistogram         float64
}

type IndicatorData struct {
	Timestamp         int64
	Close             float64
	Supertrend        float64
	IsUptrend         bool
	SupertrendFlipped bool
	EMAFast           float64
	EMASlow           float64
	RSI               float64
	ADX               float64
	ATR               float64
	ATRTrailing       float64
	MACD              MACDData
	NBarLow           float64
	NBarHigh          float64
}

type Cache struct {
	mu             sync.RWMutex
	prices         map[string]float64                  // instrumentId -> price
	lotSizes       map[string]float64                  // instrumentId -> lotSize
	contractValues map[string]float64                  // instrumentId -> ctVal
	indicators     map[string]map[string]IndicatorData // instrumentId -> TF -> данные
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
