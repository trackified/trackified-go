# Changelog

## 0.2.0 - 2026-11-24

All changes are additive.

- T-3: `WithBaseURL` / `TRACKIFIED_BASE_URL` accept a trailing `/v1` (and trailing slash).
- T-4: `client.Health(ctx)`, calling the unauthenticated `GET /v1/health`.
- T-2: `Usage.Get` keeps calling `/v1/account/usage`.
- T-5: `Carriers.Resolve(ctx, name)` (nil when nothing we serve matches); `Carrier` gains `Aliases` and
  decodes `display_name` or `displayName`.
- T-6: typed webhook events: `StatusChange()` for `tracking.status_changed`, `Tracking()` for every other
  type (each fails on the wrong type), and `TrackingOf(event)` for any type. `Data` stays raw JSON.
- T-9: `VerifyDetailed` returns `VerifyResult{Valid, Reason}` with `ReasonMissing`, `ReasonMalformed`,
  `ReasonTimestamp`, `ReasonMismatch`; `ConstructEvent` returns `*SignatureVerificationError` (still
  `errors.Is(err, ErrInvalidSignature)`). Malformed `v1` hex is now rejected instead of skipped.
- T-10: headers with several `v1=` values verify if any matches; `VerifyDetailed` and
  `ConstructEventSecrets` take several secrets; `Webhooks.RotateSecret`; `Webhook.PreviousSecretExpiresAt`.
- T-11: `Webhooks.Update(ctx, id, &UpdateWebhookParams{URL, EventTypes, Enabled})`.
- T-12: `QuotaExceededError` (402) and `ConflictError` (409); `ConnectionError.Code` is `timeout` or `connection`.
- T-13: `WithHooks(Hooks{OnRequest, OnResponse})`, called once per attempt; panics are recovered.
- T-8, T-7: documented that events are newest first, what a zoneless `occurred_at` means, and that
  `date` is removed after 2027-06-01.

## 0.1.0 - 2026-11-24

- First release: every operation of the Trackified API (trackings, carriers, webhooks, webhook
  deliveries, API keys, usage), auto-paging iterators (`iter.Seq2` and `Next`/`Current`/`Err`),
  retries with backoff and idempotency keys, typed errors, and webhook signature verification.
