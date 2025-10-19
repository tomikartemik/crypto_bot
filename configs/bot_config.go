package configs

import (
	"encoding/json"
	"log/slog"
	"os"

	"github.com/kuromii5/supertrend_trade_bot/internal/log"
)

type BotConfig struct {
	BaseURL           string   `json:"base_url"`
	TradingPairs      []string `json:"trading_pairs"`
	ATRPeriod         int      `json:"atr_period"`
	ATRMultiplierStop float64  `json:"atr_multiplier_stop"`
	Multiplier        float64  `json:"multiplier"`
	MinOrderSize      float64  `json:"min_order_size"`
	Timeframes        []string `json:"timeframes"`
	MacdTimeframe     string   `json:"macd_timeframe"`
	MacdFastPeriod    int      `json:"macd_fast_period"`
	MacdSlowPeriod    int      `json:"macd_slow_period"`
	MacdSignalPeriod  int      `json:"macd_signal_period"`
	CandlesAmount     int      `json:"candles_amount"`
	RiskPercent       float64  `json:"risk_percent"`
	CCY               string   `json:"ccy"`
	IsSimulated       bool     `json:"is_simulated"`
	Leverage          int      `json:"leverage"`
	DebugMode         bool     `json:"debug_mode"`
	Margin            string   `json:"margin"`
	// Telegram настройки
	TelegramBotToken  string   `json:"telegram_bot_token"`
	TelegramUserID    int64    `json:"telegram_user_id"`
	TelegramEnabled   bool     `json:"telegram_enabled"`
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

	// Устанавливаем значения по умолчанию для MACD, если они не указаны
	if config.MacdFastPeriod == 0 {
		config.MacdFastPeriod = 12
	}
	if config.MacdSlowPeriod == 0 {
		config.MacdSlowPeriod = 26
	}
	if config.MacdSignalPeriod == 0 {
		config.MacdSignalPeriod = 9
	}

	if config.DebugMode {
		log.Log = *log.NewLogger(slog.LevelDebug)
	} else {
		log.Log = *log.NewLogger(slog.LevelInfo)
	}

	return config, nil
}

var BotCurrentConfig BotConfig
