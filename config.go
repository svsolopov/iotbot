package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type DeviceInfo struct {
	FriendlyName string   `yaml:"friendly_name"`
	Alias        string   `yaml:"alias"`
	Type         string   `yaml:"type"`
	StateKey     string   `yaml:"state_key"`
	Unit         string   `yaml:"unit"`
	Notify       string   `yaml:"notify"`
	Interlock    []string `yaml:"interlock"`
	Countdown    int      `yaml:"countdown"`
}

type SensorSection struct {
	Section string       `yaml:"section"`
	Emoji   string       `yaml:"emoji"`
	Items   []DeviceInfo `yaml:"items"`
}

type Config struct {
	Telegram struct {
		Token  string `yaml:"token"`
		ChatID int64  `yaml:"chat_id"`
	} `yaml:"telegram"`
	MQTT struct {
		Host     string `yaml:"host"`
		Port     int    `yaml:"port"`
		Username string `yaml:"username"`
		Password string `yaml:"password"`
	} `yaml:"mqtt"`
	Sensors []SensorSection `yaml:"sensors"`
	Relay   []DeviceInfo    `yaml:"relay"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if cfg.MQTT.Port == 0 {
		cfg.MQTT.Port = 1883
	}
	for i := range cfg.Relay {
		if cfg.Relay[i].StateKey == "" {
			cfg.Relay[i].StateKey = "state"
		}
	}
	// Установить тип по умолчанию для всех устройств
	for i := range cfg.Sensors {
		setDefaultType(cfg.Sensors[i].Items)
	}
	setDefaultType(cfg.Relay)
	return &cfg, nil
}

func setDefaultType(devs []DeviceInfo) {
	for i := range devs {
		if devs[i].Type == "" {
			devs[i].Type = "z2m"
		}
	}
}

func (cfg *Config) KnownDevices() map[string]string {
	known := make(map[string]string)
	for _, section := range cfg.Sensors {
		for _, d := range section.Items {
			known[d.FriendlyName] = d.Type
		}
	}
	for _, d := range cfg.Relay {
		known[d.FriendlyName] = d.Type
	}
	return known
}
