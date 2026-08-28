package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"connect.xai.run/internal/apps"
	"connect.xai.run/internal/assets"
	"connect.xai.run/internal/config"
	"connect.xai.run/internal/httpserver"
	"connect.xai.run/internal/hydra"
	"connect.xai.run/internal/identity"
	"connect.xai.run/internal/oauth"
	"connect.xai.run/internal/reviews"
	"connect.xai.run/internal/secrets"
	"connect.xai.run/internal/session"
	"connect.xai.run/internal/store/postgres"
	"connect.xai.run/internal/web"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	if err := run(context.Background()); err != nil {
		slog.Error("connect server stopped", "error", err)
		os.Exit(1)
	}
}

func healthcheck() int {
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://127.0.0.1:8080/readyz")
	if err != nil {
		return 1
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func run(parent context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	runtime, err := newRuntime(ctx, cfg)
	if err != nil {
		return err
	}
	defer runtime.close()

	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           runtime.handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	serverErr := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}

type runtime struct {
	postgres *postgres.Store
	redis    *redis.Client
	handler  http.Handler
	close    func()
}

func newRuntime(ctx context.Context, cfg config.Config) (*runtime, error) {
	db, err := postgres.New(ctx, cfg.PostgresDSN)
	if err != nil {
		return nil, err
	}
	redisOptions, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		db.Close()
		return nil, errors.New("REDIS_URL is invalid")
	}
	redisClient := redis.NewClient(redisOptions)
	if err := redisClient.Ping(ctx).Err(); err != nil {
		db.Close()
		_ = redisClient.Close()
		return nil, errors.New("Redis is unavailable")
	}

	box, err := secrets.NewBox(cfg.EncryptionKey)
	if err != nil {
		db.Close()
		_ = redisClient.Close()
		return nil, err
	}
	hydraAdmin, err := hydra.NewOryClient(cfg.HydraAdminURL, &http.Client{Timeout: 10 * time.Second})
	if err != nil {
		db.Close()
		_ = redisClient.Close()
		return nil, err
	}
	discourse, err := identity.NewClient(cfg.DiscourseURL, []byte(cfg.DiscourseSharedSecret), &http.Client{Timeout: 10 * time.Second})
	if err != nil {
		db.Close()
		_ = redisClient.Close()
		return nil, err
	}
	nonceStore := identity.NewRedisNonceStore(redisClient, "connect:identity:nonce:")
	verifier, err := identity.NewVerifier([]byte(cfg.DiscourseSharedSecret), 5*time.Minute, nonceStore)
	if err != nil {
		db.Close()
		_ = redisClient.Close()
		return nil, err
	}
	eventIDs := identity.NewRedisNonceStore(redisClient, "connect:identity:event:")
	status := identity.NewStatusRefresher(discourse, db)
	access := identity.NewAccessLookup(status, db)
	consents := postgres.NewConsentStore(db)
	outbox := postgres.NewOutboxStore(db)
	audit := postgres.NewAuditStore(db)
	appService := apps.NewService(apps.Dependencies{
		Status: status, Apps: db, Audit: audit, Box: box, Hydra: hydraAdmin,
	})
	provisioner := apps.NewProvisioner(apps.ProvisionerDependencies{
		Apps: db, Outbox: outbox, Audit: audit, Hydra: hydraAdmin, Box: box,
	})
	reviewService := reviews.NewService(reviews.Dependencies{
		Apps: db, Outbox: outbox, Audit: audit, Access: access,
	})
	consentService := oauth.NewConsentService(hydraAdmin, status, db, consents, cfg.RefreshIdleTTL)
	loginService := oauth.NewLoginService(hydraAdmin, status, cfg.SessionSlidingTTL)
	userinfo := oauth.NewUserInfoService(hydraAdmin, status, db, db)
	sessionStore := session.NewRedisStore(redisClient, cfg.SessionSlidingTTL, cfg.SessionAbsoluteTTL)
	sessions := session.NewHTTPHandler(sessionStore, cfg.CookieName, cfg.CookieSecure)
	csrf, err := session.NewCSRF(cfg.EncryptionKey)
	if err != nil {
		db.Close()
		_ = redisClient.Close()
		return nil, err
	}
	renderer, err := web.NewRenderer()
	if err != nil {
		db.Close()
		_ = redisClient.Close()
		return nil, err
	}
	assetStore, err := assets.NewFileStore(cfg.AssetDir)
	if err != nil {
		db.Close()
		_ = redisClient.Close()
		return nil, err
	}
	sso, err := identity.NewSSOProvider(cfg.DiscourseURL, cfg.PublicIssuerURL+"/connect/callback", []byte(cfg.DiscourseSharedSecret), 10*time.Minute)
	if err != nil {
		db.Close()
		_ = redisClient.Close()
		return nil, err
	}

	hydraPublic, err := oauth.NewHydraPublicProxy(cfg.HydraPublicURL, &http.Client{Timeout: 15 * time.Second})
	if err != nil {
		db.Close()
		_ = redisClient.Close()
		return nil, err
	}
	jwks, err := oauth.NewJWKSProxy(cfg.HydraPublicURL, &http.Client{Timeout: 15 * time.Second})
	if err != nil {
		db.Close()
		_ = redisClient.Close()
		return nil, err
	}

	identityEvents := identity.NewEventConsumer(verifier, db, eventIDs)
	appHTTP := apps.NewHTTPHandler(apps.HTTPDependencies{
		Service: appService, Status: status, Sessions: sessions, CSRF: csrf, Renderer: renderer, Assets: assetStore,
	})
	reviewHTTP := reviews.NewHTTPHandler(reviews.HTTPDependencies{
		Service: reviewService, Status: status, Sessions: sessions, CSRF: csrf, Renderer: renderer,
	})
	consentHTTP := oauth.NewConsentHTTPHandler(oauth.ConsentHTTPDependencies{
		Service: consentService, Sessions: sessions, CSRF: csrf, Renderer: renderer,
	})
	register := func(mux *http.ServeMux) {
		httpserver.RegisterOAuthRoutes(mux, httpserver.OAuthDependencies{
			Discovery: oauth.NewDiscoveryHandler(cfg.PublicIssuerURL), JWKS: jwks,
			Hydra: hydraPublic, UserInfo: userinfo,
		})
		httpserver.RegisterConnectRoutes(mux, httpserver.ConnectDependencies{
			Sessions: sessions, CSRF: csrf,
			BeginLogin: func(ctx context.Context, challenge string) (string, error) {
				if _, err := loginService.Begin(ctx, challenge); err != nil {
					return "", err
				}
				return sso.Begin(ctx, "/connect/callback", challenge)
			},
			BeginSessionLogin: func(ctx context.Context, returnTo string) (string, error) {
				return sso.Begin(ctx, returnTo, "")
			},
			CompleteLogin: func(ctx context.Context, request *http.Request) (string, string, error) {
				result, err := sso.Complete(ctx, request)
				if err != nil {
					return "", "", err
				}
				user, err := identity.EnsureShadowUser(ctx, db, discourse, result.DiscourseID)
				if err != nil {
					return "", "", err
				}
				current, err := status.CurrentStatus(ctx, string(user.Subject))
				if err != nil || !current.CanAuthenticate() {
					return "", "", identity.ErrSSOIdentityDenied
				}
				if result.Challenge != "" {
					redirect, err := loginService.Complete(ctx, result.Challenge, string(user.Subject))
					return string(user.Subject), redirect, err
				}
				return string(user.Subject), result.ReturnTo, nil
			},
		})
		oauth.RegisterConsentRoutes(mux, consentHTTP)
		apps.RegisterRoutes(mux, appHTTP)
		reviews.RegisterRoutes(mux, reviewHTTP)
		mux.Handle("/connect/identity/events", identityEvents.Handler())
		mux.Handle("/assets/", assetStore)
	}
	handler := httpserver.New(httpserver.Dependencies{
		Ready: func() error {
			readyCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := db.Ready(readyCtx); err != nil {
				return err
			}
			return redisClient.Ping(readyCtx).Err()
		},
		Register: register,
	})

	workerCtx, cancelWorker := context.WithCancel(ctx)
	go runProvisioner(workerCtx, provisioner)
	return &runtime{
		postgres: db, redis: redisClient, handler: handler,
		close: func() { cancelWorker(); db.Close(); _ = redisClient.Close() },
	}, nil
}

func runProvisioner(ctx context.Context, provisioner *apps.Provisioner) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := provisioner.RunOnce(ctx); err != nil {
				slog.Warn("application provisioning attempt failed", "error", err)
			}
		}
	}
}
