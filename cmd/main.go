package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/kuromii5/supertrend_trade_bot/internal/bot"
	"github.com/kuromii5/supertrend_trade_bot/internal/log"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	bot := bot.New()

	go bot.Run(ctx)

	<-ctx.Done()
	log.Log.Info("Получен сигнал, остановка бота...")
	bot.Stop()
}
