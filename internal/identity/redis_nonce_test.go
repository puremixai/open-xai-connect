package identity

import (
	"context"
	"testing"
	"time"
)

func TestRedisNonceStoreRequiresRedisClient(t *testing.T) {
	store := NewRedisNonceStore(nil, "")
	accepted, err := store.CheckAndStore(context.Background(), "nonce", time.Now().Add(time.Minute))
	if err == nil || accepted {
		t.Fatalf("accepted/error = %v/%v", accepted, err)
	}
}
