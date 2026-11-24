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
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Version is the SDK version, sent in the User-Agent header.
const Version = "0.2.1"

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
	hooks      Hooks
}

// Option configures a Client.
type Option func(*Client)

// WithAPIKey sets the API key. Defaults to the TRACKIFIED_API_KEY environment variable.
func WithAPIKey(key string) Option { return func(c *Client) { c.apiKey = key } }

// WithBaseURL overrides the API base URL. Defaults to TRACKIFIED_BASE_URL or DefaultBaseURL.
// Both "https://api.trackified.net" and "https://api.trackified.net/v1" work, with or without a trailing slash.
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = normalizeBaseURL(u) } }

// normalizeBaseURL strips trailing slashes and one trailing "/v1"; request paths already start with /v1.
func normalizeBaseURL(u string) string {
	return strings.TrimRight(strings.TrimSuffix(strings.TrimRight(u, "/"), "/v1"), "/")
}

// RequestInfo is passed to Hooks.OnRequest before each attempt.
type RequestInfo struct {
	Method         string
	URL            string
	Attempt        int // 1 for the first try
	IdempotencyKey string
}

// ResponseInfo is passed to Hooks.OnResponse after each attempt.
type ResponseInfo struct {
	Method             string
	URL                string
	Attempt            int // 1 for the first try
	Status             int // 0 when the request got no response
	RequestID          string
	IdempotentReplayed bool
	Duration           time.Duration
	// Err is the typed error (e.g. *NotFoundError, *ConnectionError) when Status >= 400 or the connection failed.
	Err error
}

// Hooks observe every request attempt, e.g. for logging or metrics. A panicking hook is recovered and ignored.
type Hooks struct {
	OnRequest  func(RequestInfo)
	OnResponse func(ResponseInfo)
}

// WithHooks installs request and response hooks.
func WithHooks(h Hooks) Option { return func(c *Client) { c.hooks = h } }

// callHook runs a hook, never letting it break the request.
func callHook[T any](f func(T), v T) {
	if f == nil {
		return
	}
	defer func() { _ = recover() }()
	f(v)
}

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
		c.baseURL = normalizeBaseURL(u)
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

		callHook(c.hooks.OnRequest, RequestInfo{Method: method, URL: u, Attempt: attempt + 1, IdempotencyKey: idemKey})
		start := time.Now()
		info := ResponseInfo{Method: method, URL: u, Attempt: attempt + 1}
		resp, err := c.httpClient.Do(req)
		var apiErr error
		var wait time.Duration
		if err != nil {
			if ctx.Err() != nil {
				info.Err, info.Duration = ctx.Err(), time.Since(start)
				callHook(c.hooks.OnResponse, info)
				return ctx.Err()
			}
			apiErr = newConnectionError(err)
		} else {
			info.Status = resp.StatusCode
			info.RequestID = resp.Header.Get("X-Request-Id")
			info.IdempotentReplayed = resp.Header.Get("Idempotent-Replayed") == "true"
			data, rerr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if rerr != nil {
				apiErr = newConnectionError(rerr)
			} else if resp.StatusCode < 300 {
				info.Duration = time.Since(start)
				callHook(c.hooks.OnResponse, info)
				if out == nil || len(bytes.TrimSpace(data)) == 0 {
					return nil
				}
				return json.Unmarshal(data, out)
			} else {
				apiErr = newAPIError(resp, data)
			}
		}
		info.Err, info.Duration = apiErr, time.Since(start)
		callHook(c.hooks.OnResponse, info)
		var ce *ConnectionError
		if !errors.As(apiErr, &ce) {
			if !retryable(info.Status) {
				return apiErr
			}
			var rl *RateLimitError
			if errors.As(apiErr, &rl) {
				wait = rl.RetryAfter
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
	case s == 402:
		return &QuotaExceededError{base}
	case s == 404:
		return &NotFoundError{base}
	case s == 409:
		return &ConflictError{base}
	case s == 429:
		secs, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		return &RateLimitError{TrackifiedError: base, RetryAfter: time.Duration(secs) * time.Second}
	case s >= 500:
		return &APIError{base}
	}
	return base
}

// newConnectionError wraps a transport error; Code is "timeout" or "connection".
func newConnectionError(err error) error {
	code := "connection"
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		code = "timeout"
	}
	return &ConnectionError{&TrackifiedError{Code: code, Message: err.Error(), Err: err}}
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
