# 9itchi Email API

Outreach: Gmail OAuth2 connection, AI-drafted application emails, send now or schedule, email history and applications.

## Stack
Go, Gin, PostgreSQL (db `emails`, pgx + goose migrations), Viper config, Zap logger. Peers over HTTP (`pkg/`): ai, resume, job, tracker, schedule, payments. Events over RabbitMQ (names in `core/events.go`).

## Layout
`main.go`, `app/` (init order: config, repositories, services, router), `router/` (routes + middleware), `handlers/` (thin) -> `services/` (logic) -> `repositories/` (Postgres only), `core/` (interfaces/factories), `models/{api,db,mapping}`, `pkg/` (peer clients), `utils/` (config, logger).

Entities: `GmailConnection`, `Email`, `Application`.

## Rules
- Every endpoint: validate inputs -> authorize caller -> execute via service -> respond JSON. No business logic in handlers.
- Services return `(T, *models.ApiError)`; repos return plain errors; services wrap with a status code. Error body is always `{"error": "message"}`.
- Users are referenced by their UUID `id` (JWT claim `id`). JWT claims: `id`, `email`, `role`, `typ`, `exp`. Header `Authorization: Bearer <AES-GCM encrypted HS256 JWT>`.
- Every DB call uses a context with a deadline (`repositories.QueryTimeout`). Schema and indexes live in `migrations/*.sql` (goose, embedded, run at startup). Multi-table writes use `Store.WithTx`. Never select blob columns in list queries.
- Nil-check optional nested fields. Never log passwords, tokens, Gmail tokens or PII. Secrets and URLs only from env.
- `AuthMiddleware` validates the encrypted JWT (access tokens only); internal routes need `x-api-key`.
- Tests: `{resource}_test.go`, testify, mock dependencies, cover success and error paths. Write the test with the implementation.

## Commands
`go build ./... && go test ./...`, `golangci-lint run`, `go run main.go` (port 8085), `docker build -t 9itchi-email-api .`.
