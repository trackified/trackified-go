# trackified-go

[![CI](https://github.com/trackified/trackified-go/actions/workflows/ci.yml/badge.svg)](https://github.com/trackified/trackified-go/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/trackified/trackified-go.svg)](https://pkg.go.dev/github.com/trackified/trackified-go)

The official Go client for the [Trackified](https://trackified.net) parcel-tracking API.
Standard library only. Requires Go 1.23+.

- API docs: https://forumulate.com/p/trackified/docs
- Webhook signing guide: https://trackified.net/developers/webhook-signing

## Install

```sh
go get github.com/trackified/trackified-go@v0.2.1
```

## Quickstart

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/trackified/trackified-go"
)

func main() {
	ctx := context.Background()

	// Reads TRACKIFIED_API_KEY (and TRACKIFIED_BASE_URL) when no option is given.
	client, err := trackified.New(trackified.WithAPIKey("live_..."))
	if err != nil {
		log.Fatal(err)
	}

	// Create a tracking. Leave Carrier empty to detect it from the number.
	t, err := client.Trackings.Create(ctx, &trackified.CreateTrackingParams{
		TrackingNumber: "9434650110837017316201",
		Carrier:        "usps",
	})
	if err != nil {
		var ve *trackified.ValidationError
		if errors.As(err, &ve) {
			log.Fatalf("invalid: %s (%s)", ve.Message, ve.Code)
		}
		log.Fatal(err)
	}

	// Get it with its events, newest first (guaranteed by the API).
	t, err = client.Trackings.Get(ctx, t.ID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(t.Status, len(t.Events))

	// List every delivered tracking, fetching pages as needed.
	for t, err := range client.Trackings.All(ctx, &trackified.TrackingListParams{Status: trackified.StatusDelivered}) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(t.ID, t.TrackingNumber)
	}
}
```

Prefer explicit iteration? `ListAutoPaging` returns an iterator with `Next`, `Current` and `Err`:

```go
it := client.Trackings.ListAutoPaging(ctx, nil)
for it.Next() {
	fmt.Println(it.Current().ID)
}
if err := it.Err(); err != nil {
	log.Fatal(err)
}
```

### Verify a webhook

Pass the **raw** request body; re-encoded JSON will not verify.

```go
http.HandleFunc("/webhooks/trackified", func(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	event, err := trackified.ConstructEvent(body, r.Header.Get(trackified.SignatureHeader), os.Getenv("TRACKIFIED_WEBHOOK_SECRET"))
	if err != nil {
		http.Error(w, "bad signature", http.StatusBadRequest)
		return
	}
	switch event.Type {
	case trackified.EventTypeTrackingStatusChanged:
		sc, _ := event.StatusChange() // {Tracking, PreviousStatus, CurrentStatus}
		log.Printf("%s: %s -> %s", sc.Tracking.TrackingNumber, sc.PreviousStatus, sc.CurrentStatus)
	case trackified.EventTypeTrackingDelivered:
		t, _ := event.Tracking() // every other type carries a Tracking
		log.Printf("%s delivered", t.TrackingNumber)
	}
	w.WriteHeader(http.StatusNoContent)
})
```

`trackified.VerifyWebhook(body, header, secret)` returns a bool instead. Both reject timestamps more
than 5 minutes from now; pass a tolerance as the last argument to change it.

`event.Data` stays raw JSON. `Tracking()` fails on `tracking.status_changed` and `StatusChange()` fails on
every other type; `trackified.TrackingOf(event)` returns the Tracking of any event.

#### Why a signature was rejected

```go
r := trackified.VerifyDetailed(body, header, []string{secret})
if !r.Valid {
	log.Printf("rejected: %s", r.Reason) // ReasonMissing, ReasonMalformed, ReasonTimestamp or ReasonMismatch
}
```

`ConstructEvent` returns a `*trackified.SignatureVerificationError` with the same `Reason`;
`errors.Is(err, trackified.ErrInvalidSignature)` still matches it.

| Reason | Meaning |
| --- | --- |
| `missing` | empty header or secret |
| `malformed` | no `t` or `v1`, non-numeric `t`, bad hex |
| `timestamp` | outside the tolerance |
| `mismatch` | no `v1` matches |

#### Rotating the signing secret

```go
hours := 24 // how long the old secret keeps working (0-168; 0 drops it now)
w, err := client.Webhooks.RotateSecret(ctx, "wh_123", &trackified.RotateSecretParams{ExpiresInHours: &hours})
// w.Secret is the new secret, shown once. Until w.PreviousSecretExpiresAt each delivery carries
// "t=...,v1=<new>,v1=<old>", so both secrets verify. Accept both while you deploy the new one:
event, err := trackified.ConstructEventSecrets(body, header, []string{newSecret, oldSecret})
```

A header may carry several `v1=` values; it is valid if any of them matches any secret (each compared in constant time).

### Update a webhook

```go
off := false
w, err := client.Webhooks.Update(ctx, "wh_123", &trackified.UpdateWebhookParams{Enabled: &off})
```

`URL`, `EventTypes` and `Enabled` are optional (set at least one). A disabled webhook receives nothing. The secret is unchanged.

### Resolve a carrier name

```go
r, err := client.Carriers.Resolve(ctx, "USPS Priority Mail") // also "dhl_express", "Correos de España", ...
if r == nil { /* no carrier we serve matches */ }
fmt.Println(r.Carrier, r.Confidence, r.Matched) // usps 0.97 alias
```

`Trackings.Create` accepts the same free-text names in `Carrier` and stores our id. Each `Carrier` has `Aliases`.

### Health

```go
h, err := client.Health(ctx) // h.Status == "ok"; a degraded API returns *trackified.APIError (503)
```

### Hooks

```go
client, _ := trackified.New(trackified.WithHooks(trackified.Hooks{
	OnRequest: func(r trackified.RequestInfo) { log.Printf("-> %s %s (try %d)", r.Method, r.URL, r.Attempt) },
	OnResponse: func(r trackified.ResponseInfo) {
		log.Printf("<- %d %s in %s replayed=%t err=%v", r.Status, r.RequestID, r.Duration, r.IdempotentReplayed, r.Err)
	},
}))
```

Both run once per attempt, retries included. `Err` is the typed error when the status is 400 or more or the
connection failed. A panicking hook is recovered and never breaks the request.

### Event times

`Events` are newest first, guaranteed by the API. `OccurredAt` carries an offset when the carrier gives one
(`TimeZoneKnown` is true); a zoneless value is the local time where the scan happened. `Date` is deprecated,
sent until 2027-06-01 and removed after.

## Resources

| Field | Methods |
| --- | --- |
| `client.Trackings` | `Create`, `BulkCreate`, `Get`, `Update`, `Delete`, `Retrack`, `List`, `ListAutoPaging`, `All` |
| `client.Carriers` | `List`, `ListAutoPaging`, `Get`, `Detect`, `Resolve`, `Catalog`, `ValidationStatus` |
| `client.Webhooks` | `Create`, `Update`, `RotateSecret`, `List`, `ListAutoPaging`, `Delete`, `Test`, `Verify`, `VerifyDetailed`, `ConstructEvent`, `TrackingOf` |
| `client.WebhookDeliveries` | `List`, `ListAutoPaging` |
| `client.APIKeys` | `Create`, `List`, `ListAutoPaging`, `Revoke` |
| `client.Usage` | `Get` (calls `/v1/account/usage`) |
| `client` | `Health` |

## Configuration

| Option | Default |
| --- | --- |
| `WithAPIKey` | `TRACKIFIED_API_KEY` |
| `WithBaseURL` | `TRACKIFIED_BASE_URL`, else `https://api.trackified.net` (a trailing `/v1` is accepted) |
| `WithTimeout` | 30s |
| `WithMaxRetries` | 2 |
| `WithHTTPClient`, `WithUserAgent`, `WithHooks` | |

Network errors, 408, 429 (honouring `retry-after`) and 5xx are retried with exponential backoff and
jitter. POSTs that create trackings send an `Idempotency-Key` (generated unless you set
`IdempotencyKey`) that stays the same across retries.

## Errors

Every API error unwraps to `*trackified.TrackifiedError` (`StatusCode`, `Code`, `Message`, `RequestID`).
Match a specific kind with `errors.As`: `AuthenticationError` (401), `PermissionError` (403),
`QuotaExceededError` (402, `quota_exceeded`), `NotFoundError` (404), `ConflictError` (409, e.g.
`idempotency_in_progress`; not retried), `ValidationError` (400/422), `RateLimitError` (429, with `RetryAfter`),
`APIError` (5xx), `ConnectionError` (no response; `Code` is `"timeout"` or `"connection"`).

After a timeout the outcome is **unknown**: the request may have been applied. Retry with the same
idempotency key; the SDK does this automatically for the creates it retries.

## License

MIT
