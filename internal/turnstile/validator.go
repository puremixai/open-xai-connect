package turnstile

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const siteVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

const maxTokenLength = 2048

var (
	ErrRejected    = errors.New("turnstile verification rejected")
	ErrUnavailable = errors.New("turnstile verification unavailable")
)

type Validator interface {
	Verify(context.Context, string, string) error
}

type Client struct {
	secret            string
	expectedHostnames map[string]struct{}
	httpClient        *http.Client
	endpoint          string
}

func New(secret string, hostnames []string, httpClient *http.Client) (*Client, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("Turnstile secret is required")
	}
	expected := make(map[string]struct{}, len(hostnames))
	for _, hostname := range hostnames {
		if normalized := normalizeHostname(hostname); normalized != "" {
			expected[normalized] = struct{}{}
		}
	}
	if len(expected) == 0 {
		return nil, errors.New("at least one Turnstile hostname is required")
	}
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{
		secret:            secret,
		expectedHostnames: expected,
		httpClient:        httpClient,
		endpoint:          siteVerifyURL,
	}, nil
}

func (c *Client) Verify(ctx context.Context, token, action string) error {
	if c == nil || strings.TrimSpace(c.secret) == "" || len(c.expectedHostnames) == 0 {
		return ErrUnavailable
	}
	if strings.TrimSpace(token) == "" || len(token) > maxTokenLength || strings.TrimSpace(action) == "" {
		return ErrRejected
	}
	if ctx == nil {
		ctx = context.Background()
	}
	requestContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	form := url.Values{
		"secret":   {c.secret},
		"response": {token},
	}
	request, err := http.NewRequestWithContext(requestContext, http.MethodPost, c.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return ErrUnavailable
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return ErrUnavailable
	}

	var result struct {
		Success  bool   `json:"success"`
		Action   string `json:"action"`
		Hostname string `json:"hostname"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&result); err != nil {
		return ErrUnavailable
	}
	if !result.Success || result.Action != action {
		return ErrRejected
	}
	if _, ok := c.expectedHostnames[normalizeHostname(result.Hostname)]; !ok {
		return ErrRejected
	}
	return nil
}

func normalizeHostname(value string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
}
