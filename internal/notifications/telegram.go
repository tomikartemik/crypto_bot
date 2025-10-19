package notifications

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/kuromii5/supertrend_trade_bot/configs"
	"github.com/kuromii5/supertrend_trade_bot/internal/log"
)

type TelegramService struct {
	botToken string
	userID   int64
	enabled  bool
	client   *http.Client
}

type TelegramMessage struct {
	ChatID    int64  `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode"`
}

type TelegramResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
}

func NewTelegramService() *TelegramService {
	return &TelegramService{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (ts *TelegramService) Initialize() {
	ts.botToken = configs.BotCurrentConfig.TelegramBotToken
	ts.userID = configs.BotCurrentConfig.TelegramUserID
	ts.enabled = configs.BotCurrentConfig.TelegramEnabled

	if ts.enabled && (ts.botToken == "" || ts.userID == 0) {
		log.Log.Warn("Telegram уведомления включены, но не настроены токен или ID пользователя")
		ts.enabled = false
	}
}

func (ts *TelegramService) SendMessage(text string) error {
	if !ts.enabled {
		return nil
	}

	message := TelegramMessage{
		ChatID:    ts.userID,
		Text:      text,
		ParseMode: "HTML",
	}

	jsonData, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("ошибка сериализации сообщения: %w", err)
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", ts.botToken)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("ошибка создания запроса: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := ts.client.Do(req)
	if err != nil {
		return fmt.Errorf("ошибка отправки запроса: %w", err)
	}
	defer resp.Body.Close()

	var telegramResp TelegramResponse
	if err := json.NewDecoder(resp.Body).Decode(&telegramResp); err != nil {
		return fmt.Errorf("ошибка декодирования ответа: %w", err)
	}

	if !telegramResp.OK {
		return fmt.Errorf("ошибка Telegram API: %s", telegramResp.Description)
	}

	log.Log.Debug("Telegram сообщение отправлено", "text", text)
	return nil
}

func (ts *TelegramService) SendTradeNotification(pair, side, action, reason string, entryPrice, currentPrice float64, pnl float64) error {
	var emoji string
	var actionText string

	switch action {
	case "open":
		emoji = "🟢"
		actionText = "ОТКРЫТА"
	case "close":
		emoji = "🔴"
		actionText = "ЗАКРЫТА"
	default:
		emoji = "ℹ️"
		actionText = action
	}

	var sideText string
	if side == "long" {
		sideText = "LONG"
	} else {
		sideText = "SHORT"
	}

	text := fmt.Sprintf(
		"%s <b>СДЕЛКА %s</b>\n\n"+
			"📊 Пара: <code>%s</code>\n"+
			"📈 Направление: <b>%s</b>\n"+
			"💰 Цена входа: <code>%.6f</code>\n"+
			"💵 Текущая цена: <code>%.6f</code>\n"+
			"📝 Причина: <i>%s</i>\n",
		emoji, actionText, pair, sideText, entryPrice, currentPrice, reason,
	)

	if action == "close" && pnl != 0 {
		var pnlEmoji string
		if pnl > 0 {
			pnlEmoji = "📈"
		} else {
			pnlEmoji = "📉"
		}
		text += fmt.Sprintf("%s P&L: <code>%.6f USDT</code>\n", pnlEmoji, pnl)
	}

	text += fmt.Sprintf("\n⏰ Время: <code>%s</code>", time.Now().Format("2006-01-02 15:04:05"))

	return ts.SendMessage(text)
}

func (ts *TelegramService) SendErrorNotification(pair, errorMsg string) error {
	text := fmt.Sprintf(
		"⚠️ <b>ОШИБКА ТОРГОВЛИ</b>\n\n"+
			"📊 Пара: <code>%s</code>\n"+
			"❌ Ошибка: <i>%s</i>\n"+
			"⏰ Время: <code>%s</code>",
		pair, errorMsg, time.Now().Format("2006-01-02 15:04:05"),
	)

	return ts.SendMessage(text)
}

func (ts *TelegramService) SendStartupNotification() error {
	text := fmt.Sprintf(
		"🚀 <b>БОТ ЗАПУЩЕН</b>\n\n"+
			"📊 Торговые пары: <code>%v</code>\n"+
			"⏰ Таймфрейм: <code>%s</code>\n"+
			"📈 MACD таймфрейм: <code>%s</code>\n"+
			"🎯 Режим: <code>%s</code>\n"+
			"⏰ Время запуска: <code>%s</code>",
		configs.BotCurrentConfig.TradingPairs,
		configs.BotCurrentConfig.Timeframes[0],
		configs.BotCurrentConfig.MacdTimeframe,
		func() string {
			if configs.BotCurrentConfig.IsSimulated {
				return "СИМУЛЯЦИЯ"
			}
			return "РЕАЛЬНАЯ ТОРГОВЛЯ"
		}(),
		time.Now().Format("2006-01-02 15:04:05"),
	)

	return ts.SendMessage(text)
}
