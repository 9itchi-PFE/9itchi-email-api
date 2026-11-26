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
- User: `GET /gmail/connect|status`, `DELETE /gmail/connection`; `POST /emails/draft`, `POST /emails`, `POST /emails/schedule`, `GET /emails`, `POST /emails/draft-from-link`, `/emails/:id` GET|PATCH (edit scheduled)|DELETE (cancel); `/applications` POST|GET, `GET /applications/allowance`, `/applications/match` POST|GET, `PATCH /applications/:id`, `POST /applications/:id/send`, `POST /applications/:id/follow-up/draft`, `POST /applications/:id/follow-up`; `POST /recruiters/search`, `GET /recruiters`, `GET /recruiters/allowance`, `POST /recruiters/:id/draft`.
- Admin: `GET /admin/users/:id/applications|gmail|sent-with-cv`.
- Internal: `DELETE /internal/users/:id`, `POST /internal/emails/:id/dispatch` (schedule-api), `GET /internal/emails/:id/context` (notification-api).

## Data, peers, events
- Tables: `gmail_connections` (tokens AES-GCM, `utils/crypto.go`), `emails`, `applications`, `job_matches`, `ai_drafts` (`source_url` for link/recruiter letters, `0010`), `recruiter_searches`, `recruiters` (`0009`). No bytea: CV fetched from resume-api at send time. `applications.follow_up_email_id|follow_up_sent_at` (`0011`). Latest `0011_follow_ups.sql`.
- Peers (`pkg/`, shared `pkg/httpx`): ai (`/ai/emails/draft`, `/ai/jobs/match`), resume (`/resumes/:id`, `/internal/resumes/:id/pdf|original`), job (`/jobs/:id`, `/internal/jobs/:id`, `POST /internal/offers/read`: 422 = unreadable), tracker (`/internal/tracking`), schedule (`/internal/schedules`), payments (`/internal/quotas/:userId`), Google (`pkg/google`, `core.Mailer`), recruiter providers (`pkg/recruiters`, `core.RecruiterFinder`, factory map `Providers`; only `fake`). `pkg/httpx` maps 404 to `ErrPeerNotFound`, 422 to `ErrPeerUnprocessable`.
- Publishes `email.sent`, `email.failed` (with `scheduled` flag), `application.first_sent`, `application.replied`. Consumes nothing.
- Env: `RABBITMQ_URL`, `AI_API_URL`, `RESUME_API_URL`, `JOB_API_URL`, `TRACKER_API_URL`, `SCHEDULE_API_URL`, `PAYMENTS_API_URL`, `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `GMAIL_REDIRECT_URL`, `CLIENT_URL`, `TRACKER_PUBLIC_URL`, `RECRUITER_PROVIDER` (default `fake`; unknown name fails startup; warns in production), `RECRUITER_API_KEY` (required unless `fake`), `RECRUITER_BASE_URL`.

## Domain rules
- Gmail scopes `gmail.send openid email` only; revocable; disabled unless both Google vars set. Revoked Gmail = 409 "reconnect gmail".
- Quotas from payments keys `emails_per_month`, `free_applications`, `ai_matches_per_month`, `ai_drafts_per_month`, `recruiter_searches_per_day` (Tunis day, `time.FixedZone("Africa/Tunis", 3600)`; absent/negative = unlimited, 0 = 402). Allowance modes: `plan` (9itchi CV, monthly), `original` (no subscription: `free_applications` over account life, sent with the imported file as is), `none`. Over limit = 402.
- One live application per offer: unique `(user_id, job_id)`, else 409. Recipient = job `apply_email` from job-api internal, else request `to`.
- Schedule window now+1 min to now+365 d; delivery via schedule-api -> `/internal/emails/:id/dispatch`; non-scheduled status or `scheduled_at` later than now+1 min = no-op.
- PATCH `/emails/:id`: scheduled only (else 409); `to` locked (409) when the offer has an `apply_email`; `attach_cv` false = no CV, true needs a `resume_id`; imported-file emails re-check the file (422). New time: new schedule task first (502 = no change), then the row, then the old task is cancelled (log only).
- Tracker down = send without pixel. Application flagged "CV attached" fails 502 if CV can't be attached.
- AI draft/match cached, not re-counted. Drafts: the newest `ai_drafts` row for the same `(user_id, job_id, language, tone, input_hash)` comes back (a lookup, not a unique constraint; `fresh` writes a new row). Matches: unique `(user_id, job_id, resume_id, language, input_hash)`.
- Link drafts: URL normalized (http(s), no fragment, no `utm_*`, max 2048). A `CLIENT_URL` `/offres/<uuid>` link is the normal job draft (`source` `9itchi`). Other links: plan checked first (0 = 402; month used up = last letter for that link, else 402), then job-api reads it (never stored there, never fetched from LinkedIn; 422 = "paste its text"); kept in `ai_drafts.source_url`.
- Recruiter Radar: search needs a `resume_id` (skills, max 10), keywords, company or domain (host). Same normalized query the same Tunis day = cached, not counted. Provider asked for 20; failure = 502. People deduped by lower email, else LinkedIn, else name|company. `fake` provider: deterministic, `.example` domains only. `POST /recruiters/:id/draft` = spontaneous letter, cached/counted like link drafts under `source_url` `recruiter:<id>`; only the recruiter title and company reach the AI; the student's `goal` also goes to ai-api as its own `goal` field (the letter is written around it). Never log recruiter names or emails.
- Follow-up (« relance »): only for an application waiting for an answer (`sent`/`opened`), 7 days (`FollowUpAfter`) after its email left (or after it was recorded, `self`), one per application (409 after), reset when the student applies again. Draft: ai-api `kind: follow_up` with the first email and the days since, cached/counted under `source_url` `followup:<application id>`, no CV read; subject forced to `Re: <first subject>`. Send: `via: gmail` only (422 otherwise), to the first email's recipient (never a typed address), no CV, plan mode only (402 « part of the subscription » without one; counted in `emails_per_month`).
- Match carries `keywords {matched, missing}` (offer order, computed by ai-api) and `keyword_coverage` 0-100, stored in `job_matches` (`0008`); rows made before read as empty lists and 0.
- Real Gmail send still untested with real Google credentials.
