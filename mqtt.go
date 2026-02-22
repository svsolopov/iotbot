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
		handleESPHomeMessage(parts, msg)
	}
}

func handleZ2MMessage(parts []string, msg mqtt.Message, cfg *Config, bot *tele.Bot) {
	if parts[1] == "bridge" || parts[len(parts)-1] == "set" {
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

	// Проверяем утечку воды
	if leak, ok := data["water_leak"]; ok {
		if leakBool, ok := leak.(bool); ok && leakBool {
			dev := FindDevice(cfg.Devices.WaterLeak, friendlyName)
			if dev != nil {
				text := fmt.Sprintf("\xf0\x9f\x9a\xa8 УТЕЧКА ВОДЫ: %s (%s)", dev.Alias, dev.FriendlyName)
				recipient := &chatID{id: cfg.Telegram.ChatID}
				_, err := bot.Send(recipient, text)
				if err != nil {
					log.Printf("Ошибка отправки уведомления: %v", err)
				}
			}
		}
	}
}

func handleESPHomeMessage(parts []string, msg mqtt.Message) {
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

	// Попробовать распарсить как float64
	if f, err := strconv.ParseFloat(value, 64); err == nil {
		state[sensorID] = f
	} else {
		state[sensorID] = value
	}
	deviceState.Store(deviceName, state)
}

func requestAllStates(c mqtt.Client, cfg *Config) {
	var all []DeviceInfo
	for _, dev := range cfg.Devices.Climate {
		if dev.Type != "esphome" {
			all = append(all, dev)
		}
	}
	for _, dev := range cfg.Devices.WaterLeak {
		if dev.Type != "esphome" {
			all = append(all, dev)
		}
	}

	// Для реле запрашиваем по конкретному state_key, дедуплицируя по friendly_name
	requested := make(map[string]bool)
	for _, dev := range cfg.Devices.Relay {
		if dev.Type == "esphome" || requested[dev.FriendlyName] {
			continue
		}
		requested[dev.FriendlyName] = true
		all = append(all, dev)
	}

	for _, dev := range all {
		topic := fmt.Sprintf("zigbee2mqtt/%s/get", dev.FriendlyName)
		payload := fmt.Sprintf(`{"%s":""}`, dev.StateKey)
		if dev.StateKey == "" {
			payload = `{"state":""}`
		}
		c.Publish(topic, 0, false, []byte(payload))
	}
	log.Printf("Запрошено состояние %d устройств", len(all))
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
