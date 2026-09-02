package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL    *url.URL
	secret     []byte
	httpClient *http.Client
	now        func() time.Time
}

func NewClient(rawBaseURL string, secret []byte, httpClient *http.Client) (*Client, error) {
	if len(secret) == 0 {
		return nil, errors.New("DiscourseConnect shared secret must not be empty")
	}
	base, err := url.Parse(strings.TrimRight(rawBaseURL, "/"))
	if err != nil || base.Scheme == "" || base.Host == "" || base.User != nil {
		return nil, errors.New("DiscourseConnect base URL must be absolute")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		baseURL: base, secret: append([]byte(nil), secret...),
		httpClient: httpClient, now: time.Now,
	}, nil
}

func (c *Client) FetchUser(ctx context.Context, discourseID int64) (UserSnapshot, error) {
	if c == nil || c.baseURL == nil || c.httpClient == nil {
		return UserSnapshot{}, errors.New("DiscourseConnect client is not initialized")
	}
	if discourseID <= 0 {
		return UserSnapshot{}, errors.New("Discourse ID must be positive")
	}
	path := "/connect/identity/users/" + strconv.FormatInt(discourseID, 10)
	var snapshot UserSnapshot
	if err := c.fetchSignedJSON(ctx, path, &snapshot, "user"); err != nil {
		return UserSnapshot{}, err
	}
	if snapshot.DiscourseID != discourseID || snapshot.Username == "" {
		return UserSnapshot{}, errors.New("DiscourseConnect returned incomplete user")
	}
	return snapshot, nil
}

func (c *Client) FetchLevelProgress(ctx context.Context, discourseID int64) (LevelProgressSnapshot, error) {
	if c == nil || c.baseURL == nil || c.httpClient == nil {
		return LevelProgressSnapshot{}, errors.New("DiscourseConnect client is not initialized")
	}
	if discourseID <= 0 {
		return LevelProgressSnapshot{}, errors.New("Discourse ID must be positive")
	}
	path := "/connect/identity/users/" + strconv.FormatInt(discourseID, 10) + "/level-progress"
	var snapshot LevelProgressSnapshot
	if err := c.fetchSignedJSON(ctx, path, &snapshot, "level progress"); err != nil {
		return LevelProgressSnapshot{}, err
	}
	if err := snapshot.ValidateFor(discourseID); err != nil {
		return LevelProgressSnapshot{}, err
	}
	return snapshot, nil
}

func (c *Client) fetchSignedJSON(ctx context.Context, path string, out any, decodeLabel string) error {
	requestURL := *c.baseURL
	requestURL.Path = strings.TrimRight(requestURL.Path, "/") + path
	requestURL.RawQuery = ""
	requestURL.Fragment = ""
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return fmt.Errorf("build DiscourseConnect request: %w", err)
	}
	timestamp := c.now().UTC()
	nonce, err := randomNonce()
	if err != nil {
		return fmt.Errorf("generate DiscourseConnect nonce: %w", err)
	}
	body := []byte{}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Connect-Timestamp", strconv.FormatInt(timestamp.Unix(), 10))
	request.Header.Set("X-Connect-Nonce", nonce)
	request.Header.Set("X-Connect-Signature", Sign(c.secret, request.Method, request.URL.RequestURI(), timestamp, nonce, body))
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("call DiscourseConnect: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("DiscourseConnect returned HTTP %d", response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("decode DiscourseConnect %s: %w", decodeLabel, err)
	}
	return nil
}

func randomNonce() (string, error) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

var _ Provider = (*Client)(nil)
