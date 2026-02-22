package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	tele "gopkg.in/telebot.v4"
)

var (
	deviceState  sync.Map
	mqttClient   mqtt.Client
	knownDevices map[string]string
)

func StartMQTT(cfg *Config, bot *tele.Bot) {
	knownDevices = cfg.KnownDevices()
	broker := fmt.Sprintf("tcp://%s:%d", cfg.MQTT.Host, cfg.MQTT.Port)

	opts := mqtt.NewClientOptions().
		AddBroker(broker).
		SetUsername(cfg.MQTT.Username).
		SetPassword(cfg.MQTT.Password).
		SetClientID("mkbot").
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetOnConnectHandler(func(c mqtt.Client) {
			log.Printf("MQTT подключён к %s", broker)
			token := c.Subscribe("zigbee2mqtt/#", 0, func(_ mqtt.Client, msg mqtt.Message) {
				handleMQTTMessage(msg, cfg, bot)
			})
			token.Wait()
			if token.Error() != nil {
				log.Printf("Ошибка подписки MQTT: %v", token.Error())
			}
			// Подписаться на ESPHome-устройства
			subscribed := make(map[string]bool)
			for name, devType := range knownDevices {
				if devType == "esphome" && !subscribed[name] {
					subscribed[name] = true
					t := c.Subscribe(name+"/#", 0, func(_ mqtt.Client, msg mqtt.Message) {
						handleMQTTMessage(msg, cfg, bot)
					})
					t.Wait()
					if t.Error() != nil {
						log.Printf("Ошибка подписки ESPHome %s: %v", name, t.Error())
					}
				}
			}
			// Запросить текущее состояние всех устройств
			requestAllStates(c, cfg)
		}).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			log.Printf("MQTT отключён: %v", err)
		})

	mqttClient = mqtt.NewClient(opts)
	token := mqttClient.Connect()
	token.Wait()
	if token.Error() != nil {
		log.Printf("Ошибка подключения MQTT: %v", token.Error())
	}
}

func handleMQTTMessage(msg mqtt.Message, cfg *Config, bot *tele.Bot) {
	topic := msg.Topic()
	parts := strings.Split(topic, "/")
	if len(parts) < 2 {
		return
	}

	// Zigbee2MQTT: zigbee2mqtt/{friendly_name}
	if parts[0] == "zigbee2mqtt" {
		handleZ2MMessage(parts, msg, cfg, bot)
		return
	}

	// ESPHome: {device}/sensor/{sensor_id}/state
	if devType, ok := knownDevices[parts[0]]; ok && devType == "esphome" {
		handleESPHomeMessage(parts, msg, cfg, bot)
	}
}

func handleZ2MMessage(parts []string, msg mqtt.Message, cfg *Config, bot *tele.Bot) {
	if parts[1] == "bridge" || parts[len(parts)-1] == "set" || parts[len(parts)-1] == "get" {
		return
	}

	var data map[string]interface{}
	if err := json.Unmarshal(msg.Payload(), &data); err != nil {
		return
	}

	friendlyName := parts[1]
	if knownDevices[friendlyName] == "" {
		return
	}
	deviceState.Store(friendlyName, data)

	// Проверяем устройства с notify: instant
	for _, section := range cfg.Sensors {
		for _, dev := range section.Items {
			if dev.Notify != "instant" || dev.FriendlyName != friendlyName || dev.StateKey == "" {
				continue
			}
			val, ok := data[dev.StateKey]
			if !ok {
				continue
			}
			if boolVal, ok := val.(bool); ok && boolVal {
				text := fmt.Sprintf("%s %s: %s (%s)", section.Emoji, section.Section, dev.Alias, dev.FriendlyName)
				recipient := &chatID{id: cfg.Telegram.ChatID}
				if _, err := bot.Send(recipient, text); err != nil {
					log.Printf("Ошибка отправки уведомления: %v", err)
				}
			}
		}
	}
}

func handleESPHomeMessage(parts []string, msg mqtt.Message, cfg *Config, bot *tele.Bot) {
	// Формат: {device}/sensor/{sensor_id}/state
	if len(parts) != 4 || parts[1] != "sensor" || parts[3] != "state" {
		return
	}
	deviceName := parts[0]
	sensorID := parts[2]
	value := string(msg.Payload())

	// Загрузить или создать map состояния устройства
	existing, _ := deviceState.Load(deviceName)
	state, _ := existing.(map[string]interface{})
	if state == nil {
		state = make(map[string]interface{})
	}

	// Запомнить старое значение для сравнения
	oldVal := state[sensorID]

	// Попробовать распарсить как float64
	var newVal interface{}
	if f, err := strconv.ParseFloat(value, 64); err == nil {
		newVal = f
	} else {
		newVal = value
	}
	state[sensorID] = newVal
	deviceState.Store(deviceName, state)

	// Уведомления для ESPHome-устройств с notify: instant
	if oldVal != newVal {
		for _, section := range cfg.Sensors {
			for _, dev := range section.Items {
				if dev.Notify != "instant" || dev.Type != "esphome" || dev.FriendlyName != deviceName || dev.StateKey != sensorID {
					continue
				}
				unit := dev.Unit
				if unit != "" {
					unit = " " + unit
				}
				text := fmt.Sprintf("%s %s: %s — %v%s", section.Emoji, section.Section, dev.Alias, newVal, unit)
				recipient := &chatID{id: cfg.Telegram.ChatID}
				if _, err := bot.Send(recipient, text); err != nil {
					log.Printf("Ошибка отправки уведомления: %v", err)
				}
			}
		}
	}
}

func requestAllStates(c mqtt.Client, cfg *Config) {
	requested := make(map[string]bool)
	count := 0

	// Сенсоры: запрашиваем полное состояние, дедуплицируя по friendly_name
	for _, section := range cfg.Sensors {
		for _, dev := range section.Items {
			if dev.Type == "esphome" || requested[dev.FriendlyName] {
				continue
			}
			requested[dev.FriendlyName] = true
			topic := fmt.Sprintf("zigbee2mqtt/%s/get", dev.FriendlyName)
			c.Publish(topic, 0, false, []byte(`{"state":""}`))
			count++
		}
	}

	// Реле: тоже дедуплицируем по friendly_name
	for _, dev := range cfg.Relay {
		if dev.Type == "esphome" || requested[dev.FriendlyName] {
			continue
		}
		requested[dev.FriendlyName] = true
		topic := fmt.Sprintf("zigbee2mqtt/%s/get", dev.FriendlyName)
		c.Publish(topic, 0, false, []byte(`{"state":""}`))
		count++
	}

	log.Printf("Запрошено состояние %d устройств", count)
}

func PublishRelay(cfg *Config, dev DeviceInfo, state string) error {
	if mqttClient == nil || !mqttClient.IsConnected() {
		return fmt.Errorf("MQTT не подключён")
	}
	topic := fmt.Sprintf("zigbee2mqtt/%s/set", dev.FriendlyName)
	payload := map[string]interface{}{dev.StateKey: state}
	if state == "ON" {
		for _, key := range dev.Interlock {
			payload[key] = "OFF"
		}
		if dev.Countdown > 0 {
			countdownKey := strings.Replace(dev.StateKey, "state", "countdown", 1)
			payload[countdownKey] = dev.Countdown
		}
	}
	data, _ := json.Marshal(payload)
	token := mqttClient.Publish(topic, 0, false, data)
	token.Wait()
	return token.Error()
}

// chatID реализует интерфейс tele.Recipient для отправки сообщений по chat ID.
type chatID struct {
	id int64
}

func (c *chatID) Recipient() string {
	return fmt.Sprintf("%d", c.id)
}
