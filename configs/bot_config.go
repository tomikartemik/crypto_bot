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
	PositionSizeUSDT  float64                     `json:"position_size_usdt"`
	InitialBank       float64                     `json:"initial_bank"`
	StatsFile         string                      `json:"stats_file"`
	CCY               string                      `json:"ccy"`
	IsSimulated       bool                        `json:"is_simulated"`
	Leverage          int                         `json:"leverage"`
	DebugMode         bool                        `json:"debug_mode"`
	Margin            string                      `json:"margin"`
	TelegramEnabled   bool                        `json:"telegram_enabled"`
	TelegramBotToken  string                      `json:"telegram_bot_token"`
	TelegramChatID    string                      `json:"telegram_chat_id"`
}

type TimeframeSetting struct {
	ATRPeriod    int           `json:"atr_period"`
	Multiplier   float64       `json:"multiplier"`
	ExitSettings *ExitSettings `json:"exit_settings,omitempty"`
}

type ExitSettings struct {
	InitialSLATR        float64  `json:"initial_sl_atr"`
	PartialTakeProfitR  float64  `json:"partial_take_profit_r"`
	PartialClosePercent float64  `json:"partial_close_percent"`
	TrailingATR         float64  `json:"trailing_atr"`
	GivebackTriggerR    float64  `json:"giveback_trigger_r"`
	GivebackAmountR     float64  `json:"giveback_amount_r"`
	FlipBufferATR       float64  `json:"flip_buffer_atr"`
	BreakEvenBuffer     float64  `json:"break_even_buffer"`
	TimeStopUTC         []string `json:"time_stop_utc"`
	TimeStopHours       float64  `json:"time_stop_hours"`
	TimeStopMinR        float64  `json:"time_stop_min_r"`
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
