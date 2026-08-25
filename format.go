package main

import (
	"cmp"
	"fmt"
	"html"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const maxTelegramTextLength = 4096

var russianMonths = [...]string{
	"",
	"января",
	"февраля",
	"марта",
	"апреля",
	"мая",
	"июня",
	"июля",
	"августа",
	"сентября",
	"октября",
	"ноября",
	"декабря",
}

func formatReminder(payload reminderPayload, vikunjaURL string, location *time.Location) string {
	projectTitle := ""
	if payload.Project != nil {
		projectTitle = payload.Project.Title
	}

	build := func(title, project string) string {
		lines := []string{
			"<b>Напоминание</b>",
			"",
			formatTaskLink(vikunjaURL, payload.Task.ID, title),
		}
		if project != "" {
			lines = append(lines, "Проект: "+project)
		}
		if due := formatRussianDate(payload.Task.DueDate, location); due != "" {
			lines = append(lines, "Срок: "+due)
		}
		return strings.Join(lines, "\n")
	}

	title := html.EscapeString(payload.Task.Title)
	project := html.EscapeString(projectTitle)
	message := build(title, project)
	if textLength(message) <= maxTelegramTextLength {
		return message
	}

	withoutProject := build(title, "")
	if projectTitle != "" && textLength(withoutProject) < maxTelegramTextLength {
		available := maxTelegramTextLength - textLength(withoutProject) - textLength("\nПроект: ")
		project = truncateEscaped(projectTitle, available)
		message = build(title, project)
		if textLength(message) <= maxTelegramTextLength {
			return message
		}
	}

	emptyTitle := build("", "")
	title = truncateEscaped(payload.Task.Title, maxTelegramTextLength-textLength(emptyTitle))
	return build(title, "")
}

func formatOverdue(payload overduePayload, vikunjaURL string, location *time.Location) []string {
	tasks := slices.Clone(payload.Tasks)
	slices.SortFunc(tasks, compareTasks)

	header := fmt.Sprintf("<b>Просроченные задачи: %d</b>", len(tasks))
	messages := make([]string, 0, 1)
	current := header
	maxBlockLength := maxTelegramTextLength - textLength(header) - textLength("\n\n")

	for index, task := range tasks {
		projectTitle := ""
		if project, ok := payload.Projects[task.ProjectID]; ok {
			projectTitle = project.Title
		}

		block := formatOverdueTask(index+1, task, projectTitle, vikunjaURL, location, maxBlockLength)
		candidate := current + "\n\n" + block
		if textLength(candidate) <= maxTelegramTextLength {
			current = candidate
			continue
		}

		messages = append(messages, current)
		current = header + "\n\n" + block
	}

	return append(messages, current)
}

func compareTasks(a, b task) int {
	aHasDueDate := !a.DueDate.IsZero()
	bHasDueDate := !b.DueDate.IsZero()
	switch {
	case aHasDueDate && !bHasDueDate:
		return -1
	case !aHasDueDate && bHasDueDate:
		return 1
	case aHasDueDate && bHasDueDate:
		if order := a.DueDate.Compare(b.DueDate); order != 0 {
			return order
		}
	}
	return cmp.Compare(a.ID, b.ID)
}

func formatOverdueTask(index int, task task, projectTitle, vikunjaURL string, location *time.Location, maxLength int) string {
	build := func(title, project string) string {
		lines := []string{
			strconv.Itoa(index) + ". " + formatTaskLink(vikunjaURL, task.ID, title),
		}
		if project != "" {
			lines = append(lines, "   Проект: "+project)
		}
		if due := formatRussianDate(task.DueDate, location); due != "" {
			lines = append(lines, "   Срок: "+due)
		}
		return strings.Join(lines, "\n")
	}

	title := html.EscapeString(task.Title)
	project := html.EscapeString(projectTitle)
	block := build(title, project)
	if textLength(block) <= maxLength {
		return block
	}

	withoutProject := build(title, "")
	if projectTitle != "" && textLength(withoutProject) < maxLength {
		available := maxLength - textLength(withoutProject) - textLength("\n   Проект: ")
		project = truncateEscaped(projectTitle, available)
		block = build(title, project)
		if textLength(block) <= maxLength {
			return block
		}
	}

	emptyTitle := build("", "")
	title = truncateEscaped(task.Title, maxLength-textLength(emptyTitle))
	return build(title, "")
}

func formatTaskLink(vikunjaURL string, taskID int64, escapedTitle string) string {
	taskURL := vikunjaURL + "/tasks/" + strconv.FormatInt(taskID, 10)
	return `<a href="` + html.EscapeString(taskURL) + `">` + escapedTitle + `</a>`
}

func formatRussianDate(value time.Time, location *time.Location) string {
	if value.IsZero() {
		return ""
	}

	local := value.In(location)
	return fmt.Sprintf(
		"%d %s %d, %02d:%02d",
		local.Day(),
		russianMonths[local.Month()],
		local.Year(),
		local.Hour(),
		local.Minute(),
	)
}

func truncateEscaped(value string, maxLength int) string {
	escaped := html.EscapeString(value)
	if textLength(escaped) <= maxLength {
		return escaped
	}
	if maxLength <= 0 {
		return ""
	}

	runes := []rune(value)
	low, high := 0, len(runes)
	for low < high {
		middle := (low + high + 1) / 2
		candidate := html.EscapeString(string(runes[:middle])) + "…"
		if textLength(candidate) <= maxLength {
			low = middle
		} else {
			high = middle - 1
		}
	}

	if low == 0 {
		return "…"
	}
	return html.EscapeString(string(runes[:low])) + "…"
}

func textLength(value string) int {
	return utf8.RuneCountInString(value)
}
