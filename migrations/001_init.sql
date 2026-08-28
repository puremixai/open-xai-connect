CREATE TABLE users (
    subject TEXT PRIMARY KEY,
    discourse_id BIGINT NOT NULL UNIQUE,
    username TEXT NOT NULL,
    display_name TEXT NOT NULL,
    avatar_url TEXT NOT NULL DEFAULT '',
    trust_level SMALLINT NOT NULL DEFAULT 0,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    silenced BOOLEAN NOT NULL DEFAULT FALSE,
    suspended BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE applications (
    id TEXT PRIMARY KEY,
    owner_subject TEXT NOT NULL REFERENCES users(subject),
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    logo_url TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('draft', 'pending_review', 'provisioning', 'approved', 'rejected', 'changes_requested', 'revoked')),
    client_id TEXT UNIQUE,
    encrypted_client_secret TEXT NOT NULL DEFAULT '',
    secret_version INTEGER NOT NULL DEFAULT 0,
    review_note TEXT NOT NULL DEFAULT '',
    reviewed_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE application_callbacks (
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    callback_url TEXT NOT NULL,
    PRIMARY KEY (application_id, callback_url)
);

CREATE TABLE application_domains (
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    domain TEXT NOT NULL,
    PRIMARY KEY (application_id, domain)
);

CREATE TABLE consents (
    user_subject TEXT NOT NULL REFERENCES users(subject) ON DELETE CASCADE,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    scopes TEXT[] NOT NULL,
    remember BOOLEAN NOT NULL DEFAULT FALSE,
    granted_at TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (user_subject, application_id)
);

CREATE TABLE outbox_events (
    id TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    kind TEXT NOT NULL,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    payload BYTEA NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL,
    claimed_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE audit_events (
    id TEXT PRIMARY KEY,
    actor_subject TEXT NOT NULL,
    action TEXT NOT NULL,
    application_id TEXT REFERENCES applications(id) ON DELETE SET NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX applications_owner_status_idx ON applications(owner_subject, status);
CREATE INDEX outbox_available_idx ON outbox_events(available_at) WHERE completed_at IS NULL;
CREATE INDEX audit_application_created_idx ON audit_events(application_id, created_at);
