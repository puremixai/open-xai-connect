package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/store"
)

type ApplicationRepository struct {
	mu   sync.RWMutex
	data map[domain.ApplicationID]domain.Application
}

func NewApplicationRepository() *ApplicationRepository {
	return &ApplicationRepository{data: make(map[domain.ApplicationID]domain.Application)}
}

func (r *ApplicationRepository) Create(_ context.Context, app domain.Application) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.data[app.ID]; exists {
		return store.ErrConflict
	}
	now := time.Now().UTC()
	if app.Status == "" {
		app.Status = domain.StatusDraft
	}
	if app.CreatedAt.IsZero() {
		app.CreatedAt = now
	}
	if app.UpdatedAt.IsZero() {
		app.UpdatedAt = app.CreatedAt
	}
	r.data[app.ID] = app.Clone()
	return nil
}

func (r *ApplicationRepository) Get(_ context.Context, id domain.ApplicationID) (domain.Application, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	app, exists := r.data[id]
	if !exists {
		return domain.Application{}, store.ErrNotFound
	}
	return app.Clone(), nil
}

func (r *ApplicationRepository) ListByOwner(_ context.Context, owner string) ([]domain.Application, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]domain.Application, 0)
	for _, app := range r.data {
		if app.OwnerSubject == owner {
			result = append(result, app.Clone())
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, nil
}

func (r *ApplicationRepository) CountOpenByOwner(_ context.Context, owner string) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, app := range r.data {
		if app.OwnerSubject == owner && app.IsOpen() {
			count++
		}
	}
	return count, nil
}

func (r *ApplicationRepository) Transition(_ context.Context, id domain.ApplicationID, from, to domain.ApplicationStatus, now time.Time) (domain.Application, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	app, exists := r.data[id]
	if !exists {
		return domain.Application{}, store.ErrNotFound
	}
	if app.Status != from {
		return domain.Application{}, store.ErrConflict
	}
	if err := app.Transition(to, now); err != nil {
		return domain.Application{}, err
	}
	r.data[id] = app.Clone()
	return app.Clone(), nil
}

func (r *ApplicationRepository) Save(_ context.Context, app domain.Application) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.data[app.ID]; !exists {
		return store.ErrNotFound
	}
	r.data[app.ID] = app.Clone()
	return nil
}

type UserRepository struct {
	mu          sync.RWMutex
	bySubject   map[domain.UserID]domain.User
	byDiscourse map[int64]domain.UserID
}

func NewUserRepository() *UserRepository {
	return &UserRepository{bySubject: make(map[domain.UserID]domain.User), byDiscourse: make(map[int64]domain.UserID)}
}

func (r *UserRepository) Upsert(_ context.Context, user domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bySubject[user.Subject] = user
	r.byDiscourse[user.DiscourseID] = user.Subject
	return nil
}

func (r *UserRepository) GetBySubject(_ context.Context, subject domain.UserID) (domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	user, ok := r.bySubject[subject]
	if !ok {
		return domain.User{}, store.ErrNotFound
	}
	return user, nil
}

func (r *UserRepository) GetByDiscourseID(_ context.Context, id int64) (domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	subject, ok := r.byDiscourse[id]
	if !ok {
		return domain.User{}, store.ErrNotFound
	}
	return r.bySubject[subject], nil
}

type ConsentRepository struct {
	mu   sync.RWMutex
	data map[string]domain.Consent
}

func NewConsentRepository() *ConsentRepository {
	return &ConsentRepository{data: make(map[string]domain.Consent)}
}

func consentKey(user domain.UserID, app domain.ApplicationID) string {
	return string(user) + "\x00" + string(app)
}

func (r *ConsentRepository) Get(_ context.Context, user domain.UserID, app domain.ApplicationID) (domain.Consent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	consent, ok := r.data[consentKey(user, app)]
	if !ok {
		return domain.Consent{}, store.ErrNotFound
	}
	return consent.Clone(), nil
}

func (r *ConsentRepository) Put(_ context.Context, consent domain.Consent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[consentKey(consent.UserSubject, consent.ApplicationID)] = consent.Clone()
	return nil
}

func (r *ConsentRepository) Delete(_ context.Context, user domain.UserID, app domain.ApplicationID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.data, consentKey(user, app))
	return nil
}

type OutboxRepository struct {
	mu    sync.Mutex
	data  map[string]domain.OutboxEvent
	byKey map[string]string
}

func NewOutboxRepository() *OutboxRepository {
	return &OutboxRepository{data: make(map[string]domain.OutboxEvent), byKey: make(map[string]string)}
}

func (r *OutboxRepository) Enqueue(_ context.Context, event domain.OutboxEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if event.IdempotencyKey != "" {
		if _, exists := r.byKey[event.IdempotencyKey]; exists {
			return store.ErrConflict
		}
	}
	if event.ID == "" {
		event.ID = "outbox_" + time.Now().UTC().Format("20060102150405.000000000")
	}
	if event.AvailableAt.IsZero() {
		event.AvailableAt = time.Now().UTC()
	}
	r.data[event.ID] = event.Clone()
	if event.IdempotencyKey != "" {
		r.byKey[event.IdempotencyKey] = event.ID
	}
	return nil
}

func (r *OutboxRepository) ClaimNext(_ context.Context, now time.Time) (domain.OutboxEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var selectedID string
	for id, event := range r.data {
		if event.CompletedAt == nil && event.ClaimedAt == nil && !event.AvailableAt.After(now) {
			selectedID = id
			break
		}
	}
	if selectedID == "" {
		return domain.OutboxEvent{}, store.ErrNotFound
	}
	event := r.data[selectedID]
	event.Attempts++
	claimed := now.UTC()
	event.ClaimedAt = &claimed
	r.data[selectedID] = event.Clone()
	return event.Clone(), nil
}

func (r *OutboxRepository) Complete(_ context.Context, id string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	event, ok := r.data[id]
	if !ok {
		return store.ErrNotFound
	}
	completed := now.UTC()
	event.CompletedAt = &completed
	r.data[id] = event.Clone()
	return nil
}

type AuditRepository struct {
	mu   sync.RWMutex
	data []domain.AuditEvent
}

func NewAuditRepository() *AuditRepository {
	return &AuditRepository{data: make([]domain.AuditEvent, 0)}
}

func (r *AuditRepository) Append(_ context.Context, event domain.AuditEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	r.data = append(r.data, event.Clone())
	return nil
}

func (r *AuditRepository) ListByApplication(_ context.Context, app domain.ApplicationID) ([]domain.AuditEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]domain.AuditEvent, 0)
	for _, event := range r.data {
		if event.ApplicationID == app {
			result = append(result, event.Clone())
		}
	}
	return result, nil
}
