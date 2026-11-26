# 9itchi Email API
Applications from the student's own Gmail: OAuth (send-only), AI letter draft, AI CV/offer match, send now or schedule, application tracking, quotas. Port 8085, DB `emails`.

## Conventions (shared by every 9itchi Go service)
- `handlers/` thin (validate, authorize, call service, JSON) -> `services/` all logic, returns `(T, *models.ApiError)` -> `repositories/` only Postgres, plain errors (`ErrNotFound`, `ErrConflict`). Error body `{"error": "..."}`.
- DB calls use `repositories.QueryTimeout`; multi-table writes `Store.WithTx`; never select bytea in list queries.
- Schema: add `migrations/NNNN_name.sql` (goose, embedded, runs at startup). Never edit a merged migration.
- Auth: `Authorization: Bearer` AES-GCM-wrapped HS256 JWT (claims `id`, `email`, `role`, `typ`, `exp`; access only). `/admin/*` = `RequireRole("admin")`. `/internal/*` = `x-api-key` (`INTERNAL_API_KEY`), blocked by nginx.
- Events: exchange `9itchi.events` (topic, routing key = event name), names in `core/events.go`; empty `RABBITMQ_URL` = `LogPublisher`.
- Env only, validated in `utils/config.go`. Common: `PORT`, `ENVIRONMENT`, `DATABASE_URL`, `ITCHI_SECRET_KEY`, `ITCHI_ENCRYPTION_KEY`, `INTERNAL_API_KEY`, `INTERNAL_API_KEY_REQUIRED`. Never log tokens, passwords, PII.
- Tests: `{resource}_test.go`, testify, func-field fakes (no testify/mock), success + error paths. Repo tests: `//go:build integration`, embedded Postgres.
- Check: `gofmt -l .; go build ./... && go vet ./... && go test ./...`; `go test -tags integration -count=1 ./repositories/...`; `golangci-lint run`.

## Routes
- Public: `GET /gmail/callback` (state = signed `gmail_state`, 10 min TTL).
- User: `GET /gmail/connect|status`, `DELETE /gmail/connection`; `POST /emails/draft`, `POST /emails`, `POST /emails/schedule`, `GET /emails`, `/emails/:id` GET|DELETE (cancel); `/applications` POST|GET, `GET /applications/allowance`, `/applications/match` POST|GET, `PATCH /applications/:id`, `POST /applications/:id/send`.
- Admin: `GET /admin/users/:id/applications|gmail|sent-with-cv`.
- Internal: `DELETE /internal/users/:id`, `POST /internal/emails/:id/dispatch` (schedule-api), `GET /internal/emails/:id/context` (notification-api).

## Data, peers, events
- Tables: `gmail_connections` (tokens AES-GCM, `utils/crypto.go`), `emails`, `applications`, `job_matches`, `ai_drafts`. No bytea: CV fetched from resume-api at send time. Latest `0008_match_keywords.sql`.
- Peers (`pkg/`, shared `pkg/httpx`): ai (`/ai/emails/draft`, `/ai/jobs/match`), resume (`/resumes/:id`, `/internal/resumes/:id/pdf|original`), job (`/jobs/:id`, `/internal/jobs/:id`), tracker (`/internal/tracking`), schedule (`/internal/schedules`), payments (`/internal/quotas/:userId`), Google (`pkg/google`, `core.Mailer`).
- Publishes `email.sent`, `email.failed` (with `scheduled` flag), `application.first_sent`, `application.replied`. Consumes nothing.
- Env: `RABBITMQ_URL`, `AI_API_URL`, `RESUME_API_URL`, `JOB_API_URL`, `TRACKER_API_URL`, `SCHEDULE_API_URL`, `PAYMENTS_API_URL`, `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `GMAIL_REDIRECT_URL`, `CLIENT_URL`, `TRACKER_PUBLIC_URL`.

## Domain rules
- Gmail scopes `gmail.send openid email` only; revocable; disabled unless both Google vars set. Revoked Gmail = 409 "reconnect gmail".
- Quotas from payments keys `emails_per_month`, `free_applications`, `ai_matches_per_month`, `ai_drafts_per_month`. Allowance modes: `plan` (9itchi CV, monthly), `original` (no subscription: `free_applications` over account life, sent with the imported file as is), `none`. Over limit = 402.
- One live application per offer: unique `(user_id, job_id)`, else 409. Recipient = job `apply_email` from job-api internal, else request `to`.
- Schedule window now+1 min to now+365 d; delivery via schedule-api -> `/internal/emails/:id/dispatch`; non-scheduled status = 409 no-op.
- Tracker down = send without pixel. Application flagged "CV attached" fails 502 if CV can't be attached.
- AI draft/match cached, not re-counted: unique `(user_id, job_id, resume_id, language, input_hash)`.
- Match carries `keywords {matched, missing}` (offer order, computed by ai-api) and `keyword_coverage` 0-100, stored in `job_matches` (`0008`); rows made before read as empty lists and 0.
- Real Gmail send still untested with real Google credentials.
