package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	tele "gopkg.in/telebot.v4"
)

func SetupHandlers(bot *tele.Bot, cfg *Config) {
	// Middleware авторизации
	bot.Use(func(next tele.HandlerFunc) tele.HandlerFunc {
		return func(c tele.Context) error {
			if c.Chat().ID != cfg.Telegram.ChatID {
				return nil
			}
			return next(c)
		}
	})

	bot.Handle("/start", func(c tele.Context) error {
		return c.Send(
			"Умный дом — бот управления\n\n" +
				"/sensors — датчики\n" +
				"/relay — управление реле",
		)
	})

	bot.Handle("/sensors", func(c tele.Context) error {
		var sections []string

		for _, section := range cfg.Sensors {
			if len(section.Items) == 0 {
				continue
			}
			lines := []string{section.Emoji + " " + section.Section + ":"}
			for _, dev := range section.Items {
				val, ok := deviceState.Load(dev.FriendlyName)
				if !ok {
					lines = append(lines, fmt.Sprintf("  %s: нет данных", dev.Alias))
					continue
				}
				state, _ := val.(map[string]interface{})
				if dev.StateKey != "" {
					// Явный state_key — читаем конкретное значение
					v, ok := state[dev.StateKey]
					if !ok {
						lines = append(lines, fmt.Sprintf("  %s: нет данных", dev.Alias))
						continue
					}
					switch typedVal := v.(type) {
					case bool:
						if typedVal {
							lines = append(lines, fmt.Sprintf("  %s: ⚠️ сработал", dev.Alias))
						} else {
							lines = append(lines, fmt.Sprintf("  %s: норма", dev.Alias))
						}
					default:
						unit := dev.Unit
						if unit != "" {
							unit = " " + unit
						}
						lines = append(lines, fmt.Sprintf("  %s: %v%s", dev.Alias, v, unit))
					}
				} else {
					// Нет state_key — автодетект temperature/humidity
					var parts []string
					if temp, ok := state["temperature"]; ok {
						parts = append(parts, fmt.Sprintf("%v°C", temp))
					}
					if hum, ok := state["humidity"]; ok {
						parts = append(parts, fmt.Sprintf("%v%%", hum))
					}
					if len(parts) > 0 {
						lines = append(lines, fmt.Sprintf("  %s: %s", dev.Alias, strings.Join(parts, ", ")))
					} else {
						lines = append(lines, fmt.Sprintf("  %s: нет данных", dev.Alias))
					}
				}
			}
			sections = append(sections, strings.Join(lines, "\n"))
		}

		if len(sections) == 0 {
			return c.Send("Нет датчиков в конфигурации")
		}
		return c.Send(strings.Join(sections, "\n\n"))
	})

	bot.Handle("/relay", func(c tele.Context) error {
		if len(cfg.Relay) == 0 {
			return c.Send("Нет реле в конфигурации")
		}
		return c.Send("Реле:", relayKeyboard(cfg))
	})

	bot.Handle(&tele.InlineButton{Unique: "relay_on"}, func(c tele.Context) error {
		idx, err := strconv.Atoi(c.Callback().Data)
		if err != nil || idx < 0 || idx >= len(cfg.Relay) {
			return c.Respond(&tele.CallbackResponse{Text: "Некорректный индекс реле"})
		}
		dev := cfg.Relay[idx]
		if err := PublishRelay(cfg, dev, "ON"); err != nil {
			log.Printf("Ошибка публикации MQTT: %v", err)
			return c.Respond(&tele.CallbackResponse{Text: "Ошибка отправки команды"})
		}
		_ = c.Respond(&tele.CallbackResponse{Text: "Отправлено: ON"})
		time.Sleep(1 * time.Second)
		return c.Edit("Реле:", relayKeyboard(cfg))
	})

	bot.Handle(&tele.InlineButton{Unique: "relay_off"}, func(c tele.Context) error {
		idx, err := strconv.Atoi(c.Callback().Data)
		if err != nil || idx < 0 || idx >= len(cfg.Relay) {
			return c.Respond(&tele.CallbackResponse{Text: "Некорректный индекс реле"})
		}
		dev := cfg.Relay[idx]
		if err := PublishRelay(cfg, dev, "OFF"); err != nil {
			log.Printf("Ошибка публикации MQTT: %v", err)
			return c.Respond(&tele.CallbackResponse{Text: "Ошибка отправки команды"})
		}
		_ = c.Respond(&tele.CallbackResponse{Text: "Отправлено: OFF"})
		time.Sleep(1 * time.Second)
		return c.Edit("Реле:", relayKeyboard(cfg))
	})

	bot.Handle(&tele.InlineButton{Unique: "relay_noop"}, func(c tele.Context) error {
		return c.Respond()
	})
}

func relayKeyboard(cfg *Config) *tele.ReplyMarkup {
	menu := &tele.ReplyMarkup{}
	var rows []tele.Row
	for i, dev := range cfg.Relay {
		current := "OFF"
		if val, ok := deviceState.Load(dev.FriendlyName); ok {
			if state, ok := val.(map[string]interface{}); ok {
				if s, ok := state[dev.StateKey].(string); ok {
					current = s
				}
			}
		}
		idx := strconv.Itoa(i)
		toggle := "relay_on"
		if current == "ON" {
			toggle = "relay_off"
		}
		rows = append(rows, menu.Row(
			menu.Data(dev.Alias, "relay_noop", idx),
			menu.Data(current, toggle, idx),
		))
	}
	menu.Inline(rows...)
	return menu
}
