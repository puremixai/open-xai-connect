package store

import (
	"context"
	"errors"
	"time"

	"connect.xai.run/internal/domain"
)

var (
	ErrNotFound = errors.New("record not found")
	ErrConflict = errors.New("record conflict")
)

type ApplicationRepository interface {
	Create(context.Context, domain.Application) error
	Get(context.Context, domain.ApplicationID) (domain.Application, error)
	GetByClientID(context.Context, string) (domain.Application, error)
	ListByOwner(context.Context, string) ([]domain.Application, error)
	ListByStatus(context.Context, domain.ApplicationStatus) ([]domain.Application, error)
	CountOpenByOwner(context.Context, string) (int, error)
	Transition(context.Context, domain.ApplicationID, domain.ApplicationStatus, domain.ApplicationStatus, time.Time) (domain.Application, error)
	Save(context.Context, domain.Application) error
	Delete(context.Context, domain.ApplicationID) error
}

type UserRepository interface {
	Upsert(context.Context, domain.User) error
	GetBySubject(context.Context, domain.UserID) (domain.User, error)
	GetByDiscourseID(context.Context, int64) (domain.User, error)
}

type ConsentRepository interface {
	Get(context.Context, domain.UserID, domain.ApplicationID) (domain.Consent, error)
	Put(context.Context, domain.Consent) error
	Delete(context.Context, domain.UserID, domain.ApplicationID) error
}

type OutboxRepository interface {
	Enqueue(context.Context, domain.OutboxEvent) error
	ClaimNext(context.Context, time.Time) (domain.OutboxEvent, error)
	Retry(context.Context, string, time.Time) error
	Complete(context.Context, string, time.Time) error
}

type AuditRepository interface {
	Append(context.Context, domain.AuditEvent) error
	ListByApplication(context.Context, domain.ApplicationID) ([]domain.AuditEvent, error)
}
