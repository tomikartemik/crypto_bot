package configs

func GetExitSettings(tf string) (ExitSettings, bool) {
	if BotCurrentConfig.TimeframeSettings == nil {
		return ExitSettings{}, false
	}
	setting, ok := BotCurrentConfig.TimeframeSettings[tf]
	if !ok || setting.ExitSettings == nil {
		return ExitSettings{}, false
	}
	return SanitizeExitSettings(*setting.ExitSettings), true
}

func SanitizeExitSettings(exitCfg ExitSettings) ExitSettings {
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

func GetSRSettings(tf string) (SRSettings, bool) {
	if BotCurrentConfig.TimeframeSettings == nil {
		return SRSettings{}, false
	}
	setting, ok := BotCurrentConfig.TimeframeSettings[tf]
	if !ok || setting.SRSettings == nil {
		return SRSettings{}, false
	}
	return SanitizeSRSettings(*setting.SRSettings), true
}

func SanitizeSRSettings(srCfg SRSettings) SRSettings {
	if srCfg.LookbackBars <= 0 {
		srCfg.LookbackBars = 200
	}
	if srCfg.SwingLeft <= 0 {
		srCfg.SwingLeft = 2
	}
	if srCfg.SwingRight <= 0 {
		srCfg.SwingRight = 2
	}
	if srCfg.ClusterRadiusATR <= 0 {
		srCfg.ClusterRadiusATR = 0.2
	}
	if srCfg.MinTouches <= 0 {
		srCfg.MinTouches = 3
	}
	if srCfg.InvalidateAfterBars <= 0 {
		srCfg.InvalidateAfterBars = 80
	}
	if srCfg.TpOffsetATR <= 0 {
		srCfg.TpOffsetATR = 0.1
	}
	return srCfg
}
