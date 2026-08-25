package main

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type config struct {
	HTTPAddr       string
	VikunjaURL     string
	WebhookSecret  string
	TelegramToken  string
	TelegramChatID string
	Timezone       string
	Location       *time.Location
}

func loadConfig(getenv func(string) string) (config, error) {
	cfg := config{
		HTTPAddr:       strings.TrimSpace(getenv("HTTP_ADDR")),
		VikunjaURL:     strings.TrimSpace(getenv("VIKUNJA_URL")),
		WebhookSecret:  getenv("VIKUNJA_WEBHOOK_SECRET"),
		TelegramToken:  getenv("TELEGRAM_BOT_TOKEN"),
		TelegramChatID: strings.TrimSpace(getenv("TELEGRAM_CHAT_ID")),
		Timezone:       strings.TrimSpace(getenv("TZ")),
	}

	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = ":8080"
	}
	if cfg.Timezone == "" {
		cfg.Timezone = "Europe/Moscow"
	}

	required := []struct {
		name  string
		value string
	}{
		{name: "VIKUNJA_URL", value: cfg.VikunjaURL},
		{name: "VIKUNJA_WEBHOOK_SECRET", value: cfg.WebhookSecret},
		{name: "TELEGRAM_BOT_TOKEN", value: cfg.TelegramToken},
		{name: "TELEGRAM_CHAT_ID", value: cfg.TelegramChatID},
	}
	for _, variable := range required {
		if variable.value == "" {
			return config{}, fmt.Errorf("%s is required", variable.name)
		}
	}

	parsedURL, err := url.Parse(cfg.VikunjaURL)
	if err != nil {
		return config{}, errors.New("VIKUNJA_URL is invalid")
	}
	validScheme := parsedURL.Scheme == "http" || parsedURL.Scheme == "https"
	if !validScheme || parsedURL.Host == "" || parsedURL.User != nil || parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return config{}, errors.New("VIKUNJA_URL must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	cfg.VikunjaURL = strings.TrimRight(cfg.VikunjaURL, "/")

	cfg.Location, err = time.LoadLocation(cfg.Timezone)
	if err != nil {
		return config{}, fmt.Errorf("TZ %q is invalid", cfg.Timezone)
	}

	return cfg, nil
}
