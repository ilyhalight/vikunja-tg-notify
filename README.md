# Vikunja Telegram Notify

Небольшой self-hosted сервис, который отправляет напоминания и сводки просроченных задач из Vikunja в Telegram-чат.

Поддерживаются только `task.reminder.fired` и `tasks.overdue`.

Пример уведомления:

```text
Просроченные задачи: 2

1. Подготовить отчёт
   Проект: Работа
   Срок: 24 августа 2026, 18:00

2. Оплатить счёт
   Проект: Личное
   Срок: 25 августа 2026, 12:00
```

## Требования

- Docker с Docker Compose для рекомендуемого запуска либо Go 1.26 для локального запуска
- Работающий экземпляр Vikunja и пользователь с доступом к настройкам webhook
- Публичный HTTPS-адрес сервиса, доступный из Vikunja

## Быстрый старт

1. Получите `TELEGRAM_BOT_TOKEN` и `TELEGRAM_CHAT_ID`, как описано в разделе [Настройка Telegram](#настройка-telegram)
2. Создайте `.env` на основе `.env.example` и заполните значения
3. Запустите сервис:

   ```sh
   docker compose up -d --build
   ```

4. Проверьте состояние:

   ```sh
   curl -fsS -o /dev/null -w '%{http_code}\n' http://localhost:8080/healthz
   ```

   Ожидаемый результат:

   ```text
   200
   ```

5. Создайте пользовательский webhook Vikunja по инструкции в разделе [Настройка Vikunja](#настройка-vikunja)

## Конфигурация

| Переменная               | Обязательная | По умолчанию    | Описание                                            |
| ------------------------ | ------------ | --------------- | --------------------------------------------------- |
| `HTTP_ADDR`              | нет          | `:8080`         | Адрес HTTP-сервера                                  |
| `VIKUNJA_URL`            | да           | —               | Абсолютный HTTP(S) URL Vikunja для ссылок на задачи |
| `VIKUNJA_WEBHOOK_SECRET` | да           | —               | Секрет HMAC-подписи webhook                         |
| `TELEGRAM_BOT_TOKEN`     | да           | —               | Токен от BotFather                                  |
| `TELEGRAM_CHAT_ID`       | да           | —               | Числовой ID чата или поддерживаемое Telegram имя    |
| `TZ`                     | нет          | `Europe/Moscow` | IANA-часовой пояс для дат                           |

`VIKUNJA_URL` не должен содержать credentials, query или fragment. Завершающие `/` удаляются автоматически.

Compose-файл публикует и проверяет порт `8080`. При изменении порта в `HTTP_ADDR` синхронно обновите `ports` и `healthcheck` в `compose.yaml`.

## Настройка Telegram

1. Откройте [@BotFather](https://t.me/BotFather), выполните `/newbot` и сохраните выданный токен
2. Добавьте бота в нужную группу или канал; для канала предоставьте ему право отправлять сообщения. Для личного чата сначала отправьте боту любое сообщение
3. Вызовите `https://api.telegram.org/bot<TOKEN>/getUpdates` и найдите `message.chat.id` или `channel_post.chat.id`
4. Запишите найденное значение в `TELEGRAM_CHAT_ID` (ID групп и каналов обычно отрицательный)

## Настройка Vikunja

Создайте пользовательский webhook в настройках аккаунта Vikunja:

- Target URL: `https://<public-host>/webhooks/vikunja`
- Events: `task.reminder.fired` и `tasks.overdue`
- Secret: то же значение, что и `VIKUNJA_WEBHOOK_SECRET`

Используйте именно пользовательский webhook: эти события направлены конкретному пользователю. Не выбирайте `task.overdue`, иначе уведомления будут дублироваться.

## Локальный запуск

Приложение читает системное окружение и самостоятельно не загружает `.env`.

Linux и macOS:

```sh
set -a
. ./.env
set +a
go run .
```

PowerShell:

```powershell
Get-Content .env | Where-Object { $_ -match '^[^#].*=' } | ForEach-Object {
    $name, $value = $_ -split '=', 2
    Set-Item -Path "Env:$name" -Value $value
}
go run .
```

## HTTP API

| Статус                         | Когда возвращается                                          |
| ------------------------------ | ----------------------------------------------------------- |
| `200 OK`                       | `GET /healthz`                                              |
| `204 No Content`               | Telegram принял все сообщения или событие не поддерживается |
| `400 Bad Request`              | Некорректный JSON или payload поддерживаемого события       |
| `401 Unauthorized`             | Отсутствует или не совпадает подпись Vikunja                |
| `405 Method Not Allowed`       | Неверный HTTP-метод для известного endpoint                 |
| `413 Request Entity Too Large` | Тело запроса превышает 1 МиБ                                |
| `502 Bad Gateway`              | Telegram недоступен или отклонил сообщение                  |
| `503 Service Unavailable`      | Уже обрабатываются восемь webhook-запросов                  |

## Ограничения

- Все доставки выполняются синхронно и только в один Telegram-чат
- Базы данных, постоянной очереди и повторных попыток нет
- Vikunja не повторяет неуспешные webhook-доставки; если Telegram недоступен во время запроса, уведомление будет потеряно
- Сервис принимает не более восьми webhook-запросов одновременно

## Разработка

```sh
go test ./...
go vet ./...
docker build .
```
