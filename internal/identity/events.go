package identity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/store"
)

type StatusEvent struct {
	EventID    string       `json:"event_id"`
	UserID     int64        `json:"user_id"`
	EventType  string       `json:"event_type"`
	OccurredAt time.Time    `json:"occurred_at"`
	Status     UserSnapshot `json:"status"`
}

type EventConsumer struct {
	verifier *Verifier
	users    store.UserRepository
	eventIDs NonceStore
	cache    LevelProgressCache
	now      func() time.Time
}

func NewEventConsumer(verifier *Verifier, users store.UserRepository, eventIDs NonceStore, cache LevelProgressCache) *EventConsumer {
	return &EventConsumer{verifier: verifier, users: users, eventIDs: eventIDs, cache: cache, now: time.Now}
}

func (c *EventConsumer) ServeHTTP(w http.ResponseWriter, r *http.Request) error {
	if c == nil || c.verifier == nil || c.users == nil || c.eventIDs == nil {
		writeEventError(w, http.StatusServiceUnavailable)
		return errors.New("identity event consumer is not initialized")
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return errors.New("identity events require POST")
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeEventError(w, http.StatusBadRequest)
		return err
	}
	if err := c.verifier.Verify(r.Context(), r, body); err != nil {
		writeEventError(w, http.StatusUnauthorized)
		return err
	}
	var event StatusEvent
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		writeEventError(w, http.StatusBadRequest)
		return fmt.Errorf("decode identity event: %w", err)
	}
	if event.EventID == "" || len(event.EventID) > 128 || event.UserID <= 0 ||
		event.EventType != "user_status_changed" || event.Status.DiscourseID != event.UserID {
		writeEventError(w, http.StatusBadRequest)
		return errors.New("invalid identity event")
	}
	accepted, err := c.eventIDs.CheckAndStore(r.Context(), event.EventID, c.now().UTC().Add(24*time.Hour))
	if err != nil {
		writeEventError(w, http.StatusServiceUnavailable)
		return err
	}
	if !accepted {
		writeEventError(w, http.StatusConflict)
		return ErrReplay
	}
	user, err := c.users.GetByDiscourseID(r.Context(), event.UserID)
	if err != nil {
		writeEventError(w, http.StatusNotFound)
		return err
	}
	status := event.Status
	if status.Subject != "" && status.Subject != string(user.Subject) {
		writeEventError(w, http.StatusBadRequest)
		return ErrIdentityMismatch
	}
	now := c.now().UTC()
	if event.OccurredAt.IsZero() {
		event.OccurredAt = now
	}
	if err := c.users.Upsert(r.Context(), domain.User{
		Subject: user.Subject, DiscourseID: user.DiscourseID, Username: status.Username,
		Name: status.Name, Email: status.Email, AvatarURL: status.AvatarURL, TrustLevel: status.TrustLevel,
		Active: status.Active, Silenced: status.Silenced, Suspended: status.Suspended,
		Reviewer: status.Reviewer, Admin: status.Admin, CreatedAt: user.CreatedAt, UpdatedAt: now,
	}); err != nil {
		writeEventError(w, http.StatusServiceUnavailable)
		return err
	}
	if c.cache != nil {
		if err := c.cache.Invalidate(r.Context(), event.UserID); err != nil {
			slog.Warn("invalidate level progress cache after identity event", "discourse_id", event.UserID, "error", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (c *EventConsumer) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = c.ServeHTTP(w, r)
	})
}

func writeEventError(w http.ResponseWriter, status int) {
	w.WriteHeader(status)
}
