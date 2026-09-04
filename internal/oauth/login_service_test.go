package oauth

import (
	"context"
	"strings"
	"testing"
	"time"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/hydra"
	"connect.xai.run/internal/identity"
	"connect.xai.run/internal/store/memory"
)

func TestLoginServiceCompletesActiveDiscourseLogin(t *testing.T) {
	fake := hydra.NewFake()
	request := validLoginRequest()
	fake.Login = request
	service := NewLoginService(fake, fakeStatusLookup{status: identity.StatusSnapshot{
		Subject: "sub_1", Active: true,
	}}, nil, time.Hour)

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
	}}, nil, time.Hour)
	if _, err := service.Complete(context.Background(), request.Challenge, "sub_1"); err == nil {
		t.Fatal("Complete() allowed suspended user")
	}
	if fake.LastLogin.Subject != "" {
		t.Fatal("Hydra login was accepted for suspended user")
	}
}

func TestLoginServiceAllowsLegacyRequestWhenApplicationDisablesPKCENonce(t *testing.T) {
	fake := hydra.NewFake()
	request := validLoginRequest()
	request.RequestURL = strings.Replace(request.RequestURL, "&code_challenge=challenge-1", "", 1)
	request.RequestURL = strings.Replace(request.RequestURL, "&code_challenge_method=S256", "", 1)
	request.RequestURL = strings.Replace(request.RequestURL, "&nonce=nonce-1", "", 1)
	fake.Login = request
	apps := memory.NewApplicationRepository()
	if err := apps.Create(context.Background(), domain.Application{
		ID: "app_1", OwnerSubject: "sub_1", Name: "Legacy client", Status: domain.StatusApproved,
		ClientID: domain.ClientID(request.Client.ID), RequirePKCENonce: false,
	}); err != nil {
		t.Fatalf("seed application: %v", err)
	}
	service := NewLoginService(fake, fakeStatusLookup{status: identity.StatusSnapshot{
		Subject: "sub_1", Active: true,
	}}, apps, time.Hour)

	redirect, err := service.Complete(context.Background(), request.Challenge, "sub_1")
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if redirect == "" || fake.LastLogin.Subject != "sub_1" {
		t.Fatalf("legacy login was not accepted: redirect=%q login=%#v", redirect, fake.LastLogin)
	}
}
