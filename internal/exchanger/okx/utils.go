package okx

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/tomikartemik/crypto_bot/configs"
)

func signRequest(method, path, body, timestamp, apiSecret string) string {
	message := timestamp + method + path + body
	mac := hmac.New(sha256.New, []byte(apiSecret))
	mac.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func getBalance(ccy, apiKey, passphrase, apiSecret string) (float64, error) {
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	path := fmt.Sprintf("/api/v5/account/balance?ccy=%s", ccy)

	req, _ := http.NewRequest("GET", configs.BotCurrentConfig.BaseURL+path, nil)
	sign := signRequest("GET", path, "", timestamp, apiSecret)
	req.Header.Set("OK-ACCESS-KEY", apiKey)
	req.Header.Set("OK-ACCESS-SIGN", sign)
	req.Header.Set("OK-ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("OK-ACCESS-PASSPHRASE", passphrase)
	req.Header.Set("Content-Type", "application/json")
	if configs.BotCurrentConfig.IsSimulated {
		req.Header.Set("x-simulated-trading", "1")
	}

	c := http.Client{Timeout: 10 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Data []struct {
			Details []struct {
				Eq string `json:"eq"`
			} `json:"details"`
		} `json:"data"`
	}
	json.Unmarshal(body, &result)

	if len(result.Data) == 0 || len(result.Data[0].Details) == 0 || result.Data[0].Details[0].Eq == "" {
		return 0, fmt.Errorf("нет маржинального баланса, ответ сервера: %s", string(body))
	}

	return strconv.ParseFloat(result.Data[0].Details[0].Eq, 64)
}
