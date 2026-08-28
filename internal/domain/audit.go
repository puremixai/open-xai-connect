package domain

import "time"

type AuditEvent struct {
	ID            string
	ActorSubject  string
	Action        string
	ApplicationID ApplicationID
	Metadata      map[string]string
	CreatedAt     time.Time
}

func (e AuditEvent) Clone() AuditEvent {
	copy := e
	copy.Metadata = make(map[string]string, len(e.Metadata))
	for key, value := range e.Metadata {
		copy.Metadata[key] = value
	}
	return copy
}
