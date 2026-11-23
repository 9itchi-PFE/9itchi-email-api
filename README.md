# 9itchi-email-api

Outreach: Gmail OAuth2 connection, AI-drafted application emails, send now or schedule, email history and applications.

Port `8085`, health `/health`. Skeleton only: every endpoint returns `501 not implemented` until built.

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
| POST | `/applications` | user | Apply |
| GET | `/applications` | user | ListApplications |

## Run

```
cp .env.example .env
go run main.go
```
