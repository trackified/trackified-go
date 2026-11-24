package trackified

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

// UpdateWebhookParams is the body of WebhooksService.Update. Set at least one field; nil/empty fields are unchanged.
type UpdateWebhookParams struct {
	URL        string             `json:"url,omitempty"`
	EventTypes []WebhookEventType `json:"event_types,omitempty"`
	Enabled    *bool              `json:"enabled,omitempty"` // a disabled webhook receives nothing
}

// Update changes a webhook's URL, event types or enabled flag. The secret is unchanged.
func (s *WebhooksService) Update(ctx context.Context, id string, p *UpdateWebhookParams) (*Webhook, error) {
	var w Webhook
	return &w, s.c.do(ctx, http.MethodPatch, "/v1/webhooks/"+pathEscape(id), nil, p, "", &w)
}

// RotateSecretParams is the body of WebhooksService.RotateSecret.
type RotateSecretParams struct {
	// ExpiresInHours is how long the old secret keeps signing deliveries (0-168). nil means the server
	// default of 24; 0 drops the old secret at once.
	ExpiresInHours *int `json:"expires_in_hours,omitempty"`
}

// RotateSecret issues a new signing secret, returned once in Webhook.Secret. Until
// Webhook.PreviousSecretExpiresAt, deliveries carry signatures from both secrets. p may be nil.
func (s *WebhooksService) RotateSecret(ctx context.Context, id string, p *RotateSecretParams) (*Webhook, error) {
	if p == nil {
		p = &RotateSecretParams{}
	}
	var w Webhook
	return &w, s.c.do(ctx, http.MethodPost, "/v1/webhooks/"+pathEscape(id)+"/rotate-secret", nil, p, "", &w)
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

// VerifyDetailed is the package-level VerifyDetailed, available on the client for discoverability.
func (s *WebhooksService) VerifyDetailed(body []byte, header string, secrets []string, tolerance ...time.Duration) VerifyResult {
	return VerifyDetailed(body, header, secrets, tolerance...)
}

// ConstructEvent is the package-level ConstructEvent, available on the client for discoverability.
func (s *WebhooksService) ConstructEvent(body []byte, header, secret string, tolerance ...time.Duration) (*WebhookEvent, error) {
	return ConstructEvent(body, header, secret, tolerance...)
}

// TrackingOf is the package-level TrackingOf, available on the client for discoverability.
func (s *WebhooksService) TrackingOf(e *WebhookEvent) (*Tracking, error) { return TrackingOf(e) }

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

// ErrInvalidSignature matches every *SignatureVerificationError with errors.Is.
var ErrInvalidSignature = errors.New("trackified: invalid webhook signature")

// Reasons a webhook signature is rejected, in VerifyResult.Reason and SignatureVerificationError.Reason.
const (
	ReasonMissing   = "missing"   // empty header or no secret
	ReasonMalformed = "malformed" // no t or v1, non-numeric t, or bad hex
	ReasonTimestamp = "timestamp" // outside the tolerance
	ReasonMismatch  = "mismatch"  // no v1 matches any secret
)

// SignatureVerificationError is returned by ConstructEvent when verification fails.
type SignatureVerificationError struct {
	Reason string // one of the Reason constants
}

func (e *SignatureVerificationError) Error() string {
	return ErrInvalidSignature.Error() + " (" + e.Reason + ")"
}

// Is makes errors.Is(err, ErrInvalidSignature) true.
func (e *SignatureVerificationError) Is(target error) bool { return target == ErrInvalidSignature }

// VerifyResult is the outcome of VerifyDetailed. Reason is empty when Valid.
type VerifyResult struct {
	Valid  bool
	Reason string
}

// WebhookEvent is a webhook payload.
type WebhookEvent struct {
	ID        string           `json:"id"`
	Type      WebhookEventType `json:"type"`
	CreatedAt time.Time        `json:"created_at"`
	// Data is the raw event object: a StatusChange for tracking.status_changed, a Tracking for every
	// other type. Decode it with StatusChange(), Tracking() or TrackingOf.
	Data json.RawMessage `json:"data"`
}

// StatusChange is the data of a tracking.status_changed event.
type StatusChange struct {
	Tracking       Tracking       `json:"tracking"`
	PreviousStatus TrackingStatus `json:"previous_status"`
	CurrentStatus  TrackingStatus `json:"current_status"`
}

// Tracking decodes Data as a Tracking. It fails for tracking.status_changed; use StatusChange or TrackingOf.
func (e *WebhookEvent) Tracking() (*Tracking, error) {
	if e.Type == EventTypeTrackingStatusChanged {
		return nil, fmt.Errorf("trackified: %s event data is a StatusChange, not a Tracking", e.Type)
	}
	var t Tracking
	if err := json.Unmarshal(e.Data, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// StatusChange decodes Data of a tracking.status_changed event. It fails for any other type.
func (e *WebhookEvent) StatusChange() (*StatusChange, error) {
	if e.Type != EventTypeTrackingStatusChanged {
		return nil, fmt.Errorf("trackified: %s event data is a Tracking, not a StatusChange", e.Type)
	}
	var sc StatusChange
	if err := json.Unmarshal(e.Data, &sc); err != nil {
		return nil, err
	}
	return &sc, nil
}

// TrackingOf returns the Tracking of any event type.
func TrackingOf(e *WebhookEvent) (*Tracking, error) {
	if e.Type == EventTypeTrackingStatusChanged {
		sc, err := e.StatusChange()
		if err != nil {
			return nil, err
		}
		return &sc.Tracking, nil
	}
	return e.Tracking()
}

// now is swapped in tests to pin the clock.
var now = time.Now

// VerifyWebhook reports whether header ("t=<unix>,v1=<hex>[,v1=<hex>...]") is a valid signature of the
// raw body with secret, and the timestamp is within tolerance (default DefaultTolerance) of now.
func VerifyWebhook(body []byte, header, secret string, tolerance ...time.Duration) bool {
	return VerifyDetailed(body, header, []string{secret}, tolerance...).Valid
}

// VerifyDetailed is VerifyWebhook with the reason for a rejection. It accepts several secrets (e.g. the
// old and new one while rotating) and is valid if any v1 signature matches any secret.
func VerifyDetailed(body []byte, header string, secrets []string, tolerance ...time.Duration) VerifyResult {
	tol := DefaultTolerance
	if len(tolerance) > 0 {
		tol = tolerance[0]
	}
	var keys []string
	for _, k := range secrets {
		if k != "" {
			keys = append(keys, k)
		}
	}
	if strings.TrimSpace(header) == "" || len(keys) == 0 {
		return VerifyResult{Reason: ReasonMissing}
	}
	var ts string
	var sigs [][]byte
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			return VerifyResult{Reason: ReasonMalformed}
		}
		switch k {
		case "t":
			ts = v
		case "v1":
			b, err := hex.DecodeString(v)
			if err != nil {
				return VerifyResult{Reason: ReasonMalformed}
			}
			sigs = append(sigs, b)
		}
	}
	t, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || len(sigs) == 0 {
		return VerifyResult{Reason: ReasonMalformed}
	}
	if d := now().Sub(time.Unix(t, 0)); d > tol || d < -tol {
		return VerifyResult{Reason: ReasonTimestamp}
	}
	ok := false
	for _, key := range keys {
		mac := hmac.New(sha256.New, []byte(key))
		mac.Write([]byte(ts + "."))
		mac.Write(body)
		want := mac.Sum(nil)
		for _, s := range sigs {
			// no early exit, so timing doesn't reveal which signature matched
			ok = hmac.Equal(s, want) || ok
		}
	}
	if !ok {
		return VerifyResult{Reason: ReasonMismatch}
	}
	return VerifyResult{Valid: true}
}

// ConstructEvent verifies the signature and parses the body. It returns a *SignatureVerificationError
// (matching errors.Is(err, ErrInvalidSignature)) when verification fails.
func ConstructEvent(body []byte, header, secret string, tolerance ...time.Duration) (*WebhookEvent, error) {
	return ConstructEventSecrets(body, header, []string{secret}, tolerance...)
}

// ConstructEventSecrets is ConstructEvent accepting several secrets, e.g. during a secret rotation.
func ConstructEventSecrets(body []byte, header string, secrets []string, tolerance ...time.Duration) (*WebhookEvent, error) {
	if r := VerifyDetailed(body, header, secrets, tolerance...); !r.Valid {
		return nil, &SignatureVerificationError{Reason: r.Reason}
	}
	var e WebhookEvent
	if err := json.Unmarshal(body, &e); err != nil {
		return nil, err
	}
	return &e, nil
}
