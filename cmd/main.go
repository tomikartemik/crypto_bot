package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/kuromii5/supertrend_trade_bot/internal/bot"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	bot := bot.New()

	go bot.Run(ctx)

	<-ctx.Done()
	log.Println("Получен сигнал завершения, остановка бота...")
	bot.Stop()
}
