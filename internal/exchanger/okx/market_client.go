package okx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tomikartemik/crypto_bot/configs"
	"github.com/tomikartemik/crypto_bot/internal/cache"
	"github.com/tomikartemik/crypto_bot/internal/log"
	"github.com/tomikartemik/crypto_bot/internal/models"
)

type MarketClient struct {
	httpClient *http.Client
}

func NewBotClient() *MarketClient {
	return &MarketClient{
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *MarketClient) GetCandlesticks(instId string, interval string, limit int) ([]models.Candlestick, error) {
	url := fmt.Sprintf("%s/api/v5/market/candles?instId=%s&bar=%s&limit=%d", configs.BotCurrentConfig.BaseURL, instId, interval, limit)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return []models.Candlestick{}, err
	}

	if configs.BotCurrentConfig.IsSimulated {
		req.Header.Set("x-simulated-trading", "1")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return []models.Candlestick{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения ответа: %w", err)
	}

	var result struct {
		Data [][]string `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("ошибка парсинга JSON: %w", err)
	}

	candles := make([]models.Candlestick, 0, len(result.Data))
	for i := len(result.Data) - 1; i >= 0; i-- {
		entry := result.Data[i]

		open, _ := strconv.ParseFloat(entry[1], 64)
		high, _ := strconv.ParseFloat(entry[2], 64)
		low, _ := strconv.ParseFloat(entry[3], 64)
		closep, _ := strconv.ParseFloat(entry[4], 64)
		ts, _ := strconv.ParseInt(entry[0], 10, 64)

		candles = append(candles, models.Candlestick{
			Open:     open,
			High:     high,
			Low:      low,
			Close:    closep,
			TsMillis: ts,
		})
	}

	return candles, nil
}

type WSRequest struct {
	Op   string        `json:"op"`
	Args []interface{} `json:"args"`
}

type WSTickerResponse struct {
	Arg struct {
		Channel string `json:"channel"`
		InstID  string `json:"instId"`
	} `json:"arg"`
	Data []struct {
		Last string `json:"last"`
	} `json:"data"`
}

func (c *MarketClient) Subscribe(ctx context.Context, instruments []string) error {
	url := "wss://ws.okx.com:8443/ws/v5/public"
	if configs.BotCurrentConfig.IsSimulated {
		url = "wss://wspap.okx.com:8443/ws/v5/public?brokerId=9999"
	}

	go func() {
	reconnect:
		for {
			conn, _, err := websocket.DefaultDialer.Dial(url, nil)
			if err != nil {
				time.Sleep(5 * time.Second)
				continue
			}

			for _, instId := range instruments {
				sub := WSRequest{
					Op: "subscribe",
					Args: []interface{}{
						map[string]string{
							"channel": "tickers",
							"instId":  instId,
						},
					},
				}
				if err := conn.WriteJSON(sub); err != nil {
					log.Log.Error("Ошибка подписки", "inst", instId, "error", err)
				}
			}

			conn.SetReadLimit(1024)
			conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			conn.SetPongHandler(func(appData string) error {
				conn.SetReadDeadline(time.Now().Add(60 * time.Second))
				return nil
			})

			go func() {
				ticker := time.NewTicker(15 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
							log.Log.Error("Ошибка отправки ping", "error", err)
							return
						}
					}
				}
			}()

			for {
				select {
				case <-ctx.Done():
					log.Log.Error("Закрываем WS соединение")
					conn.Close()
					return
				default:
					_, message, err := conn.ReadMessage()
					if err != nil {
						log.Log.Error("Ошибка чтения из WebSocket", "error", err)
						conn.Close()
						time.Sleep(2 * time.Second)
						continue reconnect
					}

					var resp WSTickerResponse
					if err := json.Unmarshal(message, &resp); err != nil {
						continue
					}

					if len(resp.Data) > 0 {
						priceStr := resp.Data[0].Last
						price, err := strconv.ParseFloat(priceStr, 64)
						if err == nil {
							cache.Get().SetPrice(resp.Arg.InstID, price)
						}
					}
				}
			}
		}
	}()

	return nil
}

func (c *MarketClient) GetInstrumentInfo(instId string) (*models.InstrumentInfo, error) {
	url := fmt.Sprintf("%s/api/v5/public/instruments?instType=SWAP&instId=%s", configs.BotCurrentConfig.BaseURL, instId)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	if configs.BotCurrentConfig.IsSimulated {
		req.Header.Set("x-simulated-trading", "1")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Data []struct {
			MinSize string `json:"minSz"`
			CtVal   string `json:"ctVal"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if len(result.Data) == 0 {
		return nil, fmt.Errorf("instrument info not found for %s", instId)
	}

	minSz, err := strconv.ParseFloat(result.Data[0].MinSize, 64)
	if err != nil {
		return nil, fmt.Errorf("parse minSz: %w", err)
	}

	ctVal, err := strconv.ParseFloat(result.Data[0].CtVal, 64)
	if err != nil {
		return nil, fmt.Errorf("parse ctVal: %w", err)
	}

	return &models.InstrumentInfo{
		MinSize: minSz,
		CtVal:   ctVal,
	}, nil
}
