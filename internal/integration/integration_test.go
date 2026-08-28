package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"connect.xai.run/internal/apps"
	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/hydra"
	"connect.xai.run/internal/identity"
	"connect.xai.run/internal/oauth"
	"connect.xai.run/internal/reviews"
	"connect.xai.run/internal/secrets"
	"connect.xai.run/internal/store/memory"
)

func TestReviewedApplicationOIDCFlowAndSuspensionDeny(t *testing.T) {
	const sharedSecret = "integration-shared-secret"
	var snapshots = map[int64]identity.UserSnapshot{
		101: {DiscourseID: 101, Username: "developer", Name: "Developer", TrustLevel: 1, Active: true},
		202: {DiscourseID: 202, Username: "reviewer", Name: "Reviewer", TrustLevel: 4, Active: true, Reviewer: true},
	}
	var snapshotsMu sync.RWMutex
	fakeDiscourse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		verifier, err := identity.NewVerifier([]byte(sharedSecret), time.Minute, identity.NewMemoryNonceStore())
		if err != nil || verifier.Verify(r.Context(), r, nil) != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var id int64
		if _, err := fmt.Sscanf(r.URL.Path, "/connect/identity/users/%d", &id); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		snapshotsMu.RLock()
		snapshot, ok := snapshots[id]
		snapshotsMu.RUnlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(snapshot)
	}))
	defer fakeDiscourse.Close()

	provider, err := identity.NewClient(fakeDiscourse.URL, []byte(sharedSecret), fakeDiscourse.Client())
	if err != nil {
		t.Fatal(err)
	}
	users := memory.NewUserRepository()
	status := identity.NewStatusRefresher(provider, users)
	developer, err := identity.EnsureShadowUser(context.Background(), users, provider, 101)
	if err != nil {
		t.Fatal(err)
	}
	reviewer, err := identity.EnsureShadowUser(context.Background(), users, provider, 202)
	if err != nil {
		t.Fatal(err)
	}
	appsRepo := memory.NewApplicationRepository()
	consents := memory.NewConsentRepository()
	outbox := memory.NewOutboxRepository()
	audit := memory.NewAuditRepository()
	hydraFake := hydra.NewFake()
	box, err := secrets.NewBox([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	appService := apps.NewService(apps.Dependencies{Status: status, Apps: appsRepo, Audit: audit, Box: box, Hydra: hydraFake})
	app, err := appService.CreateDraft(context.Background(), string(developer.Subject), apps.DraftInput{
		Name: "Integration App", Description: "OIDC integration", CallbackURLs: []string{"https://client.example/callback"}, VerifiedDomains: []string{"client.example"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appService.Submit(context.Background(), string(developer.Subject), app.ID); err != nil {
		t.Fatal(err)
	}
	reviewerAccess := identity.NewAccessLookup(status, users)
	reviewService := reviews.NewService(reviews.Dependencies{Apps: appsRepo, Outbox: outbox, Audit: audit, Access: reviewerAccess})
	if err := reviewService.Approve(context.Background(), string(reviewer.Subject), app.ID, "approved by integration test"); err != nil {
		t.Fatal(err)
	}
	provisioner := apps.NewProvisioner(apps.ProvisionerDependencies{Apps: appsRepo, Outbox: outbox, Audit: audit, Hydra: hydraFake, Box: box})
	if worked, err := provisioner.RunOnce(context.Background()); err != nil || !worked {
		t.Fatalf("provision = %v/%v", worked, err)
	}
	app, err = appsRepo.Get(context.Background(), app.ID)
	if err != nil || app.Status != domain.StatusApproved || app.ClientID == "" {
		t.Fatalf("provisioned app = %#v err=%v", app, err)
	}

	hydraFake.Login = hydra.LoginRequest{
		Challenge: "login-1", Subject: "", Client: hydra.ClientInfo{ID: string(app.ClientID), RedirectURIs: []string{"https://client.example/callback"}},
		RequestedScope: []string{"openid", "profile", "community"},
		RequestURL:     "https://connect.example/oauth2/auth?client_id=" + string(app.ClientID) + "&redirect_uri=https%3A%2F%2Fclient.example%2Fcallback&response_type=code&scope=openid+profile+community&state=state-1&code_challenge=challenge-1&code_challenge_method=S256&nonce=nonce-1",
	}
	login := oauth.NewLoginService(hydraFake, status, time.Hour)
	if _, err := login.Complete(context.Background(), "login-1", string(developer.Subject)); err != nil {
		t.Fatal(err)
	}
	hydraFake.Consent = hydra.ConsentRequest{Challenge: "consent-1", Subject: string(developer.Subject), Client: hydra.ClientInfo{ID: string(app.ClientID), Name: app.Name}, RequestedScope: []string{"openid", "profile", "community"}}
	consent := oauth.NewConsentService(hydraFake, status, appsRepo, consents, time.Hour)
	if _, err := consent.Accept(context.Background(), "consent-1", string(developer.Subject), []string{"openid", "profile", "community"}, true); err != nil {
		t.Fatal(err)
	}
	hydraFake.Tokens["access-token"] = hydra.TokenIntrospection{Active: true, Subject: string(developer.Subject), ClientID: string(app.ClientID), Scope: "openid profile community"}
	userinfo := oauth.NewUserInfoService(hydraFake, status, users, appsRepo)
	claims, err := userinfo.Claims(context.Background(), "access-token")
	if err != nil || claims["sub"] != string(developer.Subject) || claims["preferred_username"] != "developer" {
		t.Fatalf("userinfo claims=%#v err=%v", claims, err)
	}
	if err := hydraFake.RevokeSession(context.Background(), string(developer.Subject), "sid-1"); err != nil {
		t.Fatal(err)
	}

	snapshotsMu.Lock()
	snapshots[101] = identity.UserSnapshot{DiscourseID: 101, Username: "developer", Name: "Developer", TrustLevel: 1, Active: true, Suspended: true}
	snapshotsMu.Unlock()
	if _, err := userinfo.Claims(context.Background(), "access-token"); err == nil {
		t.Fatal("userinfo succeeded for suspended user")
	}
}
