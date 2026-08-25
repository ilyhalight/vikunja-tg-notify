package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTelegramClientSendsMessage(t *testing.T) {
	const token = "123456:test-token"
	var received telegramRequest
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/bot"+token+"/sendMessage" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
		}
		return telegramHTTPResponse(http.StatusOK, `{"ok":true,"result":{}}`), nil
	})

	client := newTelegramClient(
		"https://api.telegram.test",
		token,
		"-100123",
		&http.Client{Transport: transport, Timeout: time.Second},
	)
	if err := client.SendMessage(context.Background(), "<b>Hello</b>"); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if received.ChatID != "-100123" || received.Text != "<b>Hello</b>" || received.ParseMode != "HTML" {
		t.Errorf("request = %+v", received)
	}
	if !received.LinkPreviewOptions.IsDisabled {
		t.Error("link preview is enabled")
	}
}

func TestTelegramClientRejectsHTTPAndAPIError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{name: "HTTP error", statusCode: http.StatusTooManyRequests, body: `{"ok":false}`},
		{name: "API error", statusCode: http.StatusOK, body: `{"ok":false,"description":"rejected"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newTelegramClient(
				"https://api.telegram.test",
				"token",
				"chat",
				&http.Client{
					Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
						return telegramHTTPResponse(test.statusCode, test.body), nil
					}),
					Timeout: time.Second,
				},
			)
			if err := client.SendMessage(context.Background(), "message"); err == nil {
				t.Fatal("SendMessage() error = nil")
			}
		})
	}
}

func TestTelegramTransportErrorDoesNotLeakToken(t *testing.T) {
	const token = "123456:super-secret-token"
	client := newTelegramClient(
		"https://api.telegram.invalid",
		token,
		"chat",
		&http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("dial failed for token " + token)
			}),
			Timeout: time.Second,
		},
	)

	err := client.SendMessage(context.Background(), "message")
	if !errors.Is(err, errTelegramTransport) {
		t.Fatalf("SendMessage() error = %v, want transport category", err)
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("transport error leaks token: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func telegramHTTPResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
