package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMemoryStoreCreatesTouchesAndDeletesOpaqueSessions(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	store := NewMemoryStore(30*24*time.Hour, 90*24*time.Hour)
	session, err := store.Create(context.Background(), "sub_1", now)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(session.ID) < 32 || session.UserSubject != "sub_1" {
		t.Fatalf("session = %#v", session)
	}
	got, err := store.Get(context.Background(), session.ID, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.UserSubject != "sub_1" {
		t.Fatalf("subject = %q", got.UserSubject)
	}
	if _, err := store.Touch(context.Background(), session.ID, now.Add(2*time.Hour)); err != nil {
		t.Fatalf("Touch() error = %v", err)
	}
	if err := store.Delete(context.Background(), session.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := store.Get(context.Background(), session.ID, now.Add(3*time.Hour)); err == nil {
		t.Fatal("Get() after Delete() succeeded")
	}
}

func TestMemoryStoreEnforcesAbsoluteExpiry(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	store := NewMemoryStore(time.Hour, 2*time.Hour)
	session, err := store.Create(context.Background(), "sub_1", now)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := store.Get(context.Background(), session.ID, now.Add(2*time.Hour+time.Second)); err == nil {
		t.Fatal("Get() after absolute expiry succeeded")
	}
}

func TestSessionHomeVerificationPersistsInMemoryStore(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	store := NewMemoryStore(time.Hour, 2*time.Hour)
	created, err := store.Create(context.Background(), "sub_1", now)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.HomeVerified {
		t.Fatal("new session is already home verified")
	}

	marked, err := store.MarkHomeVerified(context.Background(), created.ID, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("MarkHomeVerified() error = %v", err)
	}
	if !marked.HomeVerified {
		t.Fatal("marked session is not home verified")
	}

	got, err := store.Get(context.Background(), created.ID, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !got.HomeVerified {
		t.Fatal("home verification was not persisted")
	}
}

func TestSessionCookieUsesSecureHttpOnlyDefaults(t *testing.T) {
	cookie := NewCookie("connect_session", "opaque-id", 3600, true)
	if cookie.Name != "connect_session" || cookie.Value != "opaque-id" {
		t.Fatalf("cookie = %#v", cookie)
	}
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie flags = secure:%v httponly:%v samesite:%v", cookie.Secure, cookie.HttpOnly, cookie.SameSite)
	}
	if cookie.MaxAge != 3600 {
		t.Fatalf("MaxAge = %d", cookie.MaxAge)
	}
}

func TestRedisSessionKeyDoesNotContainRawSessionID(t *testing.T) {
	id := "opaque-session-id"
	key := redisKey("connect:", id)
	if key == "" || strings.Contains(key, id) {
		t.Fatalf("redis key = %q", key)
	}
}

func TestHTTPSessionHandlerEstablishesReadsAndClearsCookie(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	store := NewMemoryStore(time.Hour, 2*time.Hour)
	handler := NewHTTPHandler(store, "connect_session", true)
	handler.now = func() time.Time { return now }
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "https://connect.example/connect/me", nil)
	if _, err := handler.Establish(response, request, "sub_1"); err != nil {
		t.Fatalf("Establish() error = %v", err)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatalf("set-cookie = %#v", cookies)
	}
	request.AddCookie(cookies[0])
	current, err := handler.Current(request)
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if current.UserSubject != "sub_1" {
		t.Fatalf("subject = %q", current.UserSubject)
	}
	logoutResponse := httptest.NewRecorder()
	if err := handler.Logout(logoutResponse, request); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	cleared := logoutResponse.Result().Cookies()
	if len(cleared) != 1 || cleared[0].MaxAge != -1 {
		t.Fatalf("clear cookie = %#v", cleared)
	}
}

func TestHTTPSessionHandlerTracksHomeVerification(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	handler := NewHTTPHandler(NewMemoryStore(time.Hour, 2*time.Hour), "connect_session", true)
	handler.now = func() time.Time { return now }
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "https://connect.example/", nil)
	if _, err := handler.Establish(response, request, "sub_1"); err != nil {
		t.Fatalf("Establish() error = %v", err)
	}
	request.AddCookie(response.Result().Cookies()[0])

	verified, err := handler.HomeVerified(request)
	if err != nil {
		t.Fatalf("HomeVerified() error = %v", err)
	}
	if verified {
		t.Fatal("new session is already home verified")
	}
	if err := handler.MarkHomeVerified(request); err != nil {
		t.Fatalf("MarkHomeVerified() error = %v", err)
	}
	verified, err = handler.HomeVerified(request)
	if err != nil {
		t.Fatalf("HomeVerified() after mark error = %v", err)
	}
	if !verified {
		t.Fatal("home verification was not reported")
	}
}
