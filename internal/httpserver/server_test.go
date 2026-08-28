package httpserver

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthzReturnsJSONSuccess(t *testing.T) {
	handler := New(Dependencies{})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
	if got := response.Body.String(); got != "{\"status\":\"ok\"}\n" {
		t.Fatalf("body = %q", got)
	}
}

func TestReadyzReturnsServiceUnavailableWhenDependencyFails(t *testing.T) {
	handler := New(Dependencies{
		Ready: func() error { return errors.New("database unavailable") },
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if got := response.Body.String(); got != "{\"status\":\"not_ready\"}\n" {
		t.Fatalf("body = %q", got)
	}
}

func TestReadyzReturnsJSONSuccessWhenDependenciesAreReady(t *testing.T) {
	handler := New(Dependencies{Ready: func() error { return nil }})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Body.String(); got != "{\"status\":\"ready\"}\n" {
		t.Fatalf("body = %q", got)
	}
}
