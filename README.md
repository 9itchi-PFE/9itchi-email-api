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
| POST | `/applications` | user | Apply (201) |
| GET | `/applications` | user | ListApplications |
| PATCH | `/applications/:id` | user | UpdateApplication |
| POST | `/applications/:id/send` | user | SendApplicationNow |

## Applications

`POST /applications` takes `job_id`, optional `resume_id`, and:

- `via: "gmail"` (default) with `subject` and `body`: the email leaves from the student's Gmail with the CV attached, now or at `scheduled_at`. The recipient is the address the offer takes applications at, read from job-api (`GET /internal/jobs/:id`); the browser never sees it. `to` is only used when the offer has none. `422` when there is no address at all.
- `via: "self"`: nothing is sent; the application is only recorded (the student applied on another site or from their own mailbox).

The CV is the one resume-api renders at that moment (`GET /internal/resumes/:id/pdf`); when it cannot be attached the email is not sent (`502`, a scheduled one becomes `failed`).

One live application per offer: `409` when one is already scheduled, sent or answered, and when the offer is closed. `409` also when Gmail is not connected, `402` when the plan's monthly email limit is reached.

Each application keeps the `job_title` and `company` of its offer (offers are removed once expired) and its channel `via`. Its `status` is `scheduled`, `sent`, `opened`, `replied`, `withdrawn`, `closed`, or `failed` when a scheduled email could not leave.

- `PATCH /applications/:id` `{"status": "replied" | "withdrawn" | "closed" | "sent"}`: what the student says happened. Withdrawing a scheduled application cancels its email; any other change waits until it is sent (`409`).
- `POST /applications/:id/send`: sends a scheduled application now and drops its schedule task.

## Run

```
cp .env.example .env
go run main.go
```
