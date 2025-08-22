package configs

import (
	"encoding/json"
	"os"
)

type BotConfig struct {
	BaseURL               string   `json:"base_url"`
	TradingPairs          []string `json:"trading_pairs"`
	ATRPeriod             int      `json:"atr_period"`
	ATRMultiplierTrailing float64  `json:"atr_multiplier_trailing"`
	ATRMultiplierStopLoss float64  `json:"atr_multiplier_stop_loss"`
	Multiplier            float64  `json:"multiplier"`
	MinOrderSize          float64  `json:"min_order_size"`
	Timeframes            []string `json:"timeframes"`
	CandlesAmount         int      `json:"candles_amount"`
	RiskPercent           float64  `json:"risk_percent"`
	CCY                   string   `json:"ccy"`
	IsSimulated           bool     `json:"is_simulated"`
	Leverage              int      `json:"leverage"`
}

func LoadBotConfig(filename string) (BotConfig, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return BotConfig{}, err
	}

	var config BotConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return BotConfig{}, err
	}

	return config, nil
}

var BotCurrentConfig BotConfig
