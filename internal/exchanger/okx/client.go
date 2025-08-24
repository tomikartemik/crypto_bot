package okx

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"time"

	"github.com/kuromii5/supertrend_trade_bot/configs"
	"github.com/kuromii5/supertrend_trade_bot/internal/cache"
	"github.com/kuromii5/supertrend_trade_bot/internal/utils"
)

type Client struct {
	apiKey     string
	apiSecret  string
	passphrase string
	httpClient *http.Client
}

func NewClient(apiKey, apiSecret, passphrase string) *Client {
	return &Client{
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		passphrase: passphrase,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) PlaceOrder(instId, side, posSide string, size float64) error {
	ctVal, ok := cache.Get().GetContractValue(instId)
	if !ok || ctVal == 0 {
		return fmt.Errorf("не найден contract value для %s", instId)
	}

	// размер позиции в контрактах, так как мы на фьючах
	contracts := size / ctVal
	sz := int(math.Floor(contracts)) * configs.BotCurrentConfig.Leverage
	if sz <= 0 {
		return fmt.Errorf("размер позиции меньше 1 контракта")
	}

	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	requestPath := "/api/v5/trade/order"
	fullURL := configs.BotCurrentConfig.BaseURL + requestPath

	body := fmt.Sprintf(`{
        "instId":"%s",
        "tdMode":"isolated",
        "side":"%s",
        "ordType":"market",
        "posSide":"%s",
        "sz":"%d",
		"lever":"%d"
    }`, instId, side, posSide, sz, configs.BotCurrentConfig.Leverage)

	req, _ := http.NewRequest("POST", fullURL, bytes.NewBuffer([]byte(body)))
	sign := signRequest("POST", requestPath, body, timestamp, c.apiSecret)

	req.Header.Set("OK-ACCESS-KEY", c.apiKey)
	req.Header.Set("OK-ACCESS-SIGN", sign)
	req.Header.Set("OK-ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("OK-ACCESS-PASSPHRASE", c.passphrase)
	req.Header.Set("Content-Type", "application/json")
	if configs.BotCurrentConfig.IsSimulated {
		req.Header.Set("x-simulated-trading", "1")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

func (c *Client) SetLeverage(instId string) error {
	for _, posSide := range []string{"long", "short"} {
		timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
		requestPath := "/api/v5/account/set-leverage"
		fullURL := configs.BotCurrentConfig.BaseURL + requestPath

		body := fmt.Sprintf(`{
			"instId":"%s",
			"lever":"%d",
			"posSide":"%s",
			"mgnMode":"isolated"
		}`, instId, configs.BotCurrentConfig.Leverage, posSide)

		req, _ := http.NewRequest("POST", fullURL, bytes.NewBuffer([]byte(body)))
		sign := signRequest("POST", requestPath, body, timestamp, c.apiSecret)

		req.Header.Set("OK-ACCESS-KEY", c.apiKey)
		req.Header.Set("OK-ACCESS-SIGN", sign)
		req.Header.Set("OK-ACCESS-TIMESTAMP", timestamp)
		req.Header.Set("OK-ACCESS-PASSPHRASE", c.passphrase)
		req.Header.Set("Content-Type", "application/json")
		if configs.BotCurrentConfig.IsSimulated {
			req.Header.Set("x-simulated-trading", "1")
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("ошибка при выставлении плеча для %s %s: %w", instId, posSide, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("плечо не установлено для %s %s, статус %d, ответ: %s",
				instId, posSide, resp.StatusCode, string(b))
		}
	}
	return nil
}

func (c *Client) GetTradeSize(instId, ccy string, riskPercent, price float64) (float64, error) {
	balance, err := getBalance(ccy, c.apiKey, c.passphrase, c.apiSecret)
	if err != nil {
		return 0, fmt.Errorf("ошибка получения баланса: %w", err)
	}

	lotSize, ok := cache.Get().GetLotSize(instId)
	if !ok {
		log.Printf("lotSize для %s не найден, fallback на 0.01", instId)
		lotSize = 0.01
	}

	positionSizeUSDT := balance * riskPercent
	tradeSize := positionSizeUSDT / price

	precision := utils.CountDecimals(lotSize)
	scale := math.Pow(10, float64(precision))
	size := math.Floor(tradeSize*scale) / scale

	if size < lotSize {
		return 0, fmt.Errorf("размер позиции меньше минимального лота")
	}

	return size, nil
}
