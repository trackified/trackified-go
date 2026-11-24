// Package trackified is the official Go client for the Trackified parcel-tracking API.
//
// See https://forumulate.com/p/trackified/docs for the API reference.
package trackified

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Version is the SDK version, sent in the User-Agent header.
const Version = "0.1.0"

// DefaultBaseURL is the production API endpoint.
const DefaultBaseURL = "https://api.trackified.net"

// Client talks to the Trackified API. Create one with New and reuse it; it is safe for concurrent use.
type Client struct {
	Trackings         *TrackingsService
	Carriers          *CarriersService
	Webhooks          *WebhooksService
	WebhookDeliveries *WebhookDeliveriesService
	APIKeys           *APIKeysService
	Usage             *UsageService

	apiKey     string
	baseURL    string
	httpClient *http.Client
	maxRetries int
	userAgent  string
}

// Option configures a Client.
type Option func(*Client)

// WithAPIKey sets the API key. Defaults to the TRACKIFIED_API_KEY environment variable.
func WithAPIKey(key string) Option { return func(c *Client) { c.apiKey = key } }

// WithBaseURL overrides the API base URL. Defaults to TRACKIFIED_BASE_URL or DefaultBaseURL.
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }

// WithTimeout sets the per-request timeout (default 30s).
func WithTimeout(d time.Duration) Option { return func(c *Client) { c.httpClient.Timeout = d } }

// WithMaxRetries sets how many times a failed request is retried (default 2).
func WithMaxRetries(n int) Option { return func(c *Client) { c.maxRetries = n } }

// WithHTTPClient replaces the underlying *http.Client.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

// WithUserAgent prepends a product token (e.g. "myapp/1.2") to the SDK User-Agent.
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.userAgent = ua + " " + c.userAgent }
}

// New builds a Client. It returns an error when no API key is configured.
func New(opts ...Option) (*Client, error) {
	c := &Client{
		apiKey:     os.Getenv("TRACKIFIED_API_KEY"),
		baseURL:    DefaultBaseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		maxRetries: 2,
		userAgent:  "trackified-go/" + Version,
	}
	if u := os.Getenv("TRACKIFIED_BASE_URL"); u != "" {
		c.baseURL = strings.TrimRight(u, "/")
	}
	for _, o := range opts {
		o(c)
	}
	if c.apiKey == "" {
		return nil, errors.New("trackified: missing API key (pass WithAPIKey or set TRACKIFIED_API_KEY)")
	}
	c.Trackings = &TrackingsService{c}
	c.Carriers = &CarriersService{c}
	c.Webhooks = &WebhooksService{c}
	c.WebhookDeliveries = &WebhookDeliveriesService{c}
	c.APIKeys = &APIKeysService{c}
	c.Usage = &UsageService{c}
	return c, nil
}

// sleep is swapped in tests to avoid real waits.
var sleep = func(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// do sends a request, retrying network errors, 408, 429 and 5xx, and decodes the JSON response into out.
// idemKey is sent as Idempotency-Key and reused across retries.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, idemKey string, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("User-Agent", c.userAgent)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if idemKey != "" {
			req.Header.Set("Idempotency-Key", idemKey)
		}

		resp, err := c.httpClient.Do(req)
		var apiErr error
		var wait time.Duration
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			apiErr = &ConnectionError{&TrackifiedError{Message: err.Error(), Err: err}}
		} else {
			data, rerr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if rerr != nil {
				apiErr = &ConnectionError{&TrackifiedError{Message: rerr.Error(), Err: rerr}}
			} else if resp.StatusCode < 300 {
				if out == nil || len(bytes.TrimSpace(data)) == 0 {
					return nil
				}
				return json.Unmarshal(data, out)
			} else {
				apiErr = newAPIError(resp, data)
				if !retryable(resp.StatusCode) {
					return apiErr
				}
				var rl *RateLimitError
				if errors.As(apiErr, &rl) {
					wait = rl.RetryAfter
				}
			}
		}
		if attempt >= c.maxRetries {
			return apiErr
		}
		if wait <= 0 {
			wait = backoff(attempt)
		}
		if err := sleep(ctx, wait); err != nil {
			return err
		}
	}
}

func retryable(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500
}

// backoff is 0.5s * 2^attempt, capped at 8s, with full jitter on the upper half.
func backoff(attempt int) time.Duration {
	d := min(500*time.Millisecond<<attempt, 8*time.Second)
	return d/2 + rand.N(d/2+1)
}

func newAPIError(resp *http.Response, data []byte) error {
	var env struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	_ = json.Unmarshal(data, &env)
	base := &TrackifiedError{
		StatusCode: resp.StatusCode,
		Code:       env.Error.Code,
		Message:    env.Error.Message,
		RequestID:  env.Error.RequestID,
	}
	if base.Message == "" {
		base.Message = http.StatusText(resp.StatusCode)
	}
	if base.RequestID == "" {
		base.RequestID = resp.Header.Get("X-Request-Id")
	}
	switch s := resp.StatusCode; {
	case s == 400 || s == 422:
		return &ValidationError{base}
	case s == 401:
		return &AuthenticationError{base}
	case s == 403:
		return &PermissionError{base}
	case s == 404:
		return &NotFoundError{base}
	case s == 429:
		secs, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		return &RateLimitError{TrackifiedError: base, RetryAfter: time.Duration(secs) * time.Second}
	case s >= 500:
		return &APIError{base}
	}
	return base
}

// newIdempotencyKey returns a random UUID v4.
func newIdempotencyKey() string {
	var b [16]byte
	_, _ = crand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func pathEscape(id string) string { return url.PathEscape(id) }
