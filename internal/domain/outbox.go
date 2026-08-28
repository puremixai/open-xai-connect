package domain

import "time"

type OutboxEvent struct {
	ID             string
	IdempotencyKey string
	Kind           string
	ApplicationID  ApplicationID
	Payload        []byte
	Attempts       int
	AvailableAt    time.Time
	ClaimedAt      *time.Time
	CompletedAt    *time.Time
	CreatedAt      time.Time
}

func (e OutboxEvent) Clone() OutboxEvent {
	copy := e
	copy.Payload = append([]byte(nil), e.Payload...)
	if e.ClaimedAt != nil {
		value := *e.ClaimedAt
		copy.ClaimedAt = &value
	}
	if e.CompletedAt != nil {
		value := *e.CompletedAt
		copy.CompletedAt = &value
	}
	return copy
}
