package oauth

import (
	"context"
	"testing"
	"time"

	"connect.xai.run/internal/hydra"
	"connect.xai.run/internal/identity"
)

func TestLoginServiceCompletesActiveDiscourseLogin(t *testing.T) {
	fake := hydra.NewFake()
	request := validLoginRequest()
	fake.Login = request
	service := NewLoginService(fake, fakeStatusLookup{status: identity.StatusSnapshot{
		Subject: "sub_1", Active: true,
	}}, time.Hour)

	redirect, err := service.Complete(context.Background(), request.Challenge, "sub_1")
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if redirect == "" || fake.LastLogin.Subject != "sub_1" || fake.LastLogin.RememberFor != 3600 {
		t.Fatalf("redirect/acceptance = %q/%#v", redirect, fake.LastLogin)
	}
}

func TestLoginServiceRejectsSuspendedUser(t *testing.T) {
	fake := hydra.NewFake()
	request := validLoginRequest()
	fake.Login = request
	service := NewLoginService(fake, fakeStatusLookup{status: identity.StatusSnapshot{
		Subject: "sub_1", Active: false, Suspended: true,
	}}, time.Hour)
	if _, err := service.Complete(context.Background(), request.Challenge, "sub_1"); err == nil {
		t.Fatal("Complete() allowed suspended user")
	}
	if fake.LastLogin.Subject != "" {
		t.Fatal("Hydra login was accepted for suspended user")
	}
}
