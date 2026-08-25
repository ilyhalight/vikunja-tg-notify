# Vikunja Telegram Notification Server

## Goal

Build a small Go webhook server that receives selected user webhook events from Vikunja and synchronously sends notifications to one Telegram chat.

Keep the implementation minimal. Use the Go standard library only. Do not add a database, persistent queue, message broker, or web framework.

## Supported Events

Handle only these Vikunja events:

- `task.reminder.fired`: send one reminder message for a task.
- `tasks.overdue`: send one summary containing all overdue tasks in the event.

Do not subscribe to or handle `task.overdue`. Vikunja emits it once per overdue task alongside `tasks.overdue`, which would duplicate notifications.

## Architecture

Use Go 1.26 and the standard library:

- `net/http` for the HTTP server and Telegram client.
- `encoding/json` for payloads.
- `crypto/hmac` and `crypto/sha256` for Vikunja signature verification.
- `html` for escaping user-provided values in Telegram HTML messages.
- `log/slog` for structured logs.
- `time` and `time/tzdata` for timezone-aware date formatting with an embedded timezone database.
- `slices` for cloning and sorting overdue tasks without mutating decoded payloads.

Use the stable `encoding/json` package. Do not enable or use the experimental `encoding/json/v2` implementation. Do not add a router, Telegram Bot SDK, logging library, assertion library, or `golang.org/x/sync`; the standard library covers the required functionality.

Process deliveries synchronously:

1. Receive `POST /webhooks/vikunja`.
2. Acquire a slot from a bounded webhook concurrency gate.
3. Read the raw request body with a 1 MiB limit.
4. Verify `X-Vikunja-Signature` before parsing JSON.
5. Parse and validate the event.
6. Format one or more Telegram messages.
7. Call Telegram Bot API `sendMessage`.
8. Return success only after Telegram accepts every message.

Allow at most 8 webhook requests to be processed concurrently. Implement the fixed-size semaphore with a buffered `chan struct{}` and a non-blocking acquire at the start of the webhook handler. Reject requests above the limit immediately with `503 Service Unavailable`; do not let them wait and accumulate goroutines. Always release the slot when the handler returns. The health endpoint must not use this semaphore.

Vikunja does not retry failed webhook deliveries. This limitation is accepted for this minimal implementation.

## Configuration

Read configuration from environment variables and validate it at startup:

```env
HTTP_ADDR=:8080
VIKUNJA_URL=https://vikunja.example.com
VIKUNJA_WEBHOOK_SECRET=replace-me
TELEGRAM_BOT_TOKEN=replace-me
TELEGRAM_CHAT_ID=-1001234567890
TZ=Europe/Moscow
```

Requirements:

- `VIKUNJA_URL`, `VIKUNJA_WEBHOOK_SECRET`, `TELEGRAM_BOT_TOKEN`, and `TELEGRAM_CHAT_ID` are required.
- `HTTP_ADDR` defaults to `:8080`.
- `TZ` defaults to `Europe/Moscow`.
- Normalize `VIKUNJA_URL` by removing trailing slashes.
- Treat `TELEGRAM_CHAT_ID` as a string so numeric IDs and Telegram-supported usernames both work.
- Never log secrets or complete webhook payloads.

## Vikunja Payloads

All requests use this envelope:

```json
{
  "event_name": "task.reminder.fired",
  "time": "2026-08-25T12:00:00Z",
  "data": {}
}
```

Decode `data` through `json.RawMessage`, then unmarshal the event-specific payload.

`task.reminder.fired` data contains:

```json
{
  "task": {
    "id": 1,
    "title": "Prepare report",
    "project_id": 2,
    "due_date": "2026-08-26T15:00:00Z"
  },
  "user": {
    "name": "User",
    "username": "user"
  },
  "project": {
    "id": 2,
    "title": "Work"
  },
  "reminder": {
    "reminder": "2026-08-25T12:00:00Z",
    "relative_period": 0,
    "relative_to": "due_date"
  }
}
```

`tasks.overdue` data contains:

```json
{
  "tasks": [
    {
      "id": 1,
      "title": "Prepare report",
      "project_id": 2,
      "due_date": "2026-08-24T15:00:00Z"
    }
  ],
  "user": {
    "name": "User",
    "username": "user"
  },
  "projects": {
    "2": {
      "id": 2,
      "title": "Work"
    }
  }
}
```

Define only the fields needed for formatting. Do not copy the complete Vikunja models into this project.

## Signature Verification

Vikunja sends a lowercase hexadecimal HMAC-SHA256 digest in `X-Vikunja-Signature`.

Verify it as follows:

1. Read the exact raw body bytes.
2. Hex-decode the header value.
3. Compute HMAC-SHA256 over the raw body using `VIKUNJA_WEBHOOK_SECRET`.
4. Compare the byte slices with `hmac.Equal`.
5. Reject missing, malformed, or mismatched signatures with `401 Unauthorized`.

Do not compute the signature from re-marshaled JSON.

## Telegram Messages

Send messages through:

```text
POST https://api.telegram.org/bot<TOKEN>/sendMessage
```

Use a JSON request with:

- `chat_id` set from `TELEGRAM_CHAT_ID`.
- `text` containing the formatted notification.
- `parse_mode` set to `HTML`.
- Link preview disabled.

Check both the Telegram HTTP status and the response body's `ok` field. Treat any non-success response as an error. Apply a finite HTTP client timeout.

The bot token is part of the Telegram request URL. Network errors returned by `http.Client.Do` may therefore contain the complete URL and token. Never log a raw `url.Error`, the complete Telegram request URL, or an error string that may contain either. Return or log a sanitized error category and a token-free underlying cause. Tests must verify that Telegram transport errors do not expose the bot token.

Escape all task titles, project titles, and other payload-derived text before inserting them into HTML. Build task links as:

```text
<VIKUNJA_URL>/tasks/<numeric-task-id>
```

The current Vikunja frontend route is `/tasks/:id`.

Format reminder messages in Russian with this general structure:

```text
Напоминание

Prepare report
Проект: Work
Срок: 26 августа 2026, 18:00
```

Make the task title a link to Vikunja. Omit optional lines when their source values are absent.

Format overdue summaries in Russian with this general structure:

```text
Просроченные задачи: 2

1. Prepare report
   Проект: Work
   Срок: 24 августа 2026, 18:00

2. Pay invoice
   Проект: Personal
   Срок: 25 августа 2026, 12:00
```

Resolve each task's project through `task.project_id` and the `projects` map. Make every task title a Vikunja link.

Do not rely on the order of tasks in the payload. Before formatting an overdue summary, clone the task slice with `slices.Clone` and sort the copy with `slices.SortFunc` by due date in ascending order and then by numeric task ID. Place tasks without a due date after tasks with a valid due date, ordered by task ID. Do not mutate the decoded payload slice.

Format dates in the configured `TZ`, using Russian month names or an equivalent clear Russian date representation. Handle missing and zero dates without displaying misleading values.

Telegram text messages are limited to 4096 characters. Split large overdue summaries at task boundaries into valid standalone messages. Ensure every resulting message remains within Telegram's limit, including HTML markup. Truncate an individual task title safely if it cannot fit by itself.

## HTTP Contract

Expose these endpoints:

- `POST /webhooks/vikunja`: receive Vikunja webhooks.
- `GET /healthz`: return `200 OK` when the process is running.

Use these response statuses:

- `204 No Content`: Telegram accepted all generated messages.
- `204 No Content`: the request is valid but the event is unsupported.
- `400 Bad Request`: malformed JSON or an invalid supported-event payload.
- `401 Unauthorized`: missing or invalid Vikunja signature.
- `405 Method Not Allowed`: wrong method for a known endpoint.
- `413 Request Entity Too Large`: body exceeds 1 MiB.
- `502 Bad Gateway`: Telegram request failed or Telegram rejected the message.
- `503 Service Unavailable`: the webhook concurrency limit has been reached.

Do not expose internal errors, tokens, or Telegram response bodies to webhook callers. Log concise diagnostic errors instead.

Configure server read, header, write, and idle timeouts. Propagate request contexts to Telegram calls. Implement graceful shutdown on `SIGINT` and `SIGTERM`.

## Project Files

Keep the project small and easy to navigate. A suitable layout is:

```text
go.mod
main.go
config.go
webhook.go
telegram.go
format.go
webhook_test.go
telegram_test.go
format_test.go
Dockerfile
.dockerignore
compose.yaml
.env.example
README.md
```

Use package `main` throughout unless the code grows enough to justify internal packages. Avoid unnecessary interfaces and abstractions. Introduce narrow interfaces only where they materially simplify tests.

## Tests

Use only the standard `testing` and `net/http/httptest` packages. Cover at least:

- Startup configuration validation.
- A valid HMAC signature.
- Missing, malformed, and invalid signatures.
- Oversized request bodies.
- Malformed envelope JSON.
- Reminder payload formatting.
- Overdue summary formatting and project lookup.
- Russian Moscow-time date formatting.
- HTML escaping of task and project titles.
- Unsupported events being ignored.
- Telegram HTTP errors.
- Telegram responses with `ok: false`.
- Telegram transport errors not leaking the bot token.
- Deterministic overdue task ordering by due date and task ID.
- Splitting summaries longer than 4096 characters.
- Rejection with `503` when 8 webhook requests are already being processed.
- Handler behavior with an injected test sender, without starting mock HTTP servers.

Before considering the implementation complete, run:

```sh
go test ./...
go vet ./...
docker build .
```

## Deployment Documentation

Provide a multi-stage Dockerfile and a Compose example. The runtime image must contain CA certificates because the server calls Telegram over HTTPS. Import `_ "time/tzdata"` in the main package so every build contains the IANA timezone database and `Europe/Moscow` does not depend on runtime image contents. Run as a non-root user when practical.

Document how to create a Vikunja user webhook in account settings:

- Target URL: `https://<public-host>/webhooks/vikunja`.
- Events: `task.reminder.fired` and `tasks.overdue`.
- Secret: the same value as `VIKUNJA_WEBHOOK_SECRET`.

Also document how to obtain a Telegram bot token, add the bot to the destination chat, and determine `TELEGRAM_CHAT_ID`.
