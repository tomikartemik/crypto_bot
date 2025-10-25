package configs

import (
	"encoding/json"
	"os"
)

type BotConfig struct {
	BaseURL          string   `json:"base_url"`
	TradingPairs     []string `json:"trading_pairs"`
	Timeframes       []string `json:"timeframes"`
	EntryTimeframe   string   `json:"entry_timeframe"`
	TrendTimeframe   string   `json:"trend_timeframe"`
	UseMTF           bool     `json:"use_mtf"`
	MTFRequireBoth   bool     `json:"mtf_require_both"`
	OnlyClosedCandle bool     `json:"only_closed_candle"`

	ATRPeriod             int     `json:"atr_period"`
	ATRMultiplierTrailing float64 `json:"atr_multiplier_trailing"`
	ATRMultiplierStopLoss float64 `json:"atr_multiplier_stop_loss"`
	ATRPeriodTrailing     int     `json:"atr_trailing_period"`
	Multiplier            float64 `json:"multiplier"`
	MinOrderSize          float64 `json:"min_order_size"`
	CandlesAmount         int     `json:"candles_amount"`
	RiskPercent           float64 `json:"risk_percent"`
	CCY                   string  `json:"ccy"`
	IsSimulated           bool    `json:"is_simulated"`
	Leverage              int     `json:"leverage"`

	EMAFast int `json:"ema_fast"`
	EMASlow int `json:"ema_slow"`

	RSIPeriod   int     `json:"rsi_period"`
	RSIMaxLong  float64 `json:"rsi_max_long"`
	RSIMinShort float64 `json:"rsi_min_short"`

	ADXPeriod int     `json:"adx_period"`
	ADXMin    float64 `json:"adx_min"`

	MACDFast           int  `json:"macd_fast"`
	MACDSlow           int  `json:"macd_slow"`
	MACDSignal         int  `json:"macd_signal"`
	MACDSmoothBars     int  `json:"macd_smooth_bars"`
	MACDRequireTwoBars bool `json:"macd_require_two_bars"`

	ReentryTolerance float64 `json:"reentry_tolerance"`
	ArmedBarsToLive  int     `json:"armed_bars_to_live"`
	UseArmedOnly     bool    `json:"use_armed_only"`

	InitSLPct         float64 `json:"init_sl_pct"`
	TPPct             float64 `json:"tp_pct"`
	TrailActivatePct  float64 `json:"trail_activate_pct"`
	TrailStepPct      float64 `json:"trail_step_pct"`
	TrailMode         string  `json:"trail_mode"`
	TrailLookbackBars int     `json:"trail_lookback_bars"`
	MinReentryPct     float64 `json:"min_reentry_pct"`
	CooldownBars      int     `json:"cooldown_bars"`

	EnableAlternativeEntry       bool `json:"enable_alternative_entry"`
	AlternativeRequireSupertrend bool `json:"alternative_require_supertrend"`
	AllowMacdExit                bool `json:"allow_macd_exit"`
	AllowSupertrendFlipExit      bool `json:"allow_supertrend_flip_exit"`
	AllowTrailingExit            bool `json:"allow_trailing_exit"`
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

	if config.EntryTimeframe == "" && len(config.Timeframes) > 0 {
		config.EntryTimeframe = config.Timeframes[0]
	}
	if config.EntryTimeframe == "" {
		config.EntryTimeframe = "5m"
	}

	if config.TrendTimeframe == "" && len(config.Timeframes) > 1 {
		config.TrendTimeframe = config.Timeframes[1]
	}

	if len(config.Timeframes) == 0 {
		config.Timeframes = append(config.Timeframes, config.EntryTimeframe)
		if config.TrendTimeframe != "" && config.TrendTimeframe != config.EntryTimeframe {
			config.Timeframes = append(config.Timeframes, config.TrendTimeframe)
		}
	}

	if config.MACDSmoothBars == 0 {
		config.MACDSmoothBars = 2
	}
	if config.ArmedBarsToLive == 0 {
		config.ArmedBarsToLive = 2
	}
	if config.ReentryTolerance == 0 {
		config.ReentryTolerance = 0.001
	}
	if config.TrailMode == "" {
		config.TrailMode = "atr"
	}
	if config.TrailLookbackBars == 0 {
		config.TrailLookbackBars = 3
	}
	if config.CooldownBars == 0 {
		config.CooldownBars = 1
	}
	if config.MinReentryPct == 0 {
		config.MinReentryPct = 0.003
	}
	if config.ATRPeriodTrailing == 0 {
		config.ATRPeriodTrailing = 14
	}
	if !config.OnlyClosedCandle {
		config.OnlyClosedCandle = true
	}
	if config.MTFRequireBoth && !config.UseMTF {
		config.MTFRequireBoth = false
	}
	if config.TrendTimeframe == "" {
		config.UseMTF = false
		config.MTFRequireBoth = false
	}

	config.Timeframes = compactTimeframes(config)

	return config, nil
}

func compactTimeframes(cfg BotConfig) []string {
	seen := make(map[string]struct{})
	ordered := make([]string, 0, 3)
	add := func(tf string) {
		if tf == "" {
			return
		}
		if _, ok := seen[tf]; ok {
			return
		}
		seen[tf] = struct{}{}
		ordered = append(ordered, tf)
	}
	add(cfg.EntryTimeframe)
	add(cfg.TrendTimeframe)
	for _, tf := range cfg.Timeframes {
		add(tf)
	}
	return ordered
}

var BotCurrentConfig BotConfig
