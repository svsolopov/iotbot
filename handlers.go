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

		if len(cfg.Devices.Climate) > 0 {
			lines := []string{"\xf0\x9f\x8c\xa1 Климат:"}
			for _, dev := range cfg.Devices.Climate {
				val, ok := deviceState.Load(dev.FriendlyName)
				if !ok {
					lines = append(lines, fmt.Sprintf("  %s: нет данных", dev.Alias))
					continue
				}
				state, _ := val.(map[string]interface{})
				if dev.Type == "esphome" {
					if dev.StateKey != "" {
						if v, ok := state[dev.StateKey]; ok {
							unit := dev.Unit
							if unit != "" {
								unit = " " + unit
							}
							lines = append(lines, fmt.Sprintf("  %s: %v%s", dev.Alias, v, unit))
						} else {
							lines = append(lines, fmt.Sprintf("  %s: нет данных", dev.Alias))
						}
					} else {
						lines = append(lines, fmt.Sprintf("  %s: нет данных", dev.Alias))
					}
				} else {
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

		if len(cfg.Devices.WaterLeak) > 0 {
			lines := []string{"\xf0\x9f\x92\xa7 Протечки:"}
			for _, dev := range cfg.Devices.WaterLeak {
				val, ok := deviceState.Load(dev.FriendlyName)
				if !ok {
					lines = append(lines, fmt.Sprintf("  %s: нет данных", dev.Alias))
					continue
				}
				state, _ := val.(map[string]interface{})
				leak, _ := state["water_leak"].(bool)
				if leak {
					lines = append(lines, fmt.Sprintf("  %s: \xf0\x9f\x9a\xa8 УТЕЧКА", dev.Alias))
				} else {
					lines = append(lines, fmt.Sprintf("  %s: норма", dev.Alias))
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
		if len(cfg.Devices.Relay) == 0 {
			return c.Send("Нет реле в конфигурации")
		}
		return c.Send("Реле:", relayKeyboard(cfg))
	})

	bot.Handle(&tele.InlineButton{Unique: "relay_on"}, func(c tele.Context) error {
		idx, err := strconv.Atoi(c.Callback().Data)
		if err != nil || idx < 0 || idx >= len(cfg.Devices.Relay) {
			return c.Respond(&tele.CallbackResponse{Text: "Некорректный индекс реле"})
		}
		dev := cfg.Devices.Relay[idx]
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
		if err != nil || idx < 0 || idx >= len(cfg.Devices.Relay) {
			return c.Respond(&tele.CallbackResponse{Text: "Некорректный индекс реле"})
		}
		dev := cfg.Devices.Relay[idx]
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
	for i, dev := range cfg.Devices.Relay {
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
