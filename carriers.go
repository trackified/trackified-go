package trackified

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// CarriersService reads carriers and detects them from tracking numbers.
type CarriersService struct{ c *Client }

// List returns one page of the carriers Trackified tracks. p may be nil.
func (s *CarriersService) List(ctx context.Context, p *ListParams) (*List[Carrier], error) {
	var l List[Carrier]
	return &l, s.c.do(ctx, http.MethodGet, "/v1/carriers", queryOf(p), nil, "", &l)
}

// ListAutoPaging iterates over every carrier.
func (s *CarriersService) ListAutoPaging(ctx context.Context, p *ListParams) *Iter[Carrier] {
	return autoPage(ctx, p, s.List)
}

// Get returns one carrier.
func (s *CarriersService) Get(ctx context.Context, id string) (*Carrier, error) {
	var c Carrier
	return &c, s.c.do(ctx, http.MethodGet, "/v1/carriers/"+pathEscape(id), nil, nil, "", &c)
}

// Detect guesses the carrier of a tracking number, best candidate first.
func (s *CarriersService) Detect(ctx context.Context, trackingNumber string) (*DetectResult, error) {
	var r DetectResult
	return &r, s.c.do(ctx, http.MethodGet, "/v1/carriers/detect", url.Values{"number": {trackingNumber}}, nil, "", &r)
}

// Resolve maps a free-text carrier name ("USPS Priority Mail", "dhl_express", "Correos de España") to a
// carrier Trackified serves. It returns nil, nil when nothing we serve matches.
func (s *CarriersService) Resolve(ctx context.Context, name string) (*ResolveResult, error) {
	var r ResolveResult
	if err := s.c.do(ctx, http.MethodGet, "/v1/carriers/resolve", url.Values{"name": {name}}, nil, "", &r); err != nil {
		return nil, err
	}
	if r.Carrier == "" {
		return nil, nil
	}
	return &r, nil
}

// CatalogParams filters CarriersService.Catalog.
type CatalogParams struct {
	Query   string // name search
	Support string // scraper, api or unsupported
	Limit   int    // 1..200, server default 50
}

// Catalog searches the full carrier catalog by name.
func (s *CarriersService) Catalog(ctx context.Context, p *CatalogParams) (*CatalogResult, error) {
	q := url.Values{}
	if p != nil {
		if p.Query != "" {
			q.Set("q", p.Query)
		}
		if p.Support != "" {
			q.Set("support", p.Support)
		}
		if p.Limit > 0 {
			q.Set("limit", strconv.Itoa(p.Limit))
		}
	}
	var r CatalogResult
	return &r, s.c.do(ctx, http.MethodGet, "/v1/carriers/catalog", q, nil, "", &r)
}

// ValidationStatus reports how each carrier has been validated.
func (s *CarriersService) ValidationStatus(ctx context.Context) (*CarrierValidationStatus, error) {
	var r CarrierValidationStatus
	return &r, s.c.do(ctx, http.MethodGet, "/v1/carriers/status", nil, nil, "", &r)
}

func queryOf(p *ListParams) url.Values {
	if p == nil {
		return nil
	}
	return p.query()
}

func autoPage[T any](ctx context.Context, p *ListParams, list func(context.Context, *ListParams) (*List[T], error)) *Iter[T] {
	cp := ListParams{}
	if p != nil {
		cp = *p
	}
	return newIter(ctx, cp.Offset, func(ctx context.Context, off int) (*List[T], error) {
		cp.Offset = off
		return list(ctx, &cp)
	})
}
