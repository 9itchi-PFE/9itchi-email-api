# 9itchi-email-api

Outreach: Gmail OAuth2 connection, AI-drafted application emails, send now or schedule, email history and applications.

Port `8085`, health `/health`.

## Endpoints

| Method | Path | Access | Handler |
|---|---|---|---|
| GET | `/gmail/connect` | user | ConnectGmail |
| GET | `/gmail/callback` | public | GmailCallback |
| GET | `/gmail/status` | user | GmailStatus |
| DELETE | `/gmail/connection` | user | RevokeGmail |
| POST | `/emails/draft` | user | DraftEmail |
| POST | `/emails` | user | SendEmail |
| POST | `/emails/schedule` | user | ScheduleEmail |
| GET | `/emails` | user | ListEmails |
| GET | `/emails/:id` | user | GetEmail |
| DELETE | `/emails/:id` | user | CancelEmail |
| POST | `/internal/emails/:id/dispatch` | internal | DispatchScheduled |
| GET | `/admin/users/:id/applications` | admin | UserApplications (a student's applications for the team: offer, how, state, dates; no recipient, no subject) |
| GET | `/admin/users/:id/sent-with-cv` | admin | UserBuiltSends (`count`, `last_at` of the applications sent with the CV built in 9itchi, to tell whether a payment was used; the imported file and scheduled ones do not count; no recipient, no subject) |
| GET | `/admin/users/:id/gmail` | admin | UserGmailStatus (`connected`, `connected_at`; no address) |
| GET | `/internal/emails/:id/context` | internal | EmailContext (the offer an email was sent for: `application_id`, `job_id`, `job_title`, `company`; 404 when it was sent for no application) |
| POST | `/applications` | user | Apply (201) |
| GET | `/applications` | user | ListApplications |
| GET | `/applications/allowance` | user | Allowance |
| POST | `/applications/match` | user | Match (`{job_id, resume_id, language}` → `{score, level, strengths, gaps, advice, created_at, limit, used}`: how well the CV fits the offer, read by the AI) |
| GET | `/applications/match` | user | FindMatch (same fields as query parameters: the analysis already made, 404 when none; never asks the AI) |
| PATCH | `/applications/:id` | user | UpdateApplication |
| POST | `/applications/:id/send` | user | SendApplicationNow |

## Applications

`POST /applications` takes `job_id`, optional `resume_id`, and:

- `via: "gmail"` (default) with `subject` and `body`: the email leaves from the student's Gmail with the CV attached, now or at `scheduled_at`. The recipient is the address the offer takes applications at, read from job-api (`GET /internal/jobs/:id`); the browser never sees it. `to` is only used when the offer has none. `422` when there is no address at all.
- `via: "self"`: nothing is sent; the application is only recorded (the student applied on another site or from their own mailbox).

The CV is the one resume-api renders at that moment (`GET /internal/resumes/:id/pdf`); when it cannot be attached the email is not sent (`502`, a scheduled one becomes `failed`).

One live application per offer: `409` when one is already scheduled, sent or answered, and when the offer is closed. `409` also when Gmail is not connected, `402` when the plan's monthly email limit is reached.

**Without a subscription** (the plan's `emails_per_month` is `0`) a student still sends `free_applications` applications (plan quota, payments-api) over the whole life of the account. Those leave with the file the student imported (`GET /internal/resumes/:id/original` of resume-api, its own name and type), never the CV built in 9itchi; the email is marked `cv_original`. Emails sent, being sent or scheduled count; failed and cancelled ones do not. `402` once they are used up, `422` when the CV has no imported file. `GET /applications/allowance` answers `{"mode": "plan" | "original" | "none", "limit": n | null, "used": n}` so the screen can say what will leave and how many are left.

Each application keeps the `job_title` and `company` of its offer (offers are removed once expired) and its channel `via`. Its `status` is `scheduled`, `sent`, `opened`, `replied`, `withdrawn`, `closed`, or `failed` when a scheduled email could not leave.

- `PATCH /applications/:id` `{"status": "replied" | "withdrawn" | "closed" | "sent"}`: what the student says happened. Withdrawing a scheduled application cancels its email; any other change waits until it is sent (`409`).
- `POST /applications/:id/send`: sends a scheduled application now and drops its schedule task.

## Events (RabbitMQ)

Optional. With `RABBITMQ_URL` set, `email.sent` and `email.failed` (`email_id`, `user_id`, `scheduled`: true when the scheduler sent it, false when the student clicked) are published to the topic exchange `9itchi.events` (routing key = event name) as persistent JSON messages, each confirmed by the broker and carrying a message id consumers deduplicate on. Without the variable the event name is only logged. `application.first_sent` (`user_id`) is published once per student, when their first email ever has left: statistics-api counts it as the step « first application ».

A publish failure is logged and never fails the request. A broker that cannot be reached is left alone for 15 s, so it costs one short wait and not one per request; events raised meanwhile are lost (there is no outbox). Broker tests: `RABBITMQ_TEST_URL=amqp://guest:guest@localhost:5672/ go test -tags integration ./core/` (skipped without the variable).

## Run

```
cp .env.example .env
go run main.go
```

## Account deletion

`DELETE /internal/users/:id` (`x-api-key`) is called by account-api before it deletes an account. Here it concerns the applications, the emails (the scheduled ones never leave) and the Gmail connection with its tokens. It runs in one transaction, answers `204`, and can be called again without harm (account-api retries until every service has answered). Nothing about the user is logged.

## AI letters

`POST /emails/draft` keeps each letter the AI writes in `ai_drafts` (migration `0007`), under a digest of the offer and of the CV as the AI read them. Asking again for the same offer, CV, language and tone gives the last letter back without asking the AI nor counting it; `"fresh": true` (« Réécrire ») asks for a new one. An optional `goal` (« Objectif » of the outreach page, max 300 characters) goes to the AI, which writes the letter around it; it is part of the digest, so a letter written for another goal is not given back. The plan's `ai_drafts_per_month` limits the letters written in a calendar month (absent: no limit; `0`: 402 « part of the subscription »; reached: 402 « monthly limit »); the answer carries `limit` and `used`. ai-api's draft route is internal: the call sends the internal key (and still the student's token, until every ai-api has the internal route). Letters are erased with the account.

## AI analysis of an offer

`POST /applications/match` reads the offer (job-api) and the CV as text (resume-api), and asks ai-api how well they fit. The reading is kept in `job_matches` (migration `0006`) under a digest of what the AI read: asking again for the same offer and the same CV returns it without asking the AI nor counting it; a changed CV or offer is read again. The plan's `ai_matches_per_month` limits the readings made in a calendar month (absent: no limit; `0`: 402). An empty CV is a 422 and is not sent. The readings go with the account when it is deleted.

## Replies

When a student marks an application as answered by the company (`PATCH /applications/:id` with `replied`, from another state), `application.replied` (`user_id` only) is published: statistics-api counts it as `replies`. Marking it again announces nothing; a failed publish never fails the change.
