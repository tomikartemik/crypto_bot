package telegram

import (
	"fmt"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type TelegramNotifier struct {
	bot    *tgbotapi.BotAPI
	chatID string
}

func NewTelegramNotifier(botToken, chatID string) (*TelegramNotifier, error) {
	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		return nil, fmt.Errorf("ошибка создания Telegram бота: %v", err)
	}

	bot.Debug = false

	return &TelegramNotifier{
		bot:    bot,
		chatID: chatID,
	}, nil
}

func (tn *TelegramNotifier) SendMessage(message string) error {
	chatID, err := strconv.ParseInt(tn.chatID, 10, 64)
	if err != nil {
		return fmt.Errorf("ошибка парсинга chatID: %v", err)
	}
	
	msg := tgbotapi.NewMessage(chatID, message)
	msg.ParseMode = "HTML"
	
	_, err = tn.bot.Send(msg)
	if err != nil {
		return fmt.Errorf("ошибка отправки сообщения в Telegram: %v", err)
	}
	
	return nil
}

func (tn *TelegramNotifier) SendTradeNotification(instId, action, side string, price, size float64, pnl float64) error {
	var emoji string
	var actionText string
	
	switch action {
	case "open":
		emoji = "🟢"
		actionText = "ОТКРЫТА"
	case "close":
		emoji = "🔴"
		actionText = "ЗАКРЫТА"
	}
	
	var sideEmoji string
	if side == "long" {
		sideEmoji = "📈"
	} else {
		sideEmoji = "📉"
	}
	
	message := fmt.Sprintf(
		"%s <b>%s</b> %s %s\n"+
		"Инструмент: <code>%s</code>\n"+
		"Цена: <code>%.6f</code>\n"+
		"Размер: <code>%.6f</code>",
		emoji, actionText, sideEmoji, side,
		instId, price, size,
	)
	
	if action == "close" && pnl != 0 {
		var pnlEmoji string
		if pnl > 0 {
			pnlEmoji = "💰"
		} else {
			pnlEmoji = "💸"
		}
		message += fmt.Sprintf("\nPnL: %s <code>%.3f%%</code>", pnlEmoji, pnl)
	}
	
	return tn.SendMessage(message)
}

func (tn *TelegramNotifier) SendTrendChangeNotification(instId string, oldTrend, newTrend bool) error {
	var oldTrendText, newTrendText string
	var emoji string
	
	if oldTrend {
		oldTrendText = "ВОСХОДЯЩИЙ"
	} else {
		oldTrendText = "НИСХОДЯЩИЙ"
	}
	
	if newTrend {
		newTrendText = "ВОСХОДЯЩИЙ"
		emoji = "📈"
	} else {
		newTrendText = "НИСХОДЯЩИЙ"
		emoji = "📉"
	}
	
	message := fmt.Sprintf(
		"🔄 <b>СМЕНА ТРЕНДА</b>\n"+
		"Инструмент: <code>%s</code>\n"+
		"Было: %s → Стало: %s %s",
		instId, oldTrendText, newTrendText, emoji,
	)
	
	return tn.SendMessage(message)
}
