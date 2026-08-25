package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const maxTelegramResponseSize = 64 << 10

var errTelegramTransport = errors.New("telegram transport error")

type telegramClient struct {
	client   *http.Client
	endpoint string
	token    string
	chatID   string
}

type telegramRequest struct {
	ChatID             string                     `json:"chat_id"`
	Text               string                     `json:"text"`
	ParseMode          string                     `json:"parse_mode"`
	LinkPreviewOptions telegramLinkPreviewOptions `json:"link_preview_options"`
}

type telegramLinkPreviewOptions struct {
	IsDisabled bool `json:"is_disabled"`
}

type telegramResponse struct {
	OK bool `json:"ok"`
}

func newTelegramClient(baseURL, token, chatID string, client *http.Client) *telegramClient {
	return &telegramClient{
		client:   client,
		endpoint: strings.TrimRight(baseURL, "/") + "/bot" + token + "/sendMessage",
		token:    token,
		chatID:   chatID,
	}
}

func (c *telegramClient) SendMessage(ctx context.Context, text string) error {
	payload, err := json.Marshal(telegramRequest{
		ChatID:    c.chatID,
		Text:      text,
		ParseMode: "HTML",
		LinkPreviewOptions: telegramLinkPreviewOptions{
			IsDisabled: true,
		},
	})
	if err != nil {
		return errors.New("encoding Telegram request")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return errors.New("creating Telegram request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return sanitizeTelegramTransportError(err, c.token)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTelegramResponseSize+1))
	if err != nil {
		return errors.New("reading Telegram response")
	}
	if len(body) > maxTelegramResponseSize {
		return errors.New("Telegram response is too large")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Telegram returned HTTP status %d", resp.StatusCode)
	}

	var result telegramResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return errors.New("decoding Telegram response")
	}
	if !result.OK {
		return errors.New("Telegram rejected the message")
	}

	return nil
}

func sanitizeTelegramTransportError(err error, token string) error {
	cause := err
	var urlError *url.Error
	if errors.As(err, &urlError) && urlError.Err != nil {
		cause = urlError.Err
	}

	message := cause.Error()
	if token != "" {
		message = strings.ReplaceAll(message, token, "[redacted]")
	}
	return fmt.Errorf("%w: %s", errTelegramTransport, message)
}
