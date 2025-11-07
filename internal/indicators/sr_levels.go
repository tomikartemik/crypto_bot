package indicators

import (
	"math"
	"sort"

	"github.com/tomikartemik/crypto_bot/configs"
	"github.com/tomikartemik/crypto_bot/internal/models"
)

type swingPoint struct {
	Price float64
	Age   int
}

type srLevel struct {
	Price   float64
	Touches int
	LastAge int
}

func CalculateSRLevels(c []models.Candlestick, atr float64, cfg configs.SRSettings) (supportTarget, supportLevel, resistanceTarget, resistanceLevel float64) {
	if !cfg.Enabled || len(c) == 0 || atr == 0 {
		return 0, 0, 0, 0
	}

	lookback := cfg.LookbackBars
	if lookback <= 0 || lookback > len(c) {
		lookback = len(c)
	}

	sub := c[len(c)-lookback:]
	swingLeft := cfg.SwingLeft
	swingRight := cfg.SwingRight
	if swingLeft < 1 {
		swingLeft = 1
	}
	if swingRight < 1 {
		swingRight = 1
	}
	if len(sub) < swingLeft+swingRight+1 {
		return 0, 0, 0, 0
	}

	highSwings := detectSwings(sub, swingLeft, swingRight, true)
	lowSwings := detectSwings(sub, swingLeft, swingRight, false)

	clusterRadius := cfg.ClusterRadiusATR * atr
	if clusterRadius <= 0 {
		clusterRadius = 0.2 * atr
	}

	highLevels := clusterSwings(highSwings, clusterRadius)
	lowLevels := clusterSwings(lowSwings, clusterRadius)

	minTouches := cfg.MinTouches
	if minTouches < 1 {
		minTouches = 1
	}

	maxAge := cfg.InvalidateAfterBars
	if maxAge <= 0 {
		maxAge = lookback
	}

	currentClose := sub[len(sub)-1].Close

	validHighs := filterLevels(highLevels, minTouches, maxAge)
	validLows := filterLevels(lowLevels, minTouches, maxAge)

	sort.Slice(validHighs, func(i, j int) bool { return validHighs[i].Price < validHighs[j].Price })
	sort.Slice(validLows, func(i, j int) bool { return validLows[i].Price < validLows[j].Price })

	for _, lvl := range validHighs {
		if lvl.Price > currentClose {
			resistanceLevel = lvl.Price
			break
		}
	}

	for i := len(validLows) - 1; i >= 0; i-- {
		lvl := validLows[i]
		if lvl.Price < currentClose {
			supportLevel = lvl.Price
			break
		}
		if supportLevel == 0 && lvl.Price == currentClose {
			supportLevel = lvl.Price
		}
	}

	if resistanceLevel > 0 {
		targetOffset := cfg.TpOffsetATR * atr
		resistanceTarget = resistanceLevel - targetOffset
	}
	if supportLevel > 0 {
		targetOffset := cfg.TpOffsetATR * atr
		supportTarget = supportLevel + targetOffset
	}

	return supportTarget, supportLevel, resistanceTarget, resistanceLevel
}

func detectSwings(c []models.Candlestick, left, right int, highs bool) []swingPoint {
	var swings []swingPoint
	for i := left; i < len(c)-right; i++ {
		isSwing := true
		val := c[i].High
		if !highs {
			val = c[i].Low
		}
		for l := 1; l <= left && isSwing; l++ {
			if highs {
				if val <= c[i-l].High {
					isSwing = false
				}
			} else {
				if val >= c[i-l].Low {
					isSwing = false
				}
			}
		}
		for r := 1; r <= right && isSwing; r++ {
			if highs {
				if val < c[i+r].High {
					isSwing = false
				}
			} else {
				if val > c[i+r].Low {
					isSwing = false
				}
			}
		}
		if isSwing {
			age := len(c) - 1 - i
			swings = append(swings, swingPoint{Price: val, Age: age})
		}
	}
	return swings
}

func clusterSwings(points []swingPoint, radius float64) []srLevel {
	if len(points) == 0 {
		return nil
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Price < points[j].Price })
	var levels []srLevel
	for _, p := range points {
		assigned := false
		for i := range levels {
			if math.Abs(p.Price-levels[i].Price) <= radius {
				levels[i].Price = (levels[i].Price*float64(levels[i].Touches) + p.Price) / float64(levels[i].Touches+1)
				levels[i].Touches++
				if p.Age < levels[i].LastAge || levels[i].LastAge == 0 {
					levels[i].LastAge = p.Age
				}
				assigned = true
				break
			}
		}
		if !assigned {
			levels = append(levels, srLevel{Price: p.Price, Touches: 1, LastAge: p.Age})
		}
	}
	return levels
}

func filterLevels(levels []srLevel, minTouches, maxAge int) []srLevel {
	var res []srLevel
	for _, lvl := range levels {
		if lvl.Touches >= minTouches && lvl.LastAge <= maxAge {
			res = append(res, lvl)
		}
	}
	return res
}
