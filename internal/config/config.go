package config

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config contains the runtime settings shared by the Portal components.
// Sensitive values are kept as byte slices or strings and must never be
// included in logs or error messages.
type Config struct {
	ListenAddr            string
	PublicIssuerURL       string
	DiscourseURL          string
	DiscourseSharedSecret string
	TurnstileSiteKey      string
	TurnstileSecret       string
	TurnstileHostnames    []string
	PostgresDSN           string
	RedisURL              string
	HydraPublicURL        string
	HydraAdminURL         string
	AssetDir              string
	EncryptionKey         []byte
	CookieSecure          bool
	CookieName            string
	Environment           string

	AuthCodeTTL        time.Duration
	IDTokenTTL         time.Duration
	AccessTokenTTL     time.Duration
	RefreshIdleTTL     time.Duration
	RefreshAbsoluteTTL time.Duration
	SessionSlidingTTL  time.Duration
	SessionAbsoluteTTL time.Duration
}

// Load reads and validates the process environment.
func Load() (Config, error) {
	var cfg Config
	cfg.ListenAddr = envOr("CONNECT_LISTEN_ADDR", ":8080")
	cfg.CookieName = envOr("CONNECT_COOKIE_NAME", "__Host-connect_session")
	cfg.Environment = envOr("CONNECT_ENV", "production")
	cfg.AssetDir = envOr("CONNECT_ASSET_DIR", "/var/lib/connect/assets")

	var err error
	if cfg.PublicIssuerURL, err = requiredURL("CONNECT_ISSUER_URL", true); err != nil {
		return Config{}, err
	}
	if cfg.DiscourseURL, err = requiredURL("DISCOURSE_URL", true); err != nil {
		return Config{}, err
	}
	if cfg.DiscourseSharedSecret, err = requiredEnv("DISCOURSE_SHARED_SECRET"); err != nil {
		return Config{}, err
	}
	if cfg.TurnstileSiteKey, err = requiredEnv("TURNSTILE_SITE_KEY"); err != nil {
		return Config{}, err
	}
	if cfg.TurnstileSecret, err = requiredEnv("TURNSTILE_SECRET"); err != nil {
		return Config{}, err
	}
	if cfg.TurnstileHostnames, err = requiredHostnames("TURNSTILE_HOSTNAMES", cfg.Environment == "production"); err != nil {
		return Config{}, err
	}
	if cfg.PostgresDSN, err = requiredEnv("POSTGRES_DSN"); err != nil {
		return Config{}, err
	}
	if cfg.RedisURL, err = requiredEnv("REDIS_URL"); err != nil {
		return Config{}, err
	}
	if cfg.HydraPublicURL, err = requiredURL("HYDRA_PUBLIC_URL", false); err != nil {
		return Config{}, err
	}
	if cfg.HydraAdminURL, err = requiredURL("HYDRA_ADMIN_URL", false); err != nil {
		return Config{}, err
	}
	rawKey, err := requiredEnv("CONNECT_ENCRYPTION_KEY")
	if err != nil {
		return Config{}, err
	}
	if cfg.EncryptionKey, err = decodeEncryptionKey(rawKey); err != nil {
		return Config{}, fmt.Errorf("CONNECT_ENCRYPTION_KEY: %w", err)
	}
	if cfg.CookieSecure, err = boolEnv("CONNECT_COOKIE_SECURE", cfg.Environment != "development" && cfg.Environment != "test"); err != nil {
		return Config{}, err
	}
	if cfg.CookieName == "" {
		return Config{}, errors.New("CONNECT_COOKIE_NAME must not be empty")
	}
	if strings.ContainsAny(cfg.CookieName, "=;\r\n") {
		return Config{}, errors.New("CONNECT_COOKIE_NAME contains invalid characters")
	}
	if cfg.AssetDir == "" {
		return Config{}, errors.New("CONNECT_ASSET_DIR must not be empty")
	}

	cfg.AuthCodeTTL = durationEnv("CONNECT_AUTH_CODE_TTL", 10*time.Minute)
	cfg.IDTokenTTL = durationEnv("CONNECT_ID_TOKEN_TTL", time.Hour)
	cfg.AccessTokenTTL = durationEnv("CONNECT_ACCESS_TOKEN_TTL", 24*time.Hour)
	cfg.RefreshIdleTTL = durationEnv("CONNECT_REFRESH_IDLE_TTL", 180*24*time.Hour)
	cfg.RefreshAbsoluteTTL = durationEnv("CONNECT_REFRESH_ABSOLUTE_TTL", 365*24*time.Hour)
	cfg.SessionSlidingTTL = durationEnv("CONNECT_SESSION_SLIDING_TTL", 30*24*time.Hour)
	cfg.SessionAbsoluteTTL = durationEnv("CONNECT_SESSION_ABSOLUTE_TTL", 90*24*time.Hour)
	if cfg.AuthCodeTTL <= 0 || cfg.IDTokenTTL <= 0 || cfg.AccessTokenTTL <= 0 ||
		cfg.RefreshIdleTTL <= 0 || cfg.RefreshAbsoluteTTL <= 0 ||
		cfg.SessionSlidingTTL <= 0 || cfg.SessionAbsoluteTTL <= 0 {
		return Config{}, errors.New("token and session TTLs must be positive")
	}
	return cfg, nil
}

func requiredEnv(key string) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func requiredHostnames(key string, rejectLocal bool) ([]string, error) {
	raw, err := requiredEnv(key)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	hostnames := make([]string, 0)
	for _, item := range strings.Split(raw, ",") {
		hostname := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(item), "."))
		if hostname == "" {
			continue
		}
		if strings.ContainsAny(hostname, "/\\:*?\"<>|") || strings.ContainsAny(hostname, "\t\r\n") {
			return nil, fmt.Errorf("%s contains an invalid hostname", key)
		}
		if rejectLocal && (hostname == "localhost" || hostname == "127.0.0.1") {
			return nil, fmt.Errorf("%s cannot include local hostnames in production", key)
		}
		if _, ok := seen[hostname]; ok {
			continue
		}
		seen[hostname] = struct{}{}
		hostnames = append(hostnames, hostname)
	}
	if len(hostnames) == 0 {
		return nil, fmt.Errorf("%s must contain at least one hostname", key)
	}
	return hostnames, nil
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func requiredURL(key string, requireHTTPS bool) (string, error) {
	value, err := requiredEnv(key)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%s must be an absolute URL without credentials, query, or fragment", key)
	}
	if requireHTTPS && parsed.Scheme != "https" {
		return "", fmt.Errorf("%s must use https", key)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("%s must use http or https", key)
	}
	return strings.TrimRight(value, "/"), nil
}

func decodeEncryptionKey(raw string) ([]byte, error) {
	decoders := []func(string) ([]byte, error){
		base64.StdEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
		hex.DecodeString,
	}
	for _, decode := range decoders {
		if key, err := decode(raw); err == nil && len(key) == 32 {
			return key, nil
		}
	}
	if len(raw) == 32 {
		return []byte(raw), nil
	}
	return nil, errors.New("must decode to exactly 32 bytes")
}

func boolEnv(key string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", key)
	}
	return value, nil
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return -1
	}
	return value
}
