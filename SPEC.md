# mkbot — Telegram-бот управления умным домом

## Назначение

Telegram-бот для мониторинга датчиков и управления реле умного дома через протокол MQTT. Работает поверх Zigbee2MQTT и ESPHome — получает состояния устройств и отправляет им команды. Предназначен для запуска на домашнем сервере (включая ARM64 SBC вроде Raspberry Pi / Orange Pi).

## Стек технологий

- Язык: Go (модуль `mkbot`)
- Telegram: `gopkg.in/telebot.v4` (v4 beta, Long Polling)
- MQTT: `github.com/eclipse/paho.mqtt.golang`
- Конфигурация: YAML (`gopkg.in/yaml.v3`)
- Сборка: статический бинарник, кросс-компиляция под linux/arm64

## Структура файлов

```
mkbot/
  main.go          — точка входа
  config.go        — загрузка конфигурации, структуры данных
  handlers.go      — обработчики Telegram-команд и кнопок
  mqtt.go          — MQTT-клиент, хранение состояний, публикация команд
  config.yaml      — рабочая конфигурация (секреты, не коммитить)
  config.example.yaml — шаблон конфигурации
```

## Конфигурация

Единственный файл `config.yaml`. Формат:

```yaml
telegram:
  token: "BOT_TOKEN"        # токен от @BotFather
  chat_id: -123456789       # ID чата (или группы), которому разрешён доступ

mqtt:
  host: "192.168.1.100"
  port: 1883                # необязательно, по умолчанию 1883
  username: "user"
  password: "pass"

sensors:                    # произвольные секции датчиков
  - section: "Климат"
    emoji: "🌡"
    items:
      - friendly_name: "temp_sensor"
        alias: "Гостиная"
        # type: z2m             — необязательно, по умолчанию "z2m"
        # без state_key → автодетект temperature/humidity

  - section: "Протечки"
    emoji: "💧"
    items:
      - friendly_name: "water_leak_kitchen"
        alias: "Кухня"
        state_key: "water_leak"
        notify: instant       # мгновенное уведомление при срабатывании

  - section: "Счётчики воды"
    emoji: "🔢"
    items:
      - friendly_name: "pulsar"
        alias: "ХВС"
        type: esphome
        state_key: "pulsar_1_channel_1"
        unit: "м³"
        notify: instant       # уведомление при изменении показаний

relay:                      # управляемые реле
  - friendly_name: "relay1"
    alias: "Бойлер"
    # state_key: "state"       — необязательно, по умолчанию "state"
    # countdown: 900           — автоотключение через N секунд (0 = нет)
    # interlock: ["state_l2"]  — при ON выключить указанные каналы того же устройства
```

### SensorSection

| Поле | Тип | Обязательно | Описание |
|------|-----|-------------|----------|
| `section` | string | да | Название секции, отображаемое в `/sensors` |
| `emoji` | string | да | Эмодзи секции, выводится перед названием |
| `items` | []DeviceInfo | да | Список устройств в секции |

### Поля устройства (DeviceInfo)

| Поле | Тип | Обязательно | Описание |
|------|-----|-------------|----------|
| `friendly_name` | string | да | Имя устройства в Zigbee2MQTT или ESPHome |
| `alias` | string | да | Отображаемое имя в Telegram |
| `type` | string | нет | Тип источника: `"z2m"` (Zigbee2MQTT, по умолчанию) или `"esphome"` |
| `state_key` | string | нет | Ключ состояния. Для z2m: ключ в JSON (по умолчанию `"state"`). Для esphome: `sensor_id` из топика. Для сенсоров: если не указан — автодетект `temperature`/`humidity` |
| `unit` | string | нет | Единица измерения для отображения (например, `"м³"`, `"°C"`). Используется в `/sensors` |
| `notify` | string | нет | Режим уведомлений: `"instant"` — мгновенное уведомление при изменении значения (не зависит от типа устройства). Первое получение значения игнорируется. По умолчанию пусто — только по `/sensors` |
| `countdown` | int | нет | Автоотключение: секунды до автоматического OFF. Ключ countdown формируется заменой `state` на `countdown` в `state_key` |
| `interlock` | []string | нет | Список state_key каналов, которые нужно выключить при включении данного канала (взаимная блокировка) |

### Многоканальные устройства

Одно физическое устройство (один `friendly_name`) может иметь несколько записей в конфигурации с разными `state_key`. Пример — двухканальное реле ванной:

```yaml
- friendly_name: "bathroom"
  alias: "Ванная"
  state_key: "state_left"
- friendly_name: "bathroom"
  alias: "Ванная подсветка"
  state_key: "state_right"
```

### Interlock + Countdown (пример: управление краном)

Кран управляется двумя каналами реле: один открывает, другой закрывает. Они не должны работать одновременно, и оба должны автоматически выключаться через 60 секунд:

```yaml
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

При включении "Открыть кран":
1. Отправляется `{"state_l1": "ON", "state_l2": "OFF", "countdown_l1": 60}`
2. Через 60 секунд устройство само отключит `state_l1`

## Архитектура и потоки данных

```
Пользователь Telegram
       |
       v
  Telegram Bot API (Long Polling, 10s timeout)
       |
       v
  handlers.go — авторизация по chat_id, команды, inline-кнопки
       |                                    ^
       v                                    |
  mqtt.go — PublishRelay()          deviceState (sync.Map)
       |                                    ^
       v                                    |
  MQTT Broker  -------->  handleMQTTMessage()
       ^                         |
       |                         v
  Zigbee2MQTT            Уведомления (notify: instant) -> Telegram
       ^
       |
  Zigbee-устройства
```

## Модуль main.go — точка входа

Последовательность запуска:
1. Загрузить `config.yaml` через `LoadConfig()`
2. Создать Telegram-бот с Long Polling (timeout 10 секунд)
3. Зарегистрировать обработчики через `SetupHandlers()`
4. Зарегистрировать команды бота (`/sensors`, `/relay`)
5. Запустить MQTT-клиент в отдельной горутине (`go StartMQTT()`)
6. Запустить бот (`bot.Start()` — блокирующий вызов)

## Модуль config.go — конфигурация

### Структуры

- `DeviceInfo` — описание одного устройства (см. таблицу полей выше)
- `SensorSection` — секция датчиков: название, эмодзи, список items
- `Config` — корневая структура: секции `Telegram`, `MQTT`, `Sensors`, `Relay`

### Функции

- `LoadConfig(path string) (*Config, error)` — читает YAML-файл, парсит, применяет значения по умолчанию (порт 1883, state_key "state" для реле, type "z2m" для всех устройств)
- `(*Config) KnownDevices() map[string]string` — возвращает отображение `friendly_name -> type` для всех устройств из конфигурации (для фильтрации MQTT-сообщений и определения обработчика)

## Модуль mqtt.go — MQTT-клиент

### Глобальное состояние

- `deviceState sync.Map` — потокобезопасное хранилище состояний устройств. Ключ: `friendly_name` (string), значение: `map[string]interface{}` (распарсенный JSON от Zigbee2MQTT, или собранный map `sensor_id -> value` для ESPHome)
- `mqttClient mqtt.Client` — экземпляр MQTT-клиента
- `knownDevices map[string]string` — отображение `friendly_name -> type` из конфигурации

### StartMQTT(cfg, bot)

- Инициализирует `knownDevices` из конфигурации
- Подключается к MQTT-брокеру по `tcp://host:port`
- Параметры: ClientID `"mkbot"`, автореконнект, интервал переподключения 5 секунд
- При подключении: подписка на `zigbee2mqtt/#` (QoS 0), дополнительно на `{name}/#` для каждого ESPHome-устройства, затем запрос текущих состояний z2m-устройств
- При потере соединения: логирование

### handleMQTTMessage(msg, cfg, bot)

Маршрутизатор входящих MQTT-сообщений. Разбирает топик по `/` и передаёт в соответствующий обработчик:
- Если `parts[0] == "zigbee2mqtt"` — вызывает `handleZ2MMessage`
- Если `parts[0]` есть в `knownDevices` с типом `"esphome"` — вызывает `handleESPHomeMessage`

### handleZ2MMessage(parts, msg, cfg, bot)

Обработка сообщений Zigbee2MQTT:
1. Игнорировать топики `bridge` и заканчивающиеся на `/set` или `/get`
2. Распарсить JSON payload в `map[string]interface{}`
3. Извлечь `friendly_name` из `parts[1]`
4. **Проверка по белому списку**: если `friendly_name` нет в `knownDevices` — игнорировать
5. Загрузить предыдущее состояние для сравнения, сохранить новое в `deviceState`
6. Для items с `notify: "instant"` — вызвать `sendInstantNotification` при изменении значения

### handleESPHomeMessage(parts, msg, cfg, bot)

Обработка сообщений ESPHome. Формат топика: `{device}/sensor/{sensor_id}/state`.
1. Проверить что топик имеет ровно 4 части, `parts[1] == "sensor"`, `parts[3] == "state"`
2. Загрузить текущий `map[string]interface{}` из `deviceState` (или создать новый)
3. Запомнить старое значение для сравнения
4. Записать `sensor_id -> значение` (попытка парсинга как `float64`, иначе строка)
5. Сохранить обратно в `deviceState`
6. Для items с `notify: "instant"` — вызвать `sendInstantNotification` при изменении значения

### sendInstantNotification(bot, cfg, section, dev, oldVal, newVal)

Единая функция уведомлений для всех типов устройств:
1. Если `oldVal == nil` (первое получение значения) — уведомлять только при `bool true` (тревога), остальные значения игнорировать
2. Cooldown: не чаще одного раза в 5 минут на устройство+ключ (подавляет флаппинг)
3. Для bool: `true` → "⚠️ сработал", `false` → "норма"
4. Для числа/строки: значение с единицей измерения

### requestAllStates(client, cfg)

При подключении/переподключении запрашивает текущее состояние z2m-устройств (ESPHome-устройства пропускаются — они публикуют состояние сами):
- Для каждого z2m-устройства публикует в `zigbee2mqtt/{name}/get` запрос с нужным `state_key`
- Реле дедуплицируются по `friendly_name` (чтобы не слать несколько запросов одному многоканальному устройству)

### PublishRelay(cfg, dev, state)

Отправка команды управления реле:
1. Проверить подключение MQTT
2. Формат топика: `zigbee2mqtt/{friendly_name}/set`
3. Базовый payload: `{state_key: "ON"/"OFF"}`
4. Если `state == "ON"`:
   - Добавить в payload interlock-каналы со значением `"OFF"`
   - Если `countdown > 0` — добавить ключ countdown (формируется заменой `state` на `countdown` в `state_key`, например `state_l1` -> `countdown_l1`)
5. Сериализовать в JSON, опубликовать (QoS 0), дождаться подтверждения

### chatID

Вспомогательная структура, реализующая интерфейс `tele.Recipient` — позволяет отправлять сообщения по числовому chat ID.

## Модуль handlers.go — Telegram-обработчики

### Авторизация

Middleware проверяет `c.Chat().ID == cfg.Telegram.ChatID`. Если не совпадает — запрос молча отбрасывается (return nil). Все команды доступны только в настроенном чате.

### Команды

#### /start
Отправляет текстовое меню с перечнем доступных команд.

#### /sensors
Формирует текстовое сообщение с текущими показаниями. Секции формируются динамически из `cfg.Sensors`:

Для каждой секции выводится заголовок `"{emoji} {section}:"`, затем для каждого item:
- Если `state_key` пуст — автодетект: показать `temperature°C, humidity%`
- Если `state_key` задан — прочитать значение:
  - `bool true` → "⚠️ сработал"
  - `bool false` → "норма"
  - число/строка → `"значение unit"`
- Если данных нет: `Alias: нет данных`

#### /relay
Отправляет сообщение с inline-клавиатурой, сформированной функцией `relayKeyboard()`.

### Inline-кнопки

Каждое реле отображается строкой из двух кнопок:
1. Название (alias) — кнопка `relay_noop`, ничего не делает
2. Текущее состояние (ON/OFF) — кнопка `relay_on` или `relay_off`

В callback data передаётся индекс реле в массиве `cfg.Relay`.

#### relay_on (обработчик)
1. Парсит индекс из callback data, валидирует границы
2. Вызывает `PublishRelay(cfg, dev, "ON")`
3. Отправляет callback-ответ "Отправлено: ON"
4. Ждёт 1 секунду (чтобы устройство успело ответить)
5. Обновляет клавиатуру (Edit) с актуальным состоянием

#### relay_off (обработчик)
Аналогично relay_on, но отправляет "OFF".

#### relay_noop (обработчик)
Пустой ответ на callback — просто закрывает "часики" в Telegram.

### relayKeyboard(cfg)

Строит `*tele.ReplyMarkup` с inline-кнопками:
- Для каждого реле из конфигурации:
  - Загружает текущее состояние из `deviceState` по `friendly_name` + `state_key`
  - Определяет текущее значение (по умолчанию "OFF")
  - Формирует строку: [Alias | relay_noop] [ON/OFF | relay_on/relay_off]
  - Если сейчас ON — кнопка переключения будет relay_off, и наоборот

## MQTT-топики

### Zigbee2MQTT

| Направление | Топик | Описание |
|-------------|-------|----------|
| Входящие | `zigbee2mqtt/{friendly_name}` | JSON с текущим состоянием устройства |
| Входящие | `zigbee2mqtt/bridge/*` | Служебные — игнорируются |
| Исходящие (чтение) | `zigbee2mqtt/{friendly_name}/get` | Запрос текущего состояния |
| Исходящие (запись) | `zigbee2mqtt/{friendly_name}/set` | Отправка команды устройству |

### Пример JSON от климатического датчика

```json
{"temperature": 23.5, "humidity": 45, "battery": 87}
```

### Пример JSON от датчика протечки

```json
{"water_leak": true, "battery": 100}
```

### Пример JSON от реле

```json
{"state": "ON"}
```

Многоканальное:
```json
{"state_l1": "ON", "state_l2": "OFF", "state_l3": "OFF"}
```

### Пример команды реле с interlock и countdown

Публикуется в `zigbee2mqtt/bath_relay/set`:
```json
{"state_l1": "ON", "state_l2": "OFF", "countdown_l1": 60}
```

### ESPHome

ESPHome публикует состояние сенсоров в топики вида `{device}/sensor/{sensor_id}/state` со скалярным значением (не JSON).

| Направление | Топик | Описание |
|-------------|-------|----------|
| Входящие | `{device}/sensor/{sensor_id}/state` | Скалярное значение сенсора (число или строка) |

Пример: `pulsar/sensor/pulsar_1_channel_1/state` с payload `123.456`

## Сборка

```bash
# Обычная сборка
go build -o mkbot .

# Кросс-компиляция под ARM64
GOARCH=arm64 GOOS=linux go build -o mkbot-arm64 .
```

## Запуск

```bash
# Подготовить config.yaml по шаблону config.example.yaml
cp config.example.yaml config.yaml
# Отредактировать config.yaml — указать токен, chat_id, MQTT-брокер, устройства

./mkbot
```

Бот логирует в stdout: подключение MQTT, ошибки, отправки.

## План развития

### Безопасность (приоритетно)

- [ ] Секреты через переменные окружения: поддержать `${ENV_VAR}` в YAML или флаги командной строки для `token` и `password`
- [ ] MQTT через TLS: добавить опциональные поля `tls_cert`, `tls_key`, `tls_ca` в секцию mqtt; переключать схему на `ssl://`
- [ ] Уникальный MQTT Client ID: генерировать `"mkbot-" + hostname` или UUID вместо статического `"mkbot"`
- [ ] Rate limiting на управление реле: минимальный интервал между командами на одно реле (2-3 секунды)
- [x] Debounce уведомлений (notify: instant): cooldown 5 минут на устройство+ключ
- [ ] QoS 1 для критических операций: подписка и публикация команд реле
- [ ] Авторизация по user ID для команд управления (дополнительно к chat ID)
- [ ] Добавить `.gitignore` с записью `config.yaml`

### Функциональность

- [ ] Уведомления о потере связи с устройством (device availability через `zigbee2mqtt/{name}/availability`)
- [ ] Уведомления о низком заряде батареи датчиков
- [ ] История показаний климатических датчиков (хранение в SQLite или файле)
- [ ] Группы реле с массовым управлением ("Выключить всё")
- [ ] Сценарии: цепочки действий по расписанию или по событию (например, при утечке — закрыть кран)
- [ ] Веб-интерфейс для мониторинга (опционально)

### Инфраструктура

- [ ] Systemd unit-файл для автозапуска
- [ ] Dockerfile для контейнерного деплоя
- [ ] Graceful shutdown (обработка SIGTERM: отключение MQTT, остановка бота)
- [ ] Health check endpoint (HTTP /healthz)
