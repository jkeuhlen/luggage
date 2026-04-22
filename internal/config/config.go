package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

const (
	DefaultDays          = 90
	DefaultGranularity   = "daily"
	DefaultInclusion     = "all_runs"
	DefaultView          = "typed"
	DefaultSessionCutoff = int64(300000)
	DefaultAnomalyWindow = 14
	DefaultAnomalySigma  = 2.5
)

type Config struct {
	DefaultDays        int     `json:"default_days"`
	DefaultGranularity string  `json:"default_granularity"`
	DefaultInclusion   string  `json:"default_inclusion"`
	DefaultView        string  `json:"default_view"`
	SessionCutoffMs    int64   `json:"session_cutoff_ms"`
	AnomalyWindow      int     `json:"anomaly_window"`
	AnomalySigma       float64 `json:"anomaly_sigma"`
}

func Default() Config {
	return Config{
		DefaultDays:        DefaultDays,
		DefaultGranularity: DefaultGranularity,
		DefaultInclusion:   DefaultInclusion,
		DefaultView:        DefaultView,
		SessionCutoffMs:    DefaultSessionCutoff,
		AnomalyWindow:      DefaultAnomalyWindow,
		AnomalySigma:       DefaultAnomalySigma,
	}
}

func DataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".luggage"), nil
}

func ConfigPath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func DBPath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "luggage.db"), nil
}

func EnsureDataDir() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func Load() (Config, error) {
	cfg := Default()
	path, err := ConfigPath()
	if err != nil {
		return cfg, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c *Config) ApplyDefaults() {
	if c.DefaultDays <= 0 {
		c.DefaultDays = DefaultDays
	}
	if c.DefaultGranularity == "" {
		c.DefaultGranularity = DefaultGranularity
	}
	if c.DefaultInclusion == "" {
		c.DefaultInclusion = DefaultInclusion
	}
	if c.DefaultView == "" {
		c.DefaultView = DefaultView
	}
	if c.SessionCutoffMs <= 0 {
		c.SessionCutoffMs = DefaultSessionCutoff
	}
	if c.AnomalyWindow <= 0 {
		c.AnomalyWindow = DefaultAnomalyWindow
	}
	if c.AnomalySigma <= 0 {
		c.AnomalySigma = DefaultAnomalySigma
	}
}

func (c Config) Validate() error {
	if c.DefaultDays <= 0 {
		return fmt.Errorf("default_days must be > 0")
	}
	switch c.DefaultGranularity {
	case "hourly", "daily", "weekly":
	default:
		return fmt.Errorf("default_granularity must be hourly|daily|weekly")
	}
	switch c.DefaultInclusion {
	case "all_runs", "success_only":
	default:
		return fmt.Errorf("default_inclusion must be all_runs|success_only")
	}
	switch c.DefaultView {
	case "typed", "resolved":
	default:
		return fmt.Errorf("default_view must be typed|resolved")
	}
	if c.SessionCutoffMs <= 0 {
		return fmt.Errorf("session_cutoff_ms must be > 0")
	}
	if c.AnomalyWindow <= 1 {
		return fmt.Errorf("anomaly_window must be > 1")
	}
	if c.AnomalySigma <= 0 {
		return fmt.Errorf("anomaly_sigma must be > 0")
	}
	return nil
}

func Save(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if _, err := EnsureDataDir(); err != nil {
		return err
	}
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func SetValue(cfg *Config, key, value string) error {
	switch key {
	case "default_days":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.DefaultDays = n
	case "default_granularity":
		cfg.DefaultGranularity = value
	case "default_inclusion":
		cfg.DefaultInclusion = value
	case "default_view":
		cfg.DefaultView = value
	case "session_cutoff_ms":
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return err
		}
		cfg.SessionCutoffMs = n
	case "anomaly_window":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.AnomalyWindow = n
	case "anomaly_sigma":
		n, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return err
		}
		cfg.AnomalySigma = n
	default:
		return fmt.Errorf("unknown config key: %s", key)
	}
	cfg.ApplyDefaults()
	return cfg.Validate()
}

func GetValue(cfg Config, key string) (string, error) {
	switch key {
	case "default_days":
		return strconv.Itoa(cfg.DefaultDays), nil
	case "default_granularity":
		return cfg.DefaultGranularity, nil
	case "default_inclusion":
		return cfg.DefaultInclusion, nil
	case "default_view":
		return cfg.DefaultView, nil
	case "session_cutoff_ms":
		return strconv.FormatInt(cfg.SessionCutoffMs, 10), nil
	case "anomaly_window":
		return strconv.Itoa(cfg.AnomalyWindow), nil
	case "anomaly_sigma":
		return strconv.FormatFloat(cfg.AnomalySigma, 'f', -1, 64), nil
	default:
		return "", fmt.Errorf("unknown config key: %s", key)
	}
}

func Keys() []string {
	return []string{
		"default_days",
		"default_granularity",
		"default_inclusion",
		"default_view",
		"session_cutoff_ms",
		"anomaly_window",
		"anomaly_sigma",
	}
}
