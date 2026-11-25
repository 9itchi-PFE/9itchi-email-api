package core

import (
	"context"

	"9itchi-email-api/utils"

	"go.uber.org/zap"
)

// Event names exchanged over RabbitMQ (exchange "9itchi.events"). Never rename a published name.
const (
	EventUserRegistered      = "user.registered"
	EventEmailSent           = "email.sent"
	EventEmailFailed         = "email.failed"
	EventEmailOpened         = "email.opened"
	EventJobsMatched         = "jobs.matched"
	EventSubscriptionCreated = "subscription.created"
	EventPaymentCompleted    = "payment.completed"
	// EventApplicationFirstSent: the first email a student ever sent through 9itchi has left (once per student).
	EventApplicationFirstSent = "application.first_sent"
	// EventApplicationReplied: a student marked one of their applications as answered by the company.
	EventApplicationReplied = "application.replied"
)

// Publisher sends domain events. Swap LogPublisher for a RabbitMQ implementation without touching services.
type Publisher interface {
	Publish(ctx context.Context, event string, payload any) error
}

// LogPublisher logs the event name only (never the payload). Default until RabbitMQ is wired.
type LogPublisher struct{}

func (LogPublisher) Publish(_ context.Context, event string, _ any) error {
	utils.Logger.Info("event", zap.String("name", event))
	return nil
}
