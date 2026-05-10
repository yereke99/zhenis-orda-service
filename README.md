# ZHENIS ORDA INSIDE / Genius Orda

Production-oriented Go MVP for a Telegram Bot, Telegram Mini App, and browser admin panel.

## Stack

- Go 1.26.x
- Telegram Bot long polling
- Telegram Mini App with backend `initData` validation
- SQLite with WAL, foreign keys and additive schema
- Redis for sessions, bot states, reminders and short-lived keys
- zap logger
- Static frontend: `static/index.html`, `static/css/style.css`, `static/js/app.js`
- Docker / Docker Compose

## Architecture

- `cmd/main.go` wires dependencies only.
- `config/` reads and validates environment variables.
- `internal/handler/` owns HTTP, Telegram, auth middleware and schedulers.
- `internal/repository/` owns SQL, transactions, statuses and business rules.
- `internal/service/` owns pure reusable domain/auth helpers.
- `internal/i18n/` stores user-facing RU/KZ bot messages.
- `traits/database/` owns SQLite connection, schema, indexes and seed data.
- `traits/logger/` owns zap logger setup.
- `static/` owns Mini App and admin UI.

## Quick Start

```bash
go mod tidy
ENV=development DISABLE_TELEGRAM_BOT=true go run ./cmd
```

Open:

- Mini App/browser fallback: `http://localhost:8080/`
- Admin: `http://localhost:8080/admin`

Development admin password is `admin` when `ADMIN_PASSWORD_HASH` is empty. In production, set `ADMIN_PASSWORD_HASH`.

## Docker

```bash
DISABLE_TELEGRAM_BOT=true docker compose up --build
```

## Required Environment

- `TOKEN` Telegram bot token
- `PORT` default `8080`
- `ENV` `development` or `production`
- `BASE_URL`
- `MINI_APP_URL`
- `DB_PATH`
- `REDIS_ADDR`
- `REDIS_PASSWORD`
- `REDIS_DB`
- `ADMIN_IDS`
- `ADMIN_PASSWORD_HASH`
- `UPLOAD_DIR`
- `ALLOWED_ORIGINS`
- `KASPI_PAY_URL`
- `KASPI_QR_IMAGE_URL`
- `HALYK_PAYMENT_URL`
- `BANK_CARD_PAYMENT_URL`
- `PAYMENT_PENDING_TTL_MINUTES`
- `SUBSCRIPTION_DEFAULT_DAYS`
- `TELEGRAM_LOG_CHAT_ID`
- `TELEGRAM_LOG_THREAD_ID`
- `DISABLE_TELEGRAM_BOT`

## MVP Flow

1. User opens bot with `/start`.
2. Bot saves Telegram user and referral payload if present.
3. User selects Kazakh or Russian.
4. Mini App opens with Telegram profile header and safe-area fullscreen layout.
5. User completes diagnostics, selects tariff, creates a pending payment.
6. User uploads receipt PDF/image to the bot.
7. Admin opens `/admin`, reviews receipt and approves/rejects payment.
8. Approval activates subscription, opens LEVEL 1, grants referral rewards when applicable, and notifies the user.
9. Lessons unlock level-by-level through watched lessons and passed tests.

## Tests

```bash
go test ./...
```

Covered critical paths:

- referral registration
- payment approval
- level unlock
- test pass/fail retry
- coin idempotency
- subscription expiration
- Mini App auth middleware
- browser admin auth
