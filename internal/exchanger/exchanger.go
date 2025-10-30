package exchanger

import (
	"context"

	"github.com/tomikartemik/crypto_bot/internal/models"
)

type MarketProvider interface {
	GetCandlesticks(instId string, interval string, limit int) ([]models.Candlestick, error)
	Subscribe(ctx context.Context, instruments []string) error
	GetInstrumentInfo(instId string) (*models.InstrumentInfo, error)
}

type TradingAccount interface {
	PlaceOrder(instId, side, posSide string, size float64) error
	GetTradeSize(instId, ccy string, riskPercent, fixedUSDT, price float64) (float64, error)
	SetLeverage(instId string) error
	GetAccountBalance(ccy string) (float64, error)
}
