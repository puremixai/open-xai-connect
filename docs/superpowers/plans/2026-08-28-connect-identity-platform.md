# XAI Connect Identity Platform Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (- [ ]) syntax for tracking.

**Goal:** Build a standalone Go service at connect.xai.run that lets approved third-party web applications authenticate users with their Discourse accounts through OIDC, while keeping Discourse as the only source of identity.

**Architecture:** A Go Portal owns application registration, review, consent, sessions, current user status, and admin workflows. Ory Hydra v26.2.0 owns OAuth/OIDC protocol state and token signing. A small Discourse plugin verifies signed callbacks and exposes the minimum user/status data to the Portal. PostgreSQL stores durable state, Redis stores short-lived sessions and rate limits.

**Tech Stack:** Go, net/http, html/template, PostgreSQL with pgx/v5, Redis with go-redis/v9, Ory Hydra v26.2.0 and github.com/ory/hydra-client-go/v26, Docker Compose, and Ruby/RSpec for the Discourse plugin.

**Spec:** docs/superpowers/specs/2026-08-28-connect-identity-platform-design.md

## Global Constraints

- Discourse remains the account and password system; Connect keeps only a shadow identity mapping, application ownership, sessions, consents, and audit records.
- Only TL1-or-higher, active, non-silenced users can submit applications. A user may have at most three open applications. Suspended users cannot authenticate; silenced users may authenticate but cannot manage applications.
- The default lifecycle is draft -> provisioning -> approved. Legacy or explicitly gated applications may use pending_review before provisioning. No Hydra client or credentials exist before provisioning; provisioning creates the production client.
- Only confidential server-side web clients are supported. Authorization Code with PKCE S256 is required. Callback URLs are exact HTTPS URLs; wildcard, localhost, and IP callbacks are rejected.
- Claims are limited to stable Connect sub, username, display name, avatar URL, trust level, active, and silenced. Do not expose email, groups, external IDs, admin flags, or Discourse API keys.
- Client secrets are encrypted at rest, masked by default, re-displayable in plaintext only to the owning developer after recent sensitive-action confirmation, never logged, and resettable by admins without retrieval.
- Endpoints include discovery, authorization, token, revocation, userinfo, and JWKS. Scopes are openid, profile, community, and optional offline_access. Use the long approved TTLs from the design specification.
- Hydra admin APIs are private. Outbox events, idempotency keys, audit entries, nonce checks, HMAC verification, CSRF protection, secure cookies, rate limits, and fail-closed behavior are mandatory.
- All source files and generated documentation use UTF-8. Every task below adds tests before or together with implementation and ends with a focused verification command.

---

## Task 1: Bootstrap the Go Portal

- [ ] Create go.mod with the module path connect.xai.run, the local Go version, and pinned dependencies for pgx/v5, go-redis/v9, and github.com/ory/hydra-client-go/v26.
- [ ] Add internal/config/config.go with type Config containing HTTP listen address, public issuer URL, Discourse URL and shared secret, PostgreSQL DSN, Redis URL, Hydra public/admin URLs, encryption key, cookie settings, and environment name. Implement func Load() (Config, error) with required-value and 32-byte-key validation.
- [ ] Add internal/config/config_test.go covering missing variables, malformed encryption key, and a complete valid environment.
- [ ] Add internal/httpserver/server.go with func New(deps Dependencies) http.Handler; register /healthz, /readyz, discovery, and a versioned Portal route group. Add internal/httpserver/server_test.go for health responses, JSON content type, and readiness failure.
- [ ] Add cmd/connect/main.go to load configuration, construct dependencies, start the server with graceful shutdown, and expose structured startup errors.
- [ ] Add Dockerfile, .dockerignore, and Makefile targets test, lint, build, and run.
- [ ] Run go test ./internal/config ./internal/httpserver and go vet ./...; commit as build: bootstrap Go Connect Portal.

## Task 2: Add durable domain state, repositories, and secret encryption

- [ ] Add internal/domain/application.go, user.go, consent.go, outbox.go, and audit.go with typed IDs, lifecycle constants, timestamps, callback/domain fields, review fields, and credential metadata.
- [ ] Add repository interfaces in internal/store/store.go: ApplicationRepository, UserRepository, ConsentRepository, OutboxRepository, and AuditRepository, with context-aware CRUD and compare-and-transition methods.
- [ ] Add internal/store/memory implementations for deterministic tests and internal/store/postgres implementations using pgx transactions and parameterized SQL.
- [ ] Add migrations/001_init.sql for users, applications, callbacks, verified domains, consents, outbox, audit events, and unique constraints for ownership, client IDs, and stable Connect subjects.
- [ ] Add internal/secrets/box.go with type Box and func NewBox(key []byte) (*Box, error), Encrypt(plaintext, associatedData string) (string, error), and Decrypt(ciphertext, associatedData string) (string, error) using AES-256-GCM and random nonces.
- [ ] Add repository, state-transition, migration-shape, and encryption tests, including wrong-associated-data and tamper failures.
- [ ] Run go test ./internal/domain ./internal/store/... ./internal/secrets; commit as feat: add Connect domain persistence and encrypted secrets.

## Task 3: Implement Discourse identity verification and sessions

- [ ] Add internal/identity/provider.go defining Provider, UserSnapshot, StatusSnapshot, and StatusLookup.
- [ ] Add internal/identity/discourseconnect.go with HMAC-SHA256 verification over method, path, timestamp, nonce, and body; reject stale timestamps, nonce reuse, bad signatures, suspended users, and malformed payloads.
- [ ] Add internal/identity/status.go to refresh the current Discourse status before developer actions and token/userinfo issuance.
- [ ] Add internal/session/store.go, memory.go, and redis.go with Create, Get, Touch, and Delete; use opaque random IDs, hashed storage keys, SameSite=Lax, Secure, HttpOnly cookies, sliding 30-day and absolute 90-day expiry.
- [ ] Add tests for valid/invalid signatures, replay protection, suspended and silenced status, session rotation, cookie flags, and Redis outage fail-closed behavior.
- [ ] Wire /connect/login, /connect/callback, /connect/logout, and /connect/me into the HTTP server. Ensure TL1 is checked only for developer-management actions.
- [ ] Run go test ./internal/identity ./internal/session ./internal/httpserver; commit as feat: add Discourse-backed identity and sessions.

## Task 4: Integrate Hydra and expose the OIDC protocol

- [ ] Add internal/hydra/client.go with a small interface: CreateClient, DeleteClient, GetLoginRequest, AcceptLogin, RejectLogin, GetConsentRequest, AcceptConsent, RejectConsent, RevokeSession, and IntrospectToken.
- [ ] Add internal/hydra/ory.go backed by the pinned official SDK github.com/ory/hydra-client-go/v26, mapping SDK errors to stable Portal errors. Add internal/hydra/fake.go for protocol tests.
- [ ] Add internal/oauth/login.go to validate client_id, exact redirect URI, response type code, scope, state, and PKCE S256 before forwarding the challenge to the Portal login flow.
- [ ] Add internal/oauth/consent.go to show the registered application name/logo, requested scopes, and minimal claims; persist remember decisions and call Hydra accept/reject APIs.
- [ ] Add internal/oauth/userinfo.go to introspect the access token, refresh current Discourse status, and return only approved claims. Add discovery and JWKS proxy handlers.
- [ ] Add tests for authorization-code-only behavior, PKCE enforcement, redirect mismatch, scope filtering, consent persistence, revocation, current status, and Hydra outage behavior.
- [ ] Run go test ./internal/hydra ./internal/oauth ./internal/httpserver; commit as feat: add Hydra OIDC authorization flows.

## Task 5: Implement self-service application review and provisioning

- [ ] Add internal/apps/service.go with CreateDraft, UpdateDraft, Submit, ListMine, GetMine, ViewSecret, RotateSecret, and Revoke methods. Enforce TL1 (with `connect-admins` bypass), active/non-silenced status, three-open-app limit, single owner, exact HTTPS callbacks, verified-domain matching, and safe logo MIME/size checks; default creation queues Provisioning without manual review.
- [ ] Add internal/reviews/service.go with reviewer authorization from configured Discourse groups, ListPending, Approve, Reject, and RequestChanges; approval must atomically transition to provisioning and enqueue an idempotent outbox event.
- [ ] Add an outbox worker that creates the Hydra client only after approval, encrypts the returned secret, stores the client ID, retries transient failures with backoff, and records audit events. Never place credentials in logs.
- [ ] Add Portal templates and static assets for app list, create/edit, submit, review queue, consent, secret confirmation, and error pages. Use server-rendered HTML with small progressive-enhancement JavaScript.
- [ ] Add tests for every authorization/state transition, URI/domain validation, logo sanitization, secret confirmation and rotation, idempotent provisioning, audit records, and reviewer actions.
- [ ] Run go test ./internal/apps ./internal/reviews ./internal/audit ./internal/assets ./...; commit as feat: add reviewed application onboarding.

## Task 6: Build the thin Discourse plugin

- [ ] Create discourse-plugin/connect-identity/plugin.rb with settings for Connect base URL and shared secret, route registration, and an initializer that fails closed when configuration is absent.
- [ ] Add controllers for GET /connect/identity/users/:id and POST /connect/identity/events; sign requests with timestamp/nonce/HMAC headers and return only the approved user/status fields.
- [ ] Add serializers, locale strings, README configuration, and request specs covering permissions, suspended users, signature generation, replay fields, and response filtering.
- [ ] Run the plugin’s RuboCop/RSpec command in its documented container and commit as feat: add Discourse identity bridge plugin.

## Task 7: Package deployment, integration tests, and operator documentation

- [ ] Add deploy/docker-compose.yml for Portal, Hydra v26.2.0, PostgreSQL, and Redis; expose only Hydra public and Portal public ports, keeping Hydra admin on the internal network.
- [ ] Add .env.example, Hydra config/template, reverse-proxy example for connect.xai.run, health checks, backup notes, key rotation procedure, and scripts/check-compose.ps1.
- [ ] Add docs/integration-guide.md, docs/operator-runbook.md, and examples/go-client showing discovery, PKCE authorization, token exchange, UserInfo, refresh rotation, and logout without embedding real secrets.
- [ ] Add an integration test that starts fake Discourse and fake Hydra services, submits an app, approves it, completes login/consent, validates claims, revokes the session, and verifies suspended-user denial.
- [ ] Run go test ./..., go vet ./..., go build ./cmd/connect, the plugin specs, and scripts/check-compose.ps1; commit as docs: package Connect deployment and integration checks.

## Completion checklist

- [ ] go test ./..., go vet ./..., and go build ./cmd/connect pass from D:\bbs\xai-connect.
- [ ] Plugin request specs pass in a Discourse test container.
- [ ] The compose validation confirms Hydra admin is not externally published and all required secrets are supplied through environment or secret mounts.
- [ ] The integration test proves draft submission, manual review, direct production provisioning, PKCE OIDC login, consent, current status claims, revocation, and suspension denial.
- [ ] Review the diff for credentials, wildcard callbacks, raw Discourse IDs in public claims, plaintext secret logs, and non-UTF-8 files before release.
