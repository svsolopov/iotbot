# iotbot

Легковесный Telegram-бот для управления умным домом без тяжёлых платформ вроде Home Assistant или OpenHAB.

Если у вас есть Zigbee-устройства (датчики, реле) и/или ESPHome-контроллеры, для базовой автоматизации достаточно MQTT-брокера и этого бота. Никаких веб-интерфейсов, баз данных и сложных настроек — только Telegram, один YAML-файл конфигурации и один бинарник, который запускается на любом сервере, включая одноплатники (Raspberry Pi, Orange Pi и т.д.).

## Возможности

- **Произвольные секции датчиков** — климат, протечки, счётчики и любые другие группы сенсоров
- **Мгновенные уведомления** — параметр `notify: instant` для любого датчика: bool-сенсоры (протечки, дым), числовые (счётчики воды при изменении показаний)
- **Реле** — включение/выключение через inline-кнопки в Telegram
- **Interlock** — взаимная блокировка каналов реле
- **Countdown** — автоотключение реле через заданное время
- **Многоканальные устройства** — несколько каналов одного реле как отдельные кнопки

## Команды бота

| Команда | Описание |
|---------|----------|
| `/sensors` | Показания датчиков по всем секциям |
| `/relay` | Inline-клавиатура управления реле |

## Быстрый старт

```bash
# Клонировать
git clone https://github.com/svsolopov/iotbot.git
cd iotbot

# Настроить
cp config.example.yaml config.yaml
# Отредактировать config.yaml — указать токен бота, chat_id, MQTT-брокер, устройства

# Собрать и запустить
go build -o iotbot .
./iotbot
```

## Конфигурация

```yaml
telegram:
  token: "BOT_TOKEN"
  chat_id: 123456789

mqtt:
  host: "192.168.1.100"
  port: 1883
  username: "user"
  password: "pass"

sensors:
  - section: "Климат"
    emoji: "🌡"
    items:
      - friendly_name: "temp_sensor"
        alias: "Гостиная"
        # z2m + нет state_key → автодетект temperature/humidity

  - section: "Протечки"
    emoji: "💧"
    items:
      - friendly_name: "water_leak_kitchen"
        alias: "Кухня"
        state_key: "water_leak"
        notify: instant       # мгновенное уведомление при bool true

  - section: "Счётчики воды"
    emoji: "🔢"
    items:
      - friendly_name: "pulsar"
        alias: "ХВС"
        type: esphome
        state_key: "pulsar_1_channel_1"
        unit: "м³"
        notify: instant       # уведомление при изменении показаний

relay:
  - friendly_name: "relay_boiler"
    alias: "Бойлер"

  # Многоканальное реле с interlock и countdown
  - friendly_name: "bath_relay"
    alias: "Открыть кран"
    state_key: "state_l1"
    countdown: 60
    interlock: ["state_l2"]
  - friendly_name: "bath_relay"
    alias: "Закрыть кран"
    state_key: "state_l2"
    countdown: 60
    interlock: ["state_l1"]
```

## Стек

- Go
- [telebot v4](https://gopkg.in/telebot.v4) — Telegram Bot API
- [paho.mqtt.golang](https://github.com/eclipse/paho.mqtt.golang) — MQTT-клиент
- YAML — конфигурация
