package trackified

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	vecSecretB  = "whsec_trackified_test_vector_b"
	vecDualHead = "t=1792560000,v1=aaa4b846892609125e20b07a531d46e9e4614d8eec8d6dd3ecccae877b58111f,v1=19752dd2fca1872cc8aa6cc36c412fce3bbfa120480580bdb1ad9d7957dfc9f4"
)

func TestDualSignatureVector(t *testing.T) {
	pinClock(t, time.Unix(1792560000, 0))
	for name, secrets := range map[string][]string{"A (second v1)": {vecSecret}, "B": {vecSecretB}, "A,B": {vecSecret, vecSecretB}, "wrong,A": {"x", vecSecret}} {
		if r := VerifyDetailed([]byte(vecBody), vecDualHead, secrets); !r.Valid || r.Reason != "" {
			t.Errorf("%s: %+v", name, r)
		}
	}
	if _, err := ConstructEventSecrets([]byte(vecBody), vecDualHead, []string{vecSecretB}); err != nil {
		t.Error(err)
	}
}

func TestVerifyReasons(t *testing.T) {
	pinClock(t, time.Unix(1792560000, 0))
	cases := map[string]struct {
		header string
		secret string
		want   string
	}{
		"empty header": {"", vecSecret, ReasonMissing},
		"empty secret": {vecHeader, "", ReasonMissing},
		"garbage":      {"garbage", vecSecret, ReasonMalformed},
		"no v1":        {"t=1792560000", vecSecret, ReasonMalformed},
		"no t":         {"v1=19752dd2", vecSecret, ReasonMalformed},
		"non-numeric":  {"t=abc,v1=19752dd2", vecSecret, ReasonMalformed},
		"bad hex":      {"t=1792560000,v1=zz", vecSecret, ReasonMalformed},
		"stale":        {"t=1792550000,v1=19752dd2", vecSecret, ReasonTimestamp},
		"mismatch":     {vecHeader, "wrong", ReasonMismatch},
	}
	for name, c := range cases {
		if r := VerifyDetailed([]byte(vecBody), c.header, []string{c.secret}); r.Valid || r.Reason != c.want {
			t.Errorf("%s: %+v, want %s", name, r, c.want)
		}
	}
	_, err := ConstructEvent([]byte(vecBody), vecHeader, "wrong")
	var sve *SignatureVerificationError
	if !errors.As(err, &sve) || sve.Reason != ReasonMismatch || !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("err = %v", err)
	}
}

func TestTypedEvents(t *testing.T) {
	sc := &WebhookEvent{Type: EventTypeTrackingStatusChanged, Data: json.RawMessage(`{"tracking":{"id":"trk_1"},"previous_status":"in_transit","current_status":"delivered"}`)}
	if v, err := sc.StatusChange(); err != nil || v.Tracking.ID != "trk_1" || v.CurrentStatus != StatusDelivered || v.PreviousStatus != StatusInTransit {
		t.Fatalf("StatusChange = %+v, %v", v, err)
	}
	if _, err := sc.Tracking(); err == nil {
		t.Error("Tracking() on status_changed should fail")
	}
	up := &WebhookEvent{Type: EventTypeTrackingDelivered, Data: json.RawMessage(`{"id":"trk_2"}`)}
	if _, err := up.StatusChange(); err == nil {
		t.Error("StatusChange() on delivered should fail")
	}
	for _, e := range []*WebhookEvent{sc, up} {
		if tr, err := TrackingOf(e); err != nil || tr.ID == "" {
			t.Errorf("TrackingOf(%s) = %+v, %v", e.Type, tr, err)
		}
	}
}

func TestBaseURLNormalized(t *testing.T) {
	for _, in := range []string{"https://api.trackified.net", "https://api.trackified.net/", "https://api.trackified.net/v1", "https://api.trackified.net/v1/"} {
		c, _ := New(WithAPIKey("k"), WithBaseURL(in))
		if c.baseURL != "https://api.trackified.net" {
			t.Errorf("%q -> %q", in, c.baseURL)
		}
	}
	var path string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	WithBaseURL(c.baseURL + "/v1/")(c)
	if h, err := c.Health(context.Background()); err != nil || h.Status != "ok" || path != "/v1/health" {
		t.Fatalf("Health = %+v, %v, path %s", h, err, path)
	}
}

func TestNewEndpoints(t *testing.T) {
	var got []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery+" "+string(b))
		switch r.URL.Path {
		case "/v1/carriers/resolve":
			if r.URL.Query().Get("name") == "nope" {
				_, _ = w.Write([]byte(`{"carrier":null,"confidence":0}`))
				return
			}
			_, _ = w.Write([]byte(`{"carrier":"usps","name":"USPS","confidence":0.97,"matched":"alias"}`))
		case "/v1/carriers/usps":
			_, _ = w.Write([]byte(`{"id":"usps","displayName":"USPS","aliases":["united states postal service"]}`))
		case "/v1/webhooks/wh_1":
			_, _ = w.Write([]byte(`{"id":"wh_1","url":"https://x","enabled":false,"created_at":"2026-11-24T00:00:00Z"}`))
		case "/v1/webhooks/wh_1/rotate-secret":
			_, _ = w.Write([]byte(`{"id":"wh_1","secret":"whsec_new","previous_secret_expires_at":"2026-11-25T00:00:00Z","created_at":"2026-11-24T00:00:00Z"}`))
		}
	})
	ctx := context.Background()
	if r, err := c.Carriers.Resolve(ctx, "USPS Priority Mail"); err != nil || r.Carrier != "usps" || r.Matched != "alias" {
		t.Fatalf("Resolve = %+v, %v", r, err)
	}
	if r, err := c.Carriers.Resolve(ctx, "nope"); err != nil || r != nil {
		t.Fatalf("Resolve(nope) = %+v, %v", r, err)
	}
	if cr, err := c.Carriers.Get(ctx, "usps"); err != nil || cr.DisplayName != "USPS" || len(cr.Aliases) != 1 {
		t.Fatalf("Get = %+v, %v", cr, err)
	}
	off := false
	if w, err := c.Webhooks.Update(ctx, "wh_1", &UpdateWebhookParams{Enabled: &off}); err != nil || w.Enabled {
		t.Fatalf("Update = %+v, %v", w, err)
	}
	zero := 0
	if w, err := c.Webhooks.RotateSecret(ctx, "wh_1", &RotateSecretParams{ExpiresInHours: &zero}); err != nil || w.Secret != "whsec_new" || w.PreviousSecretExpiresAt == nil {
		t.Fatalf("RotateSecret = %+v, %v", w, err)
	}
	if _, err := c.Webhooks.RotateSecret(ctx, "wh_1", nil); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GET /v1/carriers/resolve?name=USPS+Priority+Mail ",
		"GET /v1/carriers/resolve?name=nope ",
		"GET /v1/carriers/usps? ",
		`PATCH /v1/webhooks/wh_1? {"enabled":false}`,
		`POST /v1/webhooks/wh_1/rotate-secret? {"expires_in_hours":0}`,
		`POST /v1/webhooks/wh_1/rotate-secret? {}`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests:\n%s", strings.Join(got, "\n"))
	}
}

func TestQuotaAndConflictErrors(t *testing.T) {
	for status, check := range map[int]func(error) bool{
		402: func(e error) bool { var x *QuotaExceededError; return errors.As(e, &x) && x.Code == "quota_exceeded" },
		409: func(e error) bool {
			var x *ConflictError
			return errors.As(e, &x) && x.Code == "idempotency_in_progress"
		},
	} {
		calls := 0
		code := map[int]string{402: "quota_exceeded", 409: "idempotency_in_progress"}[status]
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"code":"` + code + `","message":"m"}}`))
		})
		_, err := c.Trackings.Create(context.Background(), &CreateTrackingParams{TrackingNumber: "1"})
		var base *TrackifiedError
		if !check(err) || !errors.As(err, &base) || calls != 1 {
			t.Errorf("%d: err=%v calls=%d", status, err, calls)
		}
	}
}

func TestConnectionErrorCode(t *testing.T) {
	c, _ := New(WithAPIKey("k"), WithBaseURL("http://127.0.0.1:1"), WithMaxRetries(0))
	_, err := c.Usage.Get(context.Background())
	var ce *ConnectionError
	if !errors.As(err, &ce) || ce.Code != "connection" {
		t.Fatalf("err = %v", err)
	}
	slow := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(200 * time.Millisecond) })
	WithTimeout(20 * time.Millisecond)(slow)
	WithMaxRetries(0)(slow)
	_, err = slow.Usage.Get(context.Background())
	if !errors.As(err, &ce) || ce.Code != "timeout" {
		t.Fatalf("timeout err = %v", err)
	}
}

func TestHooks(t *testing.T) {
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("X-Request-Id", "req_"+string(rune('0'+calls)))
		if calls == 1 {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Idempotent-Replayed", "true")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"id":"trk_1"}`))
	})
	var reqs []RequestInfo
	var resps []ResponseInfo
	WithHooks(Hooks{
		OnRequest:  func(i RequestInfo) { reqs = append(reqs, i); panic("hooks must not break requests") },
		OnResponse: func(i ResponseInfo) { resps = append(resps, i); panic("nor here") },
	})(c)
	if _, err := c.Trackings.Create(context.Background(), &CreateTrackingParams{TrackingNumber: "1", IdempotencyKey: "idem_1"}); err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 2 || reqs[0].Attempt != 1 || reqs[1].Attempt != 2 || reqs[1].IdempotencyKey != "idem_1" || reqs[0].Method != "POST" || !strings.HasSuffix(reqs[0].URL, "/v1/trackings") {
		t.Fatalf("requests = %+v", reqs)
	}
	var api *APIError
	if len(resps) != 2 || resps[0].Status != 503 || !errors.As(resps[0].Err, &api) || resps[0].RequestID != "req_1" ||
		resps[1].Status != 201 || resps[1].Err != nil || !resps[1].IdempotentReplayed || resps[1].Attempt != 2 {
		t.Fatalf("responses = %+v", resps)
	}
}

func TestWebhookTestRawBodyVerifies(t *testing.T) {
	pinClock(t, time.Unix(1792560000, 0))
	// Pretty-printed like the API, with keys out of alphabetical order.
	resp := `{
  "delivered": false,
  "signature": "` + vecHeader + `",
  "body": {
    "id": "evt_test",
    "type": "tracking.updated",
    "data": {
      "id": "trk_123"
    }
  }
}`
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(resp)) })
	r, err := c.Webhooks.Test(context.Background(), "wh_1")
	if err != nil || r.Body["id"] != "evt_test" || string(r.RawBody) != vecBody {
		t.Fatalf("Test = %+v (raw %s), %v", r, r.RawBody, err)
	}
	if !VerifyWebhook(r.RawBody, r.Signature, vecSecret) {
		t.Fatal("RawBody should verify")
	}
}
