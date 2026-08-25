package main

import (
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	values := map[string]string{
		"VIKUNJA_URL":            "https://vikunja.example.com///",
		"VIKUNJA_WEBHOOK_SECRET": "webhook-secret",
		"TELEGRAM_BOT_TOKEN":     "bot-token",
		"TELEGRAM_CHAT_ID":       "-100123",
	}

	cfg, err := loadConfig(mapGetenv(values))
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, ":8080")
	}
	if cfg.VikunjaURL != "https://vikunja.example.com" {
		t.Errorf("VikunjaURL = %q", cfg.VikunjaURL)
	}
	if cfg.Timezone != "Europe/Moscow" {
		t.Errorf("Timezone = %q, want Europe/Moscow", cfg.Timezone)
	}
	if cfg.Location == nil || cfg.Location.String() != "Europe/Moscow" {
		t.Errorf("Location = %v", cfg.Location)
	}
}

func TestLoadConfigRequiresValues(t *testing.T) {
	base := map[string]string{
		"VIKUNJA_URL":            "https://vikunja.example.com",
		"VIKUNJA_WEBHOOK_SECRET": "webhook-secret",
		"TELEGRAM_BOT_TOKEN":     "bot-token",
		"TELEGRAM_CHAT_ID":       "-100123",
	}

	for name := range base {
		t.Run(name, func(t *testing.T) {
			values := mapsClone(base)
			delete(values, name)
			_, err := loadConfig(mapGetenv(values))
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("loadConfig() error = %v, want error containing %q", err, name)
			}
		})
	}
}

func TestLoadConfigRejectsInvalidURLAndTimezone(t *testing.T) {
	base := map[string]string{
		"VIKUNJA_URL":            "https://vikunja.example.com",
		"VIKUNJA_WEBHOOK_SECRET": "webhook-secret",
		"TELEGRAM_BOT_TOKEN":     "bot-token",
		"TELEGRAM_CHAT_ID":       "-100123",
	}

	tests := []struct {
		name   string
		key    string
		value  string
		needle string
	}{
		{name: "relative URL", key: "VIKUNJA_URL", value: "/vikunja", needle: "VIKUNJA_URL"},
		{name: "URL query", key: "VIKUNJA_URL", value: "https://vikunja.example.com?q=1", needle: "VIKUNJA_URL"},
		{name: "timezone", key: "TZ", value: "Mars/Olympus", needle: "TZ"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := mapsClone(base)
			values[test.key] = test.value
			_, err := loadConfig(mapGetenv(values))
			if err == nil || !strings.Contains(err.Error(), test.needle) {
				t.Fatalf("loadConfig() error = %v, want error containing %q", err, test.needle)
			}
		})
	}
}

func mapGetenv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func mapsClone(source map[string]string) map[string]string {
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}
