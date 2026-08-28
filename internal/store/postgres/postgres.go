package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/store"
)

type Store struct {
	Pool *pgxpool.Pool
}

func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Store{Pool: pool}, nil
}

func NewWithPool(pool *pgxpool.Pool) *Store {
	return &Store{Pool: pool}
}

func (s *Store) Close() {
	if s != nil && s.Pool != nil {
		s.Pool.Close()
	}
}

func (s *Store) Ready(ctx context.Context) error {
	if s == nil || s.Pool == nil {
		return errors.New("postgres pool is not initialized")
	}
	return s.Pool.Ping(ctx)
}

func mapError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ErrNotFound
	}
	return err
}

func (s *Store) Create(ctx context.Context, app domain.Application) error {
	if app.Status == "" {
		app.Status = domain.StatusDraft
	}
	now := time.Now().UTC()
	if app.CreatedAt.IsZero() {
		app.CreatedAt = now
	}
	if app.UpdatedAt.IsZero() {
		app.UpdatedAt = app.CreatedAt
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO applications
		(id, owner_subject, name, description, logo_url, status, client_id,
		 encrypted_client_secret, secret_version, review_note, reviewed_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8,$9,$10,$11,$12,$13)`,
		app.ID, app.OwnerSubject, app.Name, app.Description, app.LogoURL, app.Status,
		app.ClientID, app.EncryptedClientSecret, app.SecretVersion, app.ReviewNote, app.ReviewedBy,
		app.CreatedAt, app.UpdatedAt)
	if err != nil {
		return mapError(err)
	}
	if err := replaceApplicationLists(ctx, tx, app); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return mapError(err)
	}
	return nil
}

func (s *Store) Get(ctx context.Context, id domain.ApplicationID) (domain.Application, error) {
	app, err := scanApplication(s.Pool.QueryRow(ctx, applicationSelect+` WHERE id=$1`, id))
	if err != nil {
		return domain.Application{}, mapError(err)
	}
	if err := s.loadApplicationLists(ctx, &app); err != nil {
		return domain.Application{}, err
	}
	return app, nil
}

func (s *Store) GetByClientID(ctx context.Context, clientID string) (domain.Application, error) {
	app, err := scanApplication(s.Pool.QueryRow(ctx, applicationSelect+` WHERE client_id=$1`, clientID))
	if err != nil {
		return domain.Application{}, mapError(err)
	}
	if err := s.loadApplicationLists(ctx, &app); err != nil {
		return domain.Application{}, err
	}
	return app, nil
}

func (s *Store) ListByOwner(ctx context.Context, owner string) ([]domain.Application, error) {
	rows, err := s.Pool.Query(ctx, applicationSelect+` WHERE owner_subject=$1 ORDER BY created_at`, owner)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	result := make([]domain.Application, 0)
	for rows.Next() {
		app, scanErr := scanApplication(rows)
		if scanErr != nil {
			return nil, mapError(scanErr)
		}
		if err := s.loadApplicationLists(ctx, &app); err != nil {
			return nil, err
		}
		result = append(result, app)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}
	return result, nil
}

func (s *Store) ListByStatus(ctx context.Context, status domain.ApplicationStatus) ([]domain.Application, error) {
	rows, err := s.Pool.Query(ctx, applicationSelect+` WHERE status=$1 ORDER BY created_at`, status)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	result := make([]domain.Application, 0)
	for rows.Next() {
		app, scanErr := scanApplication(rows)
		if scanErr != nil {
			return nil, mapError(scanErr)
		}
		if err := s.loadApplicationLists(ctx, &app); err != nil {
			return nil, err
		}
		result = append(result, app)
	}
	return result, mapError(rows.Err())
}

func (s *Store) CountOpenByOwner(ctx context.Context, owner string) (int, error) {
	var count int
	err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM applications
		WHERE owner_subject=$1 AND status IN ('draft','pending_review','provisioning','changes_requested')`, owner).Scan(&count)
	return count, mapError(err)
}

func (s *Store) Transition(ctx context.Context, id domain.ApplicationID, from, to domain.ApplicationStatus, now time.Time) (domain.Application, error) {
	app, err := scanApplication(s.Pool.QueryRow(ctx, applicationSelect+` WHERE id=$1 AND status=$2`, id, from))
	if err != nil {
		return domain.Application{}, mapError(err)
	}
	if err := app.Transition(to, now); err != nil {
		return domain.Application{}, err
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE applications SET status=$1, updated_at=$2 WHERE id=$3 AND status=$4`, to, app.UpdatedAt, id, from)
	if err != nil {
		return domain.Application{}, mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return domain.Application{}, store.ErrConflict
	}
	if err := s.loadApplicationLists(ctx, &app); err != nil {
		return domain.Application{}, err
	}
	return app, nil
}

func (s *Store) Save(ctx context.Context, app domain.Application) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE applications SET owner_subject=$1, name=$2, description=$3,
		logo_url=$4, status=$5, client_id=NULLIF($6,''), encrypted_client_secret=$7,
		secret_version=$8, review_note=$9, reviewed_by=$10, updated_at=$11 WHERE id=$12`,
		app.OwnerSubject, app.Name, app.Description, app.LogoURL, app.Status, app.ClientID,
		app.EncryptedClientSecret, app.SecretVersion, app.ReviewNote, app.ReviewedBy, app.UpdatedAt, app.ID)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return store.ErrNotFound
	}
	if err := replaceApplicationLists(ctx, tx, app); err != nil {
		return err
	}
	return mapError(tx.Commit(ctx))
}

const applicationSelect = `SELECT id, owner_subject, name, description, logo_url, status,
	COALESCE(client_id,''), encrypted_client_secret, secret_version, review_note, reviewed_by,
	created_at, updated_at FROM applications`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanApplication(row rowScanner) (domain.Application, error) {
	var app domain.Application
	err := row.Scan(&app.ID, &app.OwnerSubject, &app.Name, &app.Description, &app.LogoURL,
		&app.Status, &app.ClientID, &app.EncryptedClientSecret, &app.SecretVersion,
		&app.ReviewNote, &app.ReviewedBy, &app.CreatedAt, &app.UpdatedAt)
	return app, err
}

func replaceApplicationLists(ctx context.Context, tx pgx.Tx, app domain.Application) error {
	if _, err := tx.Exec(ctx, `DELETE FROM application_callbacks WHERE application_id=$1`, app.ID); err != nil {
		return mapError(err)
	}
	for _, callback := range app.CallbackURLs {
		if _, err := tx.Exec(ctx, `INSERT INTO application_callbacks(application_id, callback_url) VALUES ($1,$2)`, app.ID, callback); err != nil {
			return mapError(err)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM application_domains WHERE application_id=$1`, app.ID); err != nil {
		return mapError(err)
	}
	for _, domainName := range app.VerifiedDomains {
		if _, err := tx.Exec(ctx, `INSERT INTO application_domains(application_id, domain) VALUES ($1,$2)`, app.ID, domainName); err != nil {
			return mapError(err)
		}
	}
	return nil
}

func (s *Store) loadApplicationLists(ctx context.Context, app *domain.Application) error {
	callbackRows, err := s.Pool.Query(ctx, `SELECT callback_url FROM application_callbacks WHERE application_id=$1 ORDER BY callback_url`, app.ID)
	if err != nil {
		return mapError(err)
	}
	defer callbackRows.Close()
	for callbackRows.Next() {
		var callback string
		if err := callbackRows.Scan(&callback); err != nil {
			return mapError(err)
		}
		app.CallbackURLs = append(app.CallbackURLs, callback)
	}
	if err := callbackRows.Err(); err != nil {
		return mapError(err)
	}
	domainRows, err := s.Pool.Query(ctx, `SELECT domain FROM application_domains WHERE application_id=$1 ORDER BY domain`, app.ID)
	if err != nil {
		return mapError(err)
	}
	defer domainRows.Close()
	for domainRows.Next() {
		var domainName string
		if err := domainRows.Scan(&domainName); err != nil {
			return mapError(err)
		}
		app.VerifiedDomains = append(app.VerifiedDomains, domainName)
	}
	return mapError(domainRows.Err())
}

func (s *Store) Upsert(ctx context.Context, user domain.User) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO users
		(subject, discourse_id, username, display_name, avatar_url, trust_level, active, silenced, suspended, reviewer, admin, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT(subject) DO UPDATE SET discourse_id=EXCLUDED.discourse_id, username=EXCLUDED.username,
		display_name=EXCLUDED.display_name, avatar_url=EXCLUDED.avatar_url, trust_level=EXCLUDED.trust_level,
		active=EXCLUDED.active, silenced=EXCLUDED.silenced, suspended=EXCLUDED.suspended,
		reviewer=EXCLUDED.reviewer, admin=EXCLUDED.admin, updated_at=EXCLUDED.updated_at`,
		user.Subject, user.DiscourseID, user.Username, user.Name, user.AvatarURL, user.TrustLevel,
		user.Active, user.Silenced, user.Suspended, user.Reviewer, user.Admin, user.CreatedAt, user.UpdatedAt)
	return mapError(err)
}

func (s *Store) GetBySubject(ctx context.Context, subject domain.UserID) (domain.User, error) {
	return s.getUser(ctx, `subject=$1`, subject)
}

func (s *Store) GetByDiscourseID(ctx context.Context, id int64) (domain.User, error) {
	return s.getUser(ctx, `discourse_id=$1`, id)
}

func (s *Store) getUser(ctx context.Context, predicate string, arg any) (domain.User, error) {
	var user domain.User
	err := s.Pool.QueryRow(ctx, `SELECT subject, discourse_id, username, display_name, avatar_url,
		trust_level, active, silenced, suspended, reviewer, admin, created_at, updated_at FROM users WHERE `+predicate, arg).
		Scan(&user.Subject, &user.DiscourseID, &user.Username, &user.Name, &user.AvatarURL,
			&user.TrustLevel, &user.Active, &user.Silenced, &user.Suspended, &user.Reviewer, &user.Admin,
			&user.CreatedAt, &user.UpdatedAt)
	return user, mapError(err)
}

type ConsentStore struct{ *Store }

func NewConsentStore(base *Store) *ConsentStore {
	return &ConsentStore{Store: base}
}

func (s *ConsentStore) Get(ctx context.Context, user domain.UserID, app domain.ApplicationID) (domain.Consent, error) {
	var consent domain.Consent
	err := s.Pool.QueryRow(ctx, `SELECT scopes, remember, granted_at, last_used_at FROM consents
		WHERE user_subject=$1 AND application_id=$2`, user, app).
		Scan(&consent.Scopes, &consent.Remember, &consent.GrantedAt, &consent.LastUsedAt)
	consent.UserSubject, consent.ApplicationID = user, app
	return consent, mapError(err)
}

func (s *ConsentStore) Put(ctx context.Context, consent domain.Consent) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO consents(user_subject, application_id, scopes, remember, granted_at, last_used_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT(user_subject, application_id) DO UPDATE SET scopes=EXCLUDED.scopes,
		remember=EXCLUDED.remember, last_used_at=EXCLUDED.last_used_at`,
		consent.UserSubject, consent.ApplicationID, consent.Scopes, consent.Remember, consent.GrantedAt, consent.LastUsedAt)
	return mapError(err)
}

func (s *ConsentStore) Delete(ctx context.Context, user domain.UserID, app domain.ApplicationID) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM consents WHERE user_subject=$1 AND application_id=$2`, user, app)
	return mapError(err)
}

type OutboxStore struct{ *Store }

func NewOutboxStore(base *Store) *OutboxStore {
	return &OutboxStore{Store: base}
}

func (s *OutboxStore) Enqueue(ctx context.Context, event domain.OutboxEvent) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO outbox_events
		(id, idempotency_key, kind, application_id, payload, attempts, available_at, claimed_at, completed_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		event.ID, event.IdempotencyKey, event.Kind, event.ApplicationID, event.Payload,
		event.Attempts, event.AvailableAt, event.ClaimedAt, event.CompletedAt, event.CreatedAt)
	return mapError(err)
}

func (s *OutboxStore) ClaimNext(ctx context.Context, now time.Time) (domain.OutboxEvent, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.OutboxEvent{}, err
	}
	defer tx.Rollback(ctx)
	var event domain.OutboxEvent
	err = tx.QueryRow(ctx, `SELECT id, idempotency_key, kind, application_id, payload, attempts,
		available_at, claimed_at, completed_at, created_at FROM outbox_events
		WHERE completed_at IS NULL AND claimed_at IS NULL AND available_at <= $1
		ORDER BY available_at, created_at LIMIT 1 FOR UPDATE SKIP LOCKED`, now).
		Scan(&event.ID, &event.IdempotencyKey, &event.Kind, &event.ApplicationID, &event.Payload,
			&event.Attempts, &event.AvailableAt, &event.ClaimedAt, &event.CompletedAt, &event.CreatedAt)
	if err != nil {
		return domain.OutboxEvent{}, mapError(err)
	}
	claimed := now.UTC()
	event.Attempts++
	event.ClaimedAt = &claimed
	if _, err := tx.Exec(ctx, `UPDATE outbox_events SET attempts=$1, claimed_at=$2 WHERE id=$3`, event.Attempts, claimed, event.ID); err != nil {
		return domain.OutboxEvent{}, mapError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.OutboxEvent{}, mapError(err)
	}
	return event, nil
}

func (s *OutboxStore) Complete(ctx context.Context, id string, now time.Time) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE outbox_events SET completed_at=$1, claimed_at=NULL WHERE id=$2 AND completed_at IS NULL`, now.UTC(), id)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return store.ErrNotFound
	}
	return nil
}

func (s *OutboxStore) Retry(ctx context.Context, id string, availableAt time.Time) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE outbox_events SET claimed_at=NULL, available_at=$1 WHERE id=$2 AND completed_at IS NULL`, availableAt.UTC(), id)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return store.ErrNotFound
	}
	return nil
}

type AuditStore struct{ *Store }

func NewAuditStore(base *Store) *AuditStore {
	return &AuditStore{Store: base}
}

func (s *AuditStore) Append(ctx context.Context, event domain.AuditEvent) error {
	metadata, err := json.Marshal(event.Metadata)
	if err != nil {
		return fmt.Errorf("marshal audit metadata: %w", err)
	}
	_, err = s.Pool.Exec(ctx, `INSERT INTO audit_events(id, actor_subject, action, application_id, metadata, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)`, event.ID, event.ActorSubject, event.Action, event.ApplicationID, metadata, event.CreatedAt)
	return mapError(err)
}

func (s *AuditStore) ListByApplication(ctx context.Context, app domain.ApplicationID) ([]domain.AuditEvent, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, actor_subject, action, application_id, metadata, created_at
		FROM audit_events WHERE application_id=$1 ORDER BY created_at`, app)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	result := make([]domain.AuditEvent, 0)
	for rows.Next() {
		var event domain.AuditEvent
		var raw json.RawMessage
		if err := rows.Scan(&event.ID, &event.ActorSubject, &event.Action, &event.ApplicationID, &raw, &event.CreatedAt); err != nil {
			return nil, mapError(err)
		}
		if err := json.Unmarshal(raw, &event.Metadata); err != nil {
			return nil, fmt.Errorf("decode audit metadata: %w", err)
		}
		result = append(result, event)
	}
	return result, mapError(rows.Err())
}

var (
	_ store.ApplicationRepository = (*Store)(nil)
	_ store.UserRepository        = (*Store)(nil)
	_ store.ConsentRepository     = (*ConsentStore)(nil)
	_ store.OutboxRepository      = (*OutboxStore)(nil)
	_ store.AuditRepository       = (*AuditStore)(nil)
)
