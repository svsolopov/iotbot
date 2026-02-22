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
	Interlock    []string `yaml:"interlock"`
	Countdown    int      `yaml:"countdown"`
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
	Devices struct {
		WaterLeak []DeviceInfo `yaml:"water_leak"`
		Climate   []DeviceInfo `yaml:"climate"`
		Relay     []DeviceInfo `yaml:"relay"`
	} `yaml:"devices"`
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
	for i := range cfg.Devices.Relay {
		if cfg.Devices.Relay[i].StateKey == "" {
			cfg.Devices.Relay[i].StateKey = "state"
		}
	}
	// Установить тип по умолчанию для всех устройств
	setDefaultType(cfg.Devices.WaterLeak)
	setDefaultType(cfg.Devices.Climate)
	setDefaultType(cfg.Devices.Relay)
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
	for _, d := range cfg.Devices.WaterLeak {
		known[d.FriendlyName] = d.Type
	}
	for _, d := range cfg.Devices.Climate {
		known[d.FriendlyName] = d.Type
	}
	for _, d := range cfg.Devices.Relay {
		known[d.FriendlyName] = d.Type
	}
	return known
}

func FindDevice(devices []DeviceInfo, friendlyName string) *DeviceInfo {
	for i := range devices {
		if devices[i].FriendlyName == friendlyName {
			return &devices[i]
		}
	}
	return nil
}
