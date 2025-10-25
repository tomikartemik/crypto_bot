package models

import (
	"fmt"
	"math"
	"strings"
	"time"
)

type Candlestick struct {
	Open     float64
	High     float64
	Low      float64
	Close    float64
	TsMillis int64
}

type Position struct {
	InstId       string
	PosSide      string
	TradeSize    float64
	EntryPrice   float64
	EntryTime    time.Time
	HardSL       float64
	TPTrigger    float64
	TrailStop    float64
	TrailExtreme float64
	Fees         float64
}

func (p Position) String() string {
	return fmt.Sprintf(
		"Инструмент: %s | Позиция: %s | Размер: %.4f | Цена входа: %.4f | SL: %.4f (%.4f%%)",
		p.InstId,
		strings.ToUpper(p.PosSide), // "short" -> "SHORT", "long" -> "LONG"
		p.TradeSize,
		p.EntryPrice,
		p.HardSL,
		math.Abs((p.HardSL/p.EntryPrice)-1)*100, // Процент от цены входа
	)
}

type InstrumentInfo struct {
	MinSize float64
	CtVal   float64
}
