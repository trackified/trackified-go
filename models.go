package trackified

import (
	"bytes"
	"encoding/json"
	"time"
)

// TrackingStatus is the overall state of a tracking.
type TrackingStatus string

const (
	StatusNotYetScanned      TrackingStatus = "not_yet_scanned"
	StatusInTransit          TrackingStatus = "in_transit"
	StatusOutForDelivery     TrackingStatus = "out_for_delivery"
	StatusAvailableForPickup TrackingStatus = "available_for_pickup"
	StatusDelivered          TrackingStatus = "delivered"
	StatusException          TrackingStatus = "exception"
	StatusReturned           TrackingStatus = "returned"
	StatusUnknown            TrackingStatus = "unknown"
)

// EventStatus is the state reported by a single carrier scan.
type EventStatus string

const (
	EventInfoReceived       EventStatus = "info_received"
	EventInTransit          EventStatus = "in_transit"
	EventOutForDelivery     EventStatus = "out_for_delivery"
	EventAvailableForPickup EventStatus = "available_for_pickup"
	EventDelivered          EventStatus = "delivered"
	EventException          EventStatus = "exception"
	EventReturned           EventStatus = "returned"
	EventUnknown            EventStatus = "unknown"
)

// WebhookEventType names a webhook event.
type WebhookEventType string

const (
	EventTypeTrackingCreated       WebhookEventType = "tracking.created"
	EventTypeTrackingUpdated       WebhookEventType = "tracking.updated"
	EventTypeTrackingStatusChanged WebhookEventType = "tracking.status_changed"
	EventTypeTrackingDelivered     WebhookEventType = "tracking.delivered"
	EventTypeTrackingException     WebhookEventType = "tracking.exception"
	EventTypeTrackingReturned      WebhookEventType = "tracking.returned"
	EventTypeTrackingExpired       WebhookEventType = "tracking.expired"
)

// TrackingEvent is one carrier scan.
type TrackingEvent struct {
	// OccurredAt is ISO 8601. With an offset when the carrier gives one (then TimeZoneKnown is true);
	// a zoneless value is the local time where the scan happened; it may also be a date alone.
	// Empty when the carrier's time couldn't be read.
	OccurredAt    string      `json:"occurred_at"`
	TimeZoneKnown bool        `json:"time_zone_known"`
	Status        EventStatus `json:"status"`
	Description   string      `json:"description"`
	Location      string      `json:"location"`
	// Deprecated: use OccurredAt. Sent until 2027-06-01, removed after.
	Date string `json:"date,omitempty"`
}

// Tracking is a tracked parcel.
type Tracking struct {
	ID              string         `json:"id"`
	TrackingNumber  string         `json:"tracking_number"`
	Carrier         string         `json:"carrier,omitempty"`
	CarrierDetected bool           `json:"carrier_detected"`
	Status          TrackingStatus `json:"status"`
	// Events are newest first (by OccurredAt; undated events keep the carrier's order after dated ones),
	// guaranteed by the API.
	Events                   []TrackingEvent `json:"events"`
	EstimatedDelivery        string          `json:"estimated_delivery,omitempty"`
	DeliveredAt              string          `json:"delivered_at,omitempty"`
	DeliveredAtTimeZoneKnown bool            `json:"delivered_at_time_zone_known"`
	DeliveryDetectedAt       *time.Time      `json:"delivery_detected_at,omitempty"`
	Exception                string          `json:"exception,omitempty"`
	ServiceLevel             string          `json:"service_level,omitempty"`
	CustomID                 string          `json:"custom_id,omitempty"`
	CustomerEmail            string          `json:"customer_email,omitempty"`
	LastScrapedAt            *time.Time      `json:"last_scraped_at,omitempty"`
	NextScrapeAt             *time.Time      `json:"next_scrape_at,omitempty"`
	CreatedAt                time.Time       `json:"created_at"`
	UpdatedAt                time.Time       `json:"updated_at"`
}

// BulkResultItem is one row of a bulk create.
type BulkResultItem struct {
	OK       bool      `json:"ok"`
	Tracking *Tracking `json:"tracking,omitempty"`
	Error    string    `json:"error,omitempty"`
}

// BulkResult is the response of TrackingsService.BulkCreate.
type BulkResult struct {
	Data []BulkResultItem `json:"data"`
}

// DeleteResult is the response of TrackingsService.Delete.
type DeleteResult struct {
	Deleted bool   `json:"deleted"`
	ID      string `json:"id"`
}

// RetrackResult is the response of TrackingsService.Retrack.
type RetrackResult struct {
	Queued   bool      `json:"queued"`
	Tracking *Tracking `json:"tracking"`
}

// Carrier is a carrier Trackified tracks.
type Carrier struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"` // decoded from display_name or displayName
	Aliases     []string `json:"aliases"`
}

// UnmarshalJSON accepts both display_name and the deprecated displayName.
func (c *Carrier) UnmarshalJSON(b []byte) error {
	type plain Carrier
	var v struct {
		plain
		DisplayNameCamel string `json:"displayName"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*c = Carrier(v.plain)
	if c.DisplayName == "" {
		c.DisplayName = v.DisplayNameCamel
	}
	return nil
}

// ResolveResult is the response of CarriersService.Resolve.
type ResolveResult struct {
	Carrier    string  `json:"carrier"`
	Name       string  `json:"name"`
	Confidence float64 `json:"confidence"`
	Matched    string  `json:"matched"` // id, alias, name, provider_slug or prefix
}

// CarrierCandidate is one guess from carrier detection.
type CarrierCandidate struct {
	Carrier    string  `json:"carrier"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

// DetectResult is the response of CarriersService.Detect.
type DetectResult struct {
	TrackingNumber string             `json:"tracking_number"`
	Candidates     []CarrierCandidate `json:"candidates"`
}

// CatalogCarrier is an entry in the carrier catalog.
type CatalogCarrier struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"displayName"`
	Country     string   `json:"country,omitempty"`
	URL         string   `json:"url,omitempty"`
	Sources     []string `json:"sources,omitempty"`
	Support     string   `json:"support"` // scraper, api or unsupported
	CarrierID   string   `json:"carrierId,omitempty"`
}

// CatalogResult is the response of CarriersService.Catalog.
type CatalogResult struct {
	Total  int              `json:"total"`
	Counts map[string]int   `json:"counts,omitempty"`
	Data   []CatalogCarrier `json:"data"`
}

// CarrierValidation is how one carrier has been validated.
type CarrierValidation struct {
	Carrier        string `json:"carrier"`
	Status         string `json:"status"`
	Sample         string `json:"sample,omitempty"`
	SampleSource   string `json:"sample_source,omitempty"`
	Result         string `json:"result,omitempty"`
	EfficiencyPath string `json:"efficiency_path,omitempty"`
}

// CarrierValidationStatus is the response of CarriersService.ValidationStatus.
type CarrierValidationStatus struct {
	Counts map[string]int      `json:"counts"`
	Values []CarrierValidation `json:"values"`
}

// Webhook is a webhook endpoint.
type Webhook struct {
	ID            string             `json:"id"`
	URL           string             `json:"url"`
	EventTypes    []WebhookEventType `json:"event_types"`
	Enabled       bool               `json:"enabled"`
	SecretPreview string             `json:"secret_preview,omitempty"`
	Secret        string             `json:"secret,omitempty"` // only in the create and rotate-secret responses
	// PreviousSecretExpiresAt is set by RotateSecret: until then deliveries are signed with both secrets.
	PreviousSecretExpiresAt *time.Time `json:"previous_secret_expires_at,omitempty"`
	ConsecutiveFailures     int        `json:"consecutive_failures,omitempty"`
	DisabledAt              *time.Time `json:"disabled_at,omitempty"`
	CreatedAt               time.Time  `json:"created_at"`
}

// WebhookTestResult is a signed sample event (not sent anywhere).
type WebhookTestResult struct {
	Signature string         `json:"signature"`
	Body      map[string]any `json:"body"`
	// RawBody is the exact body that Signature signs (key order kept); pass it to VerifyWebhook.
	// Re-encoding Body sorts its keys and no longer verifies.
	RawBody json.RawMessage `json:"-"`
}

// UnmarshalJSON fills RawBody alongside Body.
func (r *WebhookTestResult) UnmarshalJSON(b []byte) error {
	type plain WebhookTestResult
	var v struct {
		plain
		Body json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*r = WebhookTestResult(v.plain)
	if len(v.Body) == 0 {
		return nil
	}
	// The API pretty-prints responses but signs the compact body; compacting keeps key order.
	var buf bytes.Buffer
	if err := json.Compact(&buf, v.Body); err != nil {
		return err
	}
	r.RawBody = buf.Bytes()
	return json.Unmarshal(v.Body, &r.Body)
}

// WebhookDelivery is one delivery attempt of a webhook event.
type WebhookDelivery struct {
	ID          string           `json:"id"`
	WebhookID   string           `json:"webhook_id"`
	URL         string           `json:"url"`
	EventType   WebhookEventType `json:"event_type"`
	Status      int              `json:"status"` // HTTP status your endpoint returned; 0 if none
	Attempts    int              `json:"attempts"`
	Error       string           `json:"error,omitempty"`
	DeliveredAt *time.Time       `json:"delivered_at,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
}

// APIKey is an account API key.
type APIKey struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Mode       string     `json:"mode"` // test or live
	Scopes     []string   `json:"scopes"`
	Token      string     `json:"token,omitempty"` // only in the create response
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

// CarrierVolume is the number of trackings for one carrier this period.
type CarrierVolume struct {
	Carrier string `json:"carrier"`
	Count   int    `json:"count"`
}

// Usage is this billing period's usage and limits.
type Usage struct {
	PeriodStart        time.Time       `json:"period_start"`
	PeriodEnd          time.Time       `json:"period_end"`
	Plan               string          `json:"plan"`
	TrackingsUsed      int             `json:"trackings_used"`
	TrackingsLimit     int             `json:"trackings_limit"`
	TrackingsRemaining int             `json:"trackings_remaining"`
	RateLimitPerMinute int             `json:"rate_limit_per_minute"`
	WebhookDeliveries  int             `json:"webhook_deliveries"`
	CarrierVolume      []CarrierVolume `json:"carrier_volume"`
}

// Pagination describes one page of a list.
type Pagination struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Total  int `json:"total"`
}

// List is one page of results.
type List[T any] struct {
	Data       []T        `json:"data"`
	Pagination Pagination `json:"pagination"`
}
