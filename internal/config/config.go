package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

var DefaultRecordTypes = []string{"A", "AAAA", "CNAME", "MX", "NS", "TXT"}

type NotifyConfig struct {
	WindowsToast      bool   `json:"windows_toast"`
	LogFile           bool   `json:"log_file"`
	WebhookURL        string `json:"webhook_url"`
	DiscordWebhookURL string `json:"discord_webhook_url"`
	SlackWebhookURL   string `json:"slack_webhook_url"`
	TelegramBotToken  string `json:"telegram_bot_token"`
	TelegramChatID    string `json:"telegram_chat_id"`
	SMTPHost          string `json:"smtp_host"`
	SMTPPort          int    `json:"smtp_port"`
	SMTPUser          string `json:"smtp_user"`
	SMTPPassword      string `json:"smtp_password"`
	SMTPFrom          string `json:"smtp_from"`
	SMTPTo            string `json:"smtp_to"`
}

type AppConfig struct {
	RecordTypes     []string     `json:"record_types"`
	IntervalSeconds int          `json:"interval_seconds"`
	Workers         int          `json:"workers"`
	TimeoutSeconds  int          `json:"timeout_seconds"`
	Retries         int          `json:"retries"`
	DoHProvider     string       `json:"doh_provider"`
	Notify          NotifyConfig `json:"notify"`
}

func DefaultConfig() AppConfig {
	return AppConfig{
		RecordTypes:     append([]string{}, DefaultRecordTypes...),
		IntervalSeconds: 300,
		Workers:         20,
		TimeoutSeconds:  8,
		Retries:         2,
		DoHProvider:     "cloudflare",
		Notify: NotifyConfig{
			WindowsToast: true,
			LogFile:      true,
			SMTPPort:     587,
		},
	}
}

func GetAppDir() (string, error) {
	if override := os.Getenv("DNS_WATCHER_HOME"); override != "" {
		return override, nil
	}
	if runtime.GOOS == "windows" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, "dns-watcher"), nil
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "dns-watcher"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "dns-watcher"), nil
}

func LoadConfig(appDir string) (AppConfig, error) {
	cfg := DefaultConfig()
	path := filepath.Join(appDir, "config.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	if len(cfg.RecordTypes) == 0 {
		cfg.RecordTypes = append([]string{}, DefaultRecordTypes...)
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 20
	}
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 8
	}
	if cfg.IntervalSeconds <= 0 {
		cfg.IntervalSeconds = 300
	}
	return cfg, nil
}

func SaveConfig(appDir string, cfg AppConfig) (string, error) {
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(appDir, "config.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	return path, os.WriteFile(path, data, 0o644)
}
