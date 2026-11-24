package trackified

import (
	"context"
	"iter"
	"net/http"
	"net/url"
)

// TrackingsService manages trackings.
type TrackingsService struct{ c *Client }

// CreateTrackingParams is the body of TrackingsService.Create.
type CreateTrackingParams struct {
	TrackingNumber string `json:"tracking_number"`
	// Carrier is optional; leave it empty to detect it from the number. If detection is not
	// confident the call fails with a ValidationError (code carrier_not_detected).
	Carrier       string `json:"carrier,omitempty"`
	CustomID      string `json:"custom_id,omitempty"`
	CustomerEmail string `json:"customer_email,omitempty"`
	// IdempotencyKey is sent as the Idempotency-Key header. A random one is generated when empty.
	IdempotencyKey string `json:"-"`
}

// Create adds a tracking.
func (s *TrackingsService) Create(ctx context.Context, p *CreateTrackingParams) (*Tracking, error) {
	key := p.IdempotencyKey
	if key == "" {
		key = newIdempotencyKey()
	}
	var t Tracking
	return &t, s.c.do(ctx, http.MethodPost, "/v1/trackings", nil, p, key, &t)
}

// BulkCreate adds a batch of trackings. Each row reports its own success; the call itself
// only fails for request-level errors. idempotencyKey may be empty to generate one.
func (s *TrackingsService) BulkCreate(ctx context.Context, trackings []CreateTrackingParams, idempotencyKey string) (*BulkResult, error) {
	if idempotencyKey == "" {
		idempotencyKey = newIdempotencyKey()
	}
	body := struct {
		Trackings []CreateTrackingParams `json:"trackings"`
	}{trackings}
	var r BulkResult
	return &r, s.c.do(ctx, http.MethodPost, "/v1/trackings/bulk", nil, body, idempotencyKey, &r)
}

// Get returns a tracking with its events.
func (s *TrackingsService) Get(ctx context.Context, id string) (*Tracking, error) {
	var t Tracking
	return &t, s.c.do(ctx, http.MethodGet, "/v1/trackings/"+pathEscape(id), nil, nil, "", &t)
}

// UpdateTrackingParams is the body of TrackingsService.Update. Empty fields are left unchanged.
type UpdateTrackingParams struct {
	Carrier       string `json:"carrier,omitempty"`
	CustomID      string `json:"custom_id,omitempty"`
	CustomerEmail string `json:"customer_email,omitempty"`
}

// Update changes a tracking's carrier, custom ID or customer email.
func (s *TrackingsService) Update(ctx context.Context, id string, p *UpdateTrackingParams) (*Tracking, error) {
	var t Tracking
	return &t, s.c.do(ctx, http.MethodPut, "/v1/trackings/"+pathEscape(id), nil, p, "", &t)
}

// Delete stops tracking a parcel.
func (s *TrackingsService) Delete(ctx context.Context, id string) (*DeleteResult, error) {
	var r DeleteResult
	return &r, s.c.do(ctx, http.MethodDelete, "/v1/trackings/"+pathEscape(id), nil, nil, "", &r)
}

// Retrack asks for the carrier to be checked again within a minute.
func (s *TrackingsService) Retrack(ctx context.Context, id string) (*RetrackResult, error) {
	var r RetrackResult
	return &r, s.c.do(ctx, http.MethodPost, "/v1/trackings/"+pathEscape(id)+"/retrack", nil, nil, "", &r)
}

// TrackingListParams filters TrackingsService.List.
type TrackingListParams struct {
	ListParams
	Status         TrackingStatus
	Carrier        string
	CustomID       string
	TrackingNumber string
}

func (p *TrackingListParams) query() url.Values {
	if p == nil {
		return nil
	}
	q := p.ListParams.query()
	for k, v := range map[string]string{"status": string(p.Status), "carrier": p.Carrier, "custom_id": p.CustomID, "tracking_number": p.TrackingNumber} {
		if v != "" {
			q.Set(k, v)
		}
	}
	return q
}

// List returns one page of trackings. p may be nil.
func (s *TrackingsService) List(ctx context.Context, p *TrackingListParams) (*List[Tracking], error) {
	var l List[Tracking]
	return &l, s.c.do(ctx, http.MethodGet, "/v1/trackings", p.query(), nil, "", &l)
}

// ListAutoPaging iterates over every tracking matching p, fetching pages as needed. p may be nil.
func (s *TrackingsService) ListAutoPaging(ctx context.Context, p *TrackingListParams) *Iter[Tracking] {
	cp := TrackingListParams{}
	if p != nil {
		cp = *p
	}
	return newIter(ctx, cp.Offset, func(ctx context.Context, off int) (*List[Tracking], error) {
		cp.Offset = off
		return s.List(ctx, &cp)
	})
}

// All is shorthand for ListAutoPaging(ctx, p).All().
func (s *TrackingsService) All(ctx context.Context, p *TrackingListParams) iter.Seq2[Tracking, error] {
	return s.ListAutoPaging(ctx, p).All()
}
