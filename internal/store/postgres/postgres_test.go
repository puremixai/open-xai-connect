package postgres

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"connect.xai.run/internal/store"
)

func TestMapErrorConvertsMissingRows(t *testing.T) {
	if !errors.Is(mapError(pgx.ErrNoRows), store.ErrNotFound) {
		t.Fatal("mapError(pgx.ErrNoRows) is not store.ErrNotFound")
	}
}

func TestMapErrorPreservesOtherErrors(t *testing.T) {
	original := errors.New("database down")
	if !errors.Is(mapError(original), original) {
		t.Fatal("mapError changed an unrelated error")
	}
}
