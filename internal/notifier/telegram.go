package notifier

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tomikartemik/crypto_bot/configs"
	"github.com/tomikartemik/crypto_bot/internal/log"
)

var httpClient = &http.Client{Timeout: 5 * time.Second}

// SendTelegramMessage отправляет сообщение в Telegram, если уведомления включены в конфиге.
func SendTelegramMessage(message string) {
	cfg := configs.BotCurrentConfig
	if !cfg.TelegramEnabled || cfg.TelegramBotToken == "" || cfg.TelegramChatID == "" {
		return
	}

	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.TelegramBotToken)
	data := url.Values{}
	data.Set("chat_id", cfg.TelegramChatID)
	data.Set("text", message)
	data.Set("parse_mode", "Markdown")
	data.Set("disable_web_page_preview", "true")

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		log.Log.Error("не удалось сформировать запрос к Telegram", "error", err)
		return
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Log.Error("ошибка при отправке уведомления в Telegram", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		log.Log.Error("Telegram вернул ошибку", "status", resp.StatusCode, "body", string(body))
	}
}
