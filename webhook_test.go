package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testWebhookSecret = "test-webhook-secret"

func TestWebhookValidReminder(t *testing.T) {
	sender := &recordingSender{}
	handler := newTestWebhookHandler(t, sender)
	body := []byte(`{
		"event_name":"task.reminder.fired",
		"time":"2026-08-25T12:00:00Z",
		"data":{
			"task":{"id":1,"title":"Prepare report","project_id":2,"due_date":"2026-08-26T15:00:00Z"},
			"project":{"id":2,"title":"Work"}
		}
	}`)

	response := executeWebhook(handler, body, signBody(body, testWebhookSecret))
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
	}
	messages := sender.Messages()
	if len(messages) != 1 || !strings.Contains(messages[0], "Prepare report") {
		t.Fatalf("messages = %v", messages)
	}
}

func TestWebhookRejectsInvalidSignatures(t *testing.T) {
	body := []byte(`{"event_name":"unsupported","data":{}}`)
	tests := []struct {
		name      string
		signature string
	}{
		{name: "missing"},
		{name: "malformed", signature: "not-hex"},
		{name: "wrong length", signature: "00"},
		{name: "mismatch", signature: strings.Repeat("00", sha256.Size)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := executeWebhook(newTestWebhookHandler(t, &recordingSender{}), body, test.signature)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", response.Code)
			}
		})
	}
}

func TestWebhookRejectsOversizedBody(t *testing.T) {
	body := bytes.Repeat([]byte("x"), maxWebhookBodySize+1)
	response := executeWebhook(newTestWebhookHandler(t, &recordingSender{}), body, "")
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", response.Code)
	}
}

func TestWebhookRejectsMalformedEnvelope(t *testing.T) {
	body := []byte(`{"event_name":`)
	response := executeWebhook(
		newTestWebhookHandler(t, &recordingSender{}),
		body,
		signBody(body, testWebhookSecret),
	)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}

func TestWebhookRejectsInvalidSupportedPayload(t *testing.T) {
	body := []byte(`{"event_name":"task.reminder.fired","data":{"task":{"id":0,"title":""}}}`)
	response := executeWebhook(
		newTestWebhookHandler(t, &recordingSender{}),
		body,
		signBody(body, testWebhookSecret),
	)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}

func TestWebhookIgnoresUnsupportedEvent(t *testing.T) {
	sender := &recordingSender{}
	body := []byte(`{"event_name":"task.created","data":{"any":"value"}}`)
	response := executeWebhook(newTestWebhookHandler(t, sender), body, signBody(body, testWebhookSecret))
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", response.Code)
	}
	if len(sender.Messages()) != 0 {
		t.Fatalf("messages = %v, want none", sender.Messages())
	}
}

func TestRoutesMethodsAndHealth(t *testing.T) {
	handler := routes(newTestWebhookHandler(t, &recordingSender{}))

	healthRequest := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthResponse := httptest.NewRecorder()
	handler.ServeHTTP(healthResponse, healthRequest)
	if healthResponse.Code != http.StatusOK {
		t.Errorf("health status = %d, want 200", healthResponse.Code)
	}

	wrongMethod := httptest.NewRequest(http.MethodGet, "/webhooks/vikunja", nil)
	wrongMethodResponse := httptest.NewRecorder()
	handler.ServeHTTP(wrongMethodResponse, wrongMethod)
	if wrongMethodResponse.Code != http.StatusMethodNotAllowed {
		t.Errorf("wrong method status = %d, want 405", wrongMethodResponse.Code)
	}
}

func TestWebhookReturnsBadGatewayWhenSenderFails(t *testing.T) {
	sender := &recordingSender{err: errors.New("safe Telegram failure")}
	body := validReminderBody()
	response := executeWebhook(newTestWebhookHandler(t, sender), body, signBody(body, testWebhookSecret))
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", response.Code)
	}
}

func TestWebhookConcurrencyLimit(t *testing.T) {
	sender := &blockingSender{
		started: make(chan struct{}, maxConcurrentWebhooks),
		release: make(chan struct{}),
	}
	handler := newTestWebhookHandler(t, sender)
	body := validReminderBody()
	signature := signBody(body, testWebhookSecret)

	var wait sync.WaitGroup
	wait.Add(maxConcurrentWebhooks)
	for range maxConcurrentWebhooks {
		go func() {
			defer wait.Done()
			response := executeWebhook(handler, body, signature)
			if response.Code != http.StatusNoContent {
				t.Errorf("blocked request status = %d, want 204", response.Code)
			}
		}()
	}

	for range maxConcurrentWebhooks {
		select {
		case <-sender.started:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for blocked requests")
		}
	}

	overflow := executeWebhook(handler, body, signature)
	if overflow.Code != http.StatusServiceUnavailable {
		t.Fatalf("overflow status = %d, want 503", overflow.Code)
	}
	close(sender.release)
	wait.Wait()
}

func newTestWebhookHandler(t *testing.T, sender messageSender) *webhookHandler {
	t.Helper()
	return newWebhookHandler(
		config{
			VikunjaURL:    "https://vikunja.example.com",
			WebhookSecret: testWebhookSecret,
			Location:      mustLocation(t, "Europe/Moscow"),
		},
		sender,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
}

func executeWebhook(handler *webhookHandler, body []byte, signature string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/webhooks/vikunja", bytes.NewReader(body))
	if signature != "" {
		request.Header.Set("X-Vikunja-Signature", signature)
	}
	response := httptest.NewRecorder()
	handler.handle(response, request)
	return response
}

func signBody(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func validReminderBody() []byte {
	return []byte(`{
		"event_name":"task.reminder.fired",
		"time":"2026-08-25T12:00:00Z",
		"data":{
			"task":{"id":1,"title":"Prepare report","project_id":2,"due_date":"2026-08-26T15:00:00Z"},
			"project":{"id":2,"title":"Work"}
		}
	}`)
}

type recordingSender struct {
	mu       sync.Mutex
	messages []string
	err      error
}

func (s *recordingSender) SendMessage(_ context.Context, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, message)
	return s.err
}

func (s *recordingSender) Messages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.messages...)
}

type blockingSender struct {
	started chan struct{}
	release chan struct{}
}

func (s *blockingSender) SendMessage(ctx context.Context, _ string) error {
	s.started <- struct{}{}
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
