package models

import (
	"fmt"
	"math"
	"strings"
)

type Candlestick struct {
	Open     float64
	High     float64
	Low      float64
	Close    float64
	TsMillis int64
}

type Position struct {
	InstId        string
	PosSide       string
	TradeSize     float64
	EntryPrice    float64
	StopLossPrice float64
}

func (p Position) String() string {
	return fmt.Sprintf(
		"Инструмент: %s | Позиция: %s | Размер: %.4f | Цена входа: %.4f | Стоп-лосс: %.4f (%.4f%%)",
		p.InstId,
		strings.ToUpper(p.PosSide), // "short" -> "SHORT", "long" -> "LONG"
		p.TradeSize,
		p.EntryPrice,
		p.StopLossPrice,
		math.Abs((p.StopLossPrice/p.EntryPrice)-1)*100, // Процент от цены входа
	)
}
