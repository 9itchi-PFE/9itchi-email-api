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
| GET | `/admin/users/:id/gmail` | admin | UserGmailStatus (`connected`, `connected_at`; no address) |
| GET | `/internal/emails/:id/context` | internal | EmailContext (the offer an email was sent for: `application_id`, `job_id`, `job_title`, `company`; 404 when it was sent for no application) |
| POST | `/applications` | user | Apply (201) |
| GET | `/applications` | user | ListApplications |
| GET | `/applications/allowance` | user | Allowance |
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
