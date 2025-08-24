package exchanger

import (
	"context"

	"github.com/kuromii5/supertrend_trade_bot/internal/models"
)

type MarketProvider interface {
	GetCandlesticks(instId string, interval string, limit int) ([]models.Candlestick, error)
	Subscribe(ctx context.Context, instruments []string) error
	GetLotSize(instId string) (float64, error)
	GetContractValue(instId string) (float64, error)
}

type TradingAccount interface {
	PlaceOrder(instId, side, posSide string, size float64) error
	GetTradeSize(instId, ccy string, riskPercent, price float64) (float64, error)
	SetLeverage(instId string) error
}
