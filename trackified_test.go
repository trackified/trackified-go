package trackified

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"
)

const (
	vecSecret = "whsec_trackified_test_vector"
	vecBody   = `{"id":"evt_test","type":"tracking.updated","data":{"id":"trk_123"}}`
	vecHeader = "t=1792560000,v1=19752dd2fca1872cc8aa6cc36c412fce3bbfa120480580bdb1ad9d7957dfc9f4"
)

func pinClock(t *testing.T, at time.Time) {
	old := now
	now = func() time.Time { return at }
	t.Cleanup(func() { now = old })
}

func TestWebhookVector(t *testing.T) {
	pinClock(t, time.Unix(1792560000, 0))
	if !VerifyWebhook([]byte(vecBody), vecHeader, vecSecret) {
		t.Fatal("test vector should verify")
	}
	ev, err := ConstructEvent([]byte(vecBody), vecHeader, vecSecret)
	if err != nil || ev.ID != "evt_test" || ev.Type != EventTypeTrackingUpdated {
		t.Fatalf("ConstructEvent = %+v, %v", ev, err)
	}
	if tr, err := ev.Tracking(); err != nil || tr.ID != "trk_123" {
		t.Fatalf("Tracking() = %+v, %v", tr, err)
	}

	flipped := vecHeader[:len(vecHeader)-1] + "5"
	cases := map[string]struct{ body, header string }{
		"flipped hex digit": {vecBody, flipped},
		"different body":    {vecBody + " ", vecHeader},
		"malformed header":  {vecBody, "garbage"},
		"missing v1":        {vecBody, "t=1792560000"},
		"empty":             {vecBody, ""},
	}
	for name, c := range cases {
		if VerifyWebhook([]byte(c.body), c.header, vecSecret) {
			t.Errorf("%s: should be rejected", name)
		}
	}
	if _, err := ConstructEvent([]byte(vecBody), flipped, vecSecret); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("ConstructEvent flipped: %v", err)
	}

	pinClock(t, time.Unix(1792560000+301, 0))
	if VerifyWebhook([]byte(vecBody), vecHeader, vecSecret) {
		t.Error("outside tolerance should be rejected")
	}
	if !VerifyWebhook([]byte(vecBody), vecHeader, vecSecret, 10*time.Minute) {
		t.Error("custom tolerance should accept")
	}
}

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	old := sleep
	sleep = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { sleep = old })
	c, err := New(WithAPIKey("tk_test_x"), WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCreateRetriesWithSameIdempotencyKey(t *testing.T) {
	var mu sync.Mutex
	var keys []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer tk_test_x" || r.Header.Get("User-Agent") != "trackified-go/"+Version {
			t.Errorf("bad headers: %v", r.Header)
		}
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		if len(keys) < 3 {
			w.WriteHeader(503)
			return
		}
		var in CreateTrackingParams
		_ = json.NewDecoder(r.Body).Decode(&in)
		w.WriteHeader(201)
		fmt.Fprintf(w, `{"id":"trk_1","tracking_number":%q,"status":"in_transit","created_at":"2026-11-24T04:11:08.920Z"}`, in.TrackingNumber)
	})
	tr, err := c.Trackings.Create(context.Background(), &CreateTrackingParams{TrackingNumber: "123"})
	if err != nil || tr.ID != "trk_1" || tr.TrackingNumber != "123" || tr.Status != StatusInTransit {
		t.Fatalf("Create = %+v, %v", tr, err)
	}
	if len(keys) != 3 || keys[0] == "" || keys[0] != keys[1] || keys[1] != keys[2] || len(keys[0]) != 36 {
		t.Fatalf("idempotency keys = %q", keys)
	}
}

func TestRateLimitHonoursRetryAfter(t *testing.T) {
	var waits []time.Duration
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"code":"rate_limited","message":"slow down","request_id":"req_1"}}`))
	})
	sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	_, err := c.Usage.Get(context.Background())
	var rl *RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter != 7*time.Second || rl.RequestID != "req_1" || rl.StatusCode != 429 {
		t.Fatalf("err = %#v", err)
	}
	if calls != 3 || len(waits) != 2 || waits[0] != 7*time.Second {
		t.Fatalf("calls=%d waits=%v", calls, waits)
	}
}

func TestErrorsAreTypedAndNotRetried(t *testing.T) {
	for status, check := range map[int]func(error) bool{
		400: func(e error) bool { var x *ValidationError; return errors.As(e, &x) },
		401: func(e error) bool { var x *AuthenticationError; return errors.As(e, &x) },
		403: func(e error) bool { var x *PermissionError; return errors.As(e, &x) },
		404: func(e error) bool { var x *NotFoundError; return errors.As(e, &x) },
		422: func(e error) bool { var x *ValidationError; return errors.As(e, &x) },
	} {
		calls := 0
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"code":"c` + strconv.Itoa(status) + `","message":"m","request_id":"r"}}`))
		})
		_, err := c.Trackings.Get(context.Background(), "trk_x")
		var base *TrackifiedError
		if !check(err) || !errors.As(err, &base) || base.Code != "c"+strconv.Itoa(status) || base.StatusCode != status {
			t.Errorf("%d: err = %v", status, err)
		}
		if calls != 1 {
			t.Errorf("%d: retried %d times", status, calls-1)
		}
	}
}

func TestServerErrorAndConnectionError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	_, err := c.Usage.Get(context.Background())
	var api *APIError
	if !errors.As(err, &api) || api.StatusCode != 500 {
		t.Fatalf("err = %v", err)
	}
	c2, _ := New(WithAPIKey("k"), WithBaseURL("http://127.0.0.1:1"), WithMaxRetries(0))
	_, err = c2.Usage.Get(context.Background())
	var ce *ConnectionError
	if !errors.As(err, &ce) || ce.Err == nil {
		t.Fatalf("err = %v", err)
	}
}

func TestMissingAPIKey(t *testing.T) {
	t.Setenv("TRACKIFIED_API_KEY", "")
	if _, err := New(); err == nil {
		t.Fatal("expected error")
	}
}

func TestAutoPaging(t *testing.T) {
	const total = 5
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("status") != "delivered" || r.URL.Query().Get("limit") != "2" {
			t.Errorf("query = %v", r.URL.RawQuery)
		}
		off, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		var data []map[string]string
		for i := off; i < min(off+2, total); i++ {
			data = append(data, map[string]string{"id": "trk_" + strconv.Itoa(i), "created_at": "2026-11-24T00:00:00Z", "updated_at": "2026-11-24T00:00:00Z"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "pagination": map[string]int{"limit": 2, "offset": off, "total": total}})
	})
	p := &TrackingListParams{ListParams: ListParams{Limit: 2}, Status: StatusDelivered}

	var ids []string
	for tr, err := range c.Trackings.All(context.Background(), p) {
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, tr.ID)
	}
	if fmt.Sprint(ids) != "[trk_0 trk_1 trk_2 trk_3 trk_4]" {
		t.Fatalf("range ids = %v", ids)
	}

	it := c.Trackings.ListAutoPaging(context.Background(), p)
	n := 0
	for it.Next() {
		n++
	}
	if it.Err() != nil || n != total {
		t.Fatalf("classic n=%d err=%v", n, it.Err())
	}
}

// TestLive runs only when TRACKIFIED_API_KEY is set. It creates one tracking and deletes it.
func TestLive(t *testing.T) {
	if os.Getenv("TRACKIFIED_API_KEY") == "" {
		t.Skip("TRACKIFIED_API_KEY not set")
	}
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tr, err := c.Trackings.Create(ctx, &CreateTrackingParams{TrackingNumber: "9434650110837017316201", Carrier: "usps"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := c.Trackings.Delete(ctx, tr.ID); err != nil {
			t.Error(err)
		}
	}()
	if got, err := c.Trackings.Get(ctx, tr.ID); err != nil || got.ID != tr.ID {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	if d, err := c.Carriers.Detect(ctx, "9434650110837017316201"); err != nil || len(d.Candidates) == 0 {
		t.Fatalf("Detect = %+v, %v", d, err)
	}
	n := 0
	for _, err := range c.Trackings.All(ctx, &TrackingListParams{ListParams: ListParams{Limit: 100}}) {
		if err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n == 0 {
		t.Fatal("expected at least one tracking")
	}
	if _, err := c.Usage.Get(ctx); err != nil {
		t.Fatal(err)
	}
	if h, err := c.Health(ctx); err != nil || h.Status != "ok" {
		t.Fatalf("Health = %+v, %v", h, err)
	}
	if r, err := c.Carriers.Resolve(ctx, "USPS Priority Mail"); err != nil || r == nil || r.Carrier != "usps" {
		t.Fatalf("Resolve = %+v, %v", r, err)
	}
}
