package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestFormatReminder(t *testing.T) {
	location := mustLocation(t, "Europe/Moscow")
	payload := reminderPayload{
		Task: &task{
			ID:        42,
			Title:     `<Prepare & "report">`,
			ProjectID: 7,
			DueDate:   mustTime(t, "2026-08-26T15:00:00Z"),
		},
		Project: &project{ID: 7, Title: "Work & Home"},
	}

	message := formatReminder(payload, "https://vikunja.example.com", location)
	wants := []string{
		"<b>Напоминание</b>",
		`<a href="https://vikunja.example.com/tasks/42">&lt;Prepare &amp; &#34;report&#34;&gt;</a>`,
		"Проект: Work &amp; Home",
		"Срок: 26 августа 2026, 18:00",
	}
	for _, want := range wants {
		if !strings.Contains(message, want) {
			t.Errorf("message does not contain %q:\n%s", want, message)
		}
	}
	if strings.Contains(message, `<Prepare`) {
		t.Errorf("message contains unescaped title: %s", message)
	}
}

func TestFormatReminderOmitsMissingOptionalValues(t *testing.T) {
	payload := reminderPayload{Task: &task{ID: 1, Title: "Task"}}
	message := formatReminder(payload, "https://vikunja.example.com", mustLocation(t, "Europe/Moscow"))
	if strings.Contains(message, "Проект:") || strings.Contains(message, "Срок:") {
		t.Errorf("message contains optional lines: %s", message)
	}
}

func TestFormatOverdueSortsAndResolvesProjectsWithoutMutation(t *testing.T) {
	location := mustLocation(t, "Europe/Moscow")
	payload := overduePayload{
		Tasks: []task{
			{ID: 4, Title: "No due date", ProjectID: 2},
			{ID: 3, Title: "Later", ProjectID: 1, DueDate: mustTime(t, "2026-08-25T09:00:00Z")},
			{ID: 2, Title: "Same due higher ID", ProjectID: 2, DueDate: mustTime(t, "2026-08-24T15:00:00Z")},
			{ID: 1, Title: "Same due lower ID", ProjectID: 1, DueDate: mustTime(t, "2026-08-24T15:00:00Z")},
		},
		Projects: map[int64]project{
			1: {ID: 1, Title: "Work"},
			2: {ID: 2, Title: "Personal"},
		},
	}
	originalIDs := []int64{4, 3, 2, 1}

	messages := formatOverdue(payload, "https://vikunja.example.com", location)
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	message := messages[0]

	orderedTitles := []string{"Same due lower ID", "Same due higher ID", "Later", "No due date"}
	previous := -1
	for _, title := range orderedTitles {
		position := strings.Index(message, title)
		if position < 0 {
			t.Fatalf("message does not contain %q: %s", title, message)
		}
		if position <= previous {
			t.Fatalf("title %q is out of order: %s", title, message)
		}
		previous = position
	}
	if !strings.Contains(message, "Проект: Work") || !strings.Contains(message, "Проект: Personal") {
		t.Errorf("project lookup missing: %s", message)
	}
	for index, id := range originalIDs {
		if payload.Tasks[index].ID != id {
			t.Fatalf("payload mutated at index %d: got %d, want %d", index, payload.Tasks[index].ID, id)
		}
	}
}

func TestFormatOverdueSplitsAtTaskBoundaries(t *testing.T) {
	tasks := make([]task, 0, 120)
	for id := int64(1); id <= 120; id++ {
		tasks = append(tasks, task{
			ID:      id,
			Title:   fmt.Sprintf("Task %d %s", id, strings.Repeat("& длинное название ", 12)),
			DueDate: mustTime(t, "2026-08-24T15:00:00Z"),
		})
	}

	messages := formatOverdue(
		overduePayload{Tasks: tasks},
		"https://vikunja.example.com",
		mustLocation(t, "Europe/Moscow"),
	)
	if len(messages) < 2 {
		t.Fatalf("len(messages) = %d, want multiple messages", len(messages))
	}

	links := 0
	for _, message := range messages {
		if textLength(message) > maxTelegramTextLength {
			t.Errorf("message length = %d, limit = %d", textLength(message), maxTelegramTextLength)
		}
		if !strings.HasPrefix(message, "<b>Просроченные задачи: 120</b>") {
			t.Errorf("message is not standalone: %s", message[:min(len(message), 100)])
		}
		links += strings.Count(message, "https://vikunja.example.com/tasks/")
	}
	if links != len(tasks) {
		t.Errorf("task links = %d, want %d", links, len(tasks))
	}
}

func TestFormatOverdueTruncatesAnIndividualTask(t *testing.T) {
	payload := overduePayload{
		Tasks: []task{{
			ID:      1,
			Title:   strings.Repeat("<&>", 5000),
			DueDate: mustTime(t, "2026-08-24T15:00:00Z"),
		}},
		Projects: map[int64]project{0: {Title: strings.Repeat("Project & ", 5000)}},
	}

	messages := formatOverdue(payload, "https://vikunja.example.com", mustLocation(t, "Europe/Moscow"))
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	if textLength(messages[0]) > maxTelegramTextLength {
		t.Errorf("message length = %d", textLength(messages[0]))
	}
	if !strings.Contains(messages[0], "…</a>") {
		t.Errorf("message does not contain a safely truncated title")
	}
}

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("time.Parse(%q): %v", value, err)
	}
	return parsed
}

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("time.LoadLocation(%q): %v", name, err)
	}
	return location
}
