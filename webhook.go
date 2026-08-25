package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	maxWebhookBodySize       = 1 << 20
	maxConcurrentWebhooks    = 8
	webhookProcessingTimeout = 20 * time.Second
)

type messageSender interface {
	SendMessage(context.Context, string) error
}

type webhookHandler struct {
	secret      []byte
	vikunjaURL  string
	location    *time.Location
	sender      messageSender
	logger      *slog.Logger
	concurrency chan struct{}
}

type webhookEnvelope struct {
	EventName string          `json:"event_name"`
	Time      time.Time       `json:"time"`
	Data      json.RawMessage `json:"data"`
}

type task struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	ProjectID int64     `json:"project_id"`
	DueDate   time.Time `json:"due_date"`
}

type project struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

type reminderPayload struct {
	Task    *task    `json:"task"`
	Project *project `json:"project"`
}

type overduePayload struct {
	Tasks    []task            `json:"tasks"`
	Projects map[int64]project `json:"projects"`
}

func newWebhookHandler(cfg config, sender messageSender, logger *slog.Logger) *webhookHandler {
	return &webhookHandler{
		secret:      []byte(cfg.WebhookSecret),
		vikunjaURL:  cfg.VikunjaURL,
		location:    cfg.Location,
		sender:      sender,
		logger:      logger,
		concurrency: make(chan struct{}, maxConcurrentWebhooks),
	}
}

func routes(handler *webhookHandler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhooks/vikunja", handler.handle)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

func (h *webhookHandler) handle(w http.ResponseWriter, r *http.Request) {
	select {
	case h.concurrency <- struct{}{}:
		defer func() { <-h.concurrency }()
	default:
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), webhookProcessingTimeout)
	defer cancel()

	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodySize)
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	if !verifySignature(body, r.Header.Get("X-Vikunja-Signature"), h.secret) {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	var envelope webhookEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil || strings.TrimSpace(envelope.EventName) == "" {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	var messages []string
	switch envelope.EventName {
	case "task.reminder.fired":
		var payload reminderPayload
		if err := json.Unmarshal(envelope.Data, &payload); err != nil || payload.Task == nil || !validTask(*payload.Task) {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		messages = []string{formatReminder(payload, h.vikunjaURL, h.location)}
	case "tasks.overdue":
		var payload overduePayload
		if err := json.Unmarshal(envelope.Data, &payload); err != nil || len(payload.Tasks) == 0 {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		for _, task := range payload.Tasks {
			if !validTask(task) {
				http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}
		}
		messages = formatOverdue(payload, h.vikunjaURL, h.location)
	default:
		w.WriteHeader(http.StatusNoContent)
		return
	}

	for _, message := range messages {
		if err := h.sender.SendMessage(ctx, message); err != nil {
			h.logger.ErrorContext(ctx, "Telegram delivery failed", "event", envelope.EventName, "error", err)
			http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

func verifySignature(body []byte, signature string, secret []byte) bool {
	if signature == "" {
		return false
	}

	provided, err := hex.DecodeString(signature)
	if err != nil || len(provided) != sha256.Size {
		return false
	}

	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	return hmac.Equal(provided, mac.Sum(nil))
}

func validTask(task task) bool {
	return task.ID > 0 && strings.TrimSpace(task.Title) != ""
}
