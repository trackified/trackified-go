package trackified

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// WebhooksService manages webhook endpoints.
type WebhooksService struct{ c *Client }

// CreateWebhookParams is the body of WebhooksService.Create.
type CreateWebhookParams struct {
	URL        string             `json:"url"`
	EventTypes []WebhookEventType `json:"event_types,omitempty"` // empty = all events
}

// Create adds a webhook endpoint. The returned Secret is shown only once; store it to verify signatures.
func (s *WebhooksService) Create(ctx context.Context, p *CreateWebhookParams) (*Webhook, error) {
	var w Webhook
	return &w, s.c.do(ctx, http.MethodPost, "/v1/webhooks", nil, p, "", &w)
}

// List returns one page of webhook endpoints. p may be nil.
func (s *WebhooksService) List(ctx context.Context, p *ListParams) (*List[Webhook], error) {
	var l List[Webhook]
	return &l, s.c.do(ctx, http.MethodGet, "/v1/webhooks", queryOf(p), nil, "", &l)
}

// ListAutoPaging iterates over every webhook endpoint.
func (s *WebhooksService) ListAutoPaging(ctx context.Context, p *ListParams) *Iter[Webhook] {
	return autoPage(ctx, p, s.List)
}

// Delete removes a webhook endpoint.
func (s *WebhooksService) Delete(ctx context.Context, id string) error {
	return s.c.do(ctx, http.MethodDelete, "/v1/webhooks/"+pathEscape(id), nil, nil, "", nil)
}

// Test signs a sample event with the endpoint's secret without sending it.
func (s *WebhooksService) Test(ctx context.Context, id string) (*WebhookTestResult, error) {
	var r WebhookTestResult
	return &r, s.c.do(ctx, http.MethodPost, "/v1/webhooks/"+pathEscape(id)+"/test", nil, nil, "", &r)
}

// Verify is VerifyWebhook, available on the client for discoverability.
func (s *WebhooksService) Verify(body []byte, header, secret string, tolerance ...time.Duration) bool {
	return VerifyWebhook(body, header, secret, tolerance...)
}

// ConstructEvent is the package-level ConstructEvent, available on the client for discoverability.
func (s *WebhooksService) ConstructEvent(body []byte, header, secret string, tolerance ...time.Duration) (*WebhookEvent, error) {
	return ConstructEvent(body, header, secret, tolerance...)
}

// WebhookDeliveriesService lists webhook delivery attempts.
type WebhookDeliveriesService struct{ c *Client }

// List returns one page of recent delivery attempts. p may be nil.
func (s *WebhookDeliveriesService) List(ctx context.Context, p *ListParams) (*List[WebhookDelivery], error) {
	var l List[WebhookDelivery]
	return &l, s.c.do(ctx, http.MethodGet, "/v1/webhook-deliveries", queryOf(p), nil, "", &l)
}

// ListAutoPaging iterates over every delivery attempt.
func (s *WebhookDeliveriesService) ListAutoPaging(ctx context.Context, p *ListParams) *Iter[WebhookDelivery] {
	return autoPage(ctx, p, s.List)
}

// SignatureHeader is the header carrying the webhook signature.
const SignatureHeader = "x-tracking-signature"

// DefaultTolerance is how far a signature's timestamp may be from now.
const DefaultTolerance = 300 * time.Second

// ErrInvalidSignature is returned by ConstructEvent when the signature does not verify.
var ErrInvalidSignature = errors.New("trackified: invalid webhook signature")

// WebhookEvent is a webhook payload.
type WebhookEvent struct {
	ID        string           `json:"id"`
	Type      WebhookEventType `json:"type"`
	CreatedAt time.Time        `json:"created_at"`
	// Data is the event's object, usually a Tracking; decode it with json.Unmarshal or Tracking().
	Data json.RawMessage `json:"data"`
}

// Tracking decodes Data as a Tracking.
func (e *WebhookEvent) Tracking() (*Tracking, error) {
	var t Tracking
	return &t, json.Unmarshal(e.Data, &t)
}

// now is swapped in tests to pin the clock.
var now = time.Now

// VerifyWebhook reports whether header ("t=<unix>,v1=<hex>") is a valid signature of the raw body
// with secret, and the timestamp is within tolerance (default DefaultTolerance) of now.
func VerifyWebhook(body []byte, header, secret string, tolerance ...time.Duration) bool {
	tol := DefaultTolerance
	if len(tolerance) > 0 {
		tol = tolerance[0]
	}
	var ts string
	var sigs [][]byte
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			return false
		}
		switch k {
		case "t":
			ts = v
		case "v1":
			if b, err := hex.DecodeString(v); err == nil {
				sigs = append(sigs, b)
			}
		}
	}
	t, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || len(sigs) == 0 {
		return false
	}
	if d := now().Sub(time.Unix(t, 0)); d > tol || d < -tol {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	want := mac.Sum(nil)
	for _, s := range sigs {
		if hmac.Equal(s, want) {
			return true
		}
	}
	return false
}

// ConstructEvent verifies the signature and parses the body. It returns ErrInvalidSignature
// when verification fails.
func ConstructEvent(body []byte, header, secret string, tolerance ...time.Duration) (*WebhookEvent, error) {
	if !VerifyWebhook(body, header, secret, tolerance...) {
		return nil, ErrInvalidSignature
	}
	var e WebhookEvent
	if err := json.Unmarshal(body, &e); err != nil {
		return nil, err
	}
	return &e, nil
}
