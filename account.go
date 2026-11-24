package trackified

import (
	"context"
	"net/http"
)

// APIKeysService manages account API keys.
type APIKeysService struct{ c *Client }

// CreateAPIKeyParams is the body of APIKeysService.Create.
type CreateAPIKeyParams struct {
	Name   string   `json:"name,omitempty"`
	Mode   string   `json:"mode,omitempty"` // test or live
	Scopes []string `json:"scopes,omitempty"`
}

// Create makes a new API key. The returned Token is shown only once.
func (s *APIKeysService) Create(ctx context.Context, p *CreateAPIKeyParams) (*APIKey, error) {
	var k APIKey
	return &k, s.c.do(ctx, http.MethodPost, "/v1/account/api-keys", nil, p, "", &k)
}

// List returns one page of API keys. p may be nil.
func (s *APIKeysService) List(ctx context.Context, p *ListParams) (*List[APIKey], error) {
	var l List[APIKey]
	return &l, s.c.do(ctx, http.MethodGet, "/v1/account/api-keys", queryOf(p), nil, "", &l)
}

// ListAutoPaging iterates over every API key.
func (s *APIKeysService) ListAutoPaging(ctx context.Context, p *ListParams) *Iter[APIKey] {
	return autoPage(ctx, p, s.List)
}

// Revoke revokes an API key.
func (s *APIKeysService) Revoke(ctx context.Context, id string) error {
	return s.c.do(ctx, http.MethodDelete, "/v1/account/api-keys/"+pathEscape(id), nil, nil, "", nil)
}

// UsageService reads account usage.
type UsageService struct{ c *Client }

// Get returns this month's usage and limits.
func (s *UsageService) Get(ctx context.Context) (*Usage, error) {
	var u Usage
	return &u, s.c.do(ctx, http.MethodGet, "/v1/account/usage", nil, nil, "", &u)
}
