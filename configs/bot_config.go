package configs

import (
	"encoding/json"
	"log/slog"
	"os"

	"github.com/tomikartemik/crypto_bot/internal/log"
)

type BotConfig struct {
	BaseURL           string                      `json:"base_url"`
	TradingPairs      []string                    `json:"trading_pairs"`
	ATRMultiplierStop float64                     `json:"atr_multiplier_stop"`
	TimeframeSettings map[string]TimeframeSetting `json:"timeframe_settings"`
	MinOrderSize      float64                     `json:"min_order_size"`
	Timeframes        []string                    `json:"timeframes"`
	CandlesAmount     int                         `json:"candles_amount"`
	RiskPercent       float64                     `json:"risk_percent"`
	CCY               string                      `json:"ccy"`
	IsSimulated       bool                        `json:"is_simulated"`
	Leverage          int                         `json:"leverage"`
	DebugMode         bool                        `json:"debug_mode"`
	Margin            string                      `json:"margin"`
}

type TimeframeSetting struct {
	ATRPeriod  int     `json:"atr_period"`
	Multiplier float64 `json:"multiplier"`
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

	if config.DebugMode {
		log.Log = *log.NewLogger(slog.LevelDebug)
	} else {
		log.Log = *log.NewLogger(slog.LevelInfo)
	}

	return config, nil
}

var BotCurrentConfig BotConfig
