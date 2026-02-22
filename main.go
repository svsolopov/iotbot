package main

import (
	"log"
	"time"

	tele "gopkg.in/telebot.v4"
)

func main() {
	cfg, err := LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Ошибка загрузки конфига: %v", err)
	}

	bot, err := tele.NewBot(tele.Settings{
		Token:  cfg.Telegram.Token,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		log.Fatalf("Ошибка создания бота: %v", err)
	}

	SetupHandlers(bot, cfg)

	_ = bot.SetCommands([]tele.Command{
		{Text: "sensors", Description: "Датчики"},
		{Text: "relay", Description: "Управление реле"},
	})

	go StartMQTT(cfg, bot)

	log.Println("Бот запущен")
	bot.Start()
}
