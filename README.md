# trackified-go

[![CI](https://github.com/trackified/trackified-go/actions/workflows/ci.yml/badge.svg)](https://github.com/trackified/trackified-go/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/trackified/trackified-go.svg)](https://pkg.go.dev/github.com/trackified/trackified-go)

The official Go client for the [Trackified](https://trackified.net) parcel-tracking API.
Standard library only. Requires Go 1.23+.

- API docs: https://forumulate.com/p/trackified/docs
- Webhook signing guide: https://trackified.net/developers/webhook-signing

## Install

```sh
go get github.com/trackified/trackified-go@v0.1.0
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
	client, err := trackified.New(trackified.WithAPIKey("tk_live_..."))
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

	// Get it with its events (newest first).
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
	if event.Type == trackified.EventTypeTrackingDelivered {
		t, _ := event.Tracking()
		log.Printf("%s delivered", t.TrackingNumber)
	}
	w.WriteHeader(http.StatusNoContent)
})
```

`trackified.VerifyWebhook(body, header, secret)` returns a bool instead. Both reject timestamps more
than 5 minutes from now; pass a tolerance as the last argument to change it.

## Resources

| Field | Methods |
| --- | --- |
| `client.Trackings` | `Create`, `BulkCreate`, `Get`, `Update`, `Delete`, `Retrack`, `List`, `ListAutoPaging`, `All` |
| `client.Carriers` | `List`, `ListAutoPaging`, `Get`, `Detect`, `Catalog`, `ValidationStatus` |
| `client.Webhooks` | `Create`, `List`, `ListAutoPaging`, `Delete`, `Test`, `Verify`, `ConstructEvent` |
| `client.WebhookDeliveries` | `List`, `ListAutoPaging` |
| `client.APIKeys` | `Create`, `List`, `ListAutoPaging`, `Revoke` |
| `client.Usage` | `Get` |

## Configuration

| Option | Default |
| --- | --- |
| `WithAPIKey` | `TRACKIFIED_API_KEY` |
| `WithBaseURL` | `TRACKIFIED_BASE_URL`, else `https://api.trackified.net` |
| `WithTimeout` | 30s |
| `WithMaxRetries` | 2 |
| `WithHTTPClient`, `WithUserAgent` | |

Network errors, 408, 429 (honouring `retry-after`) and 5xx are retried with exponential backoff and
jitter. POSTs that create trackings send an `Idempotency-Key` (generated unless you set
`IdempotencyKey`) that stays the same across retries.

## Errors

Every API error unwraps to `*trackified.TrackifiedError` (`StatusCode`, `Code`, `Message`, `RequestID`).
Match a specific kind with `errors.As`: `AuthenticationError` (401), `PermissionError` (403),
`NotFoundError` (404), `ValidationError` (400/422), `RateLimitError` (429, with `RetryAfter`),
`APIError` (5xx), `ConnectionError` (no response).

## License

MIT
