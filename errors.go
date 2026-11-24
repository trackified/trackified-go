package trackified

import (
	"fmt"
	"time"
)

// TrackifiedError is the base error for every failed API call. All typed errors below unwrap to it,
// so errors.As(err, &(*trackified.TrackifiedError)) matches any of them.
type TrackifiedError struct {
	StatusCode int    // HTTP status; 0 for connection errors
	Code       string // machine-readable code, e.g. "carrier_not_detected"
	Message    string
	RequestID  string
	Err        error // underlying cause for connection errors
}

func (e *TrackifiedError) Error() string {
	s := "trackified: "
	if e.StatusCode != 0 {
		s += fmt.Sprintf("%d ", e.StatusCode)
	}
	if e.Code != "" {
		s += e.Code + ": "
	}
	s += e.Message
	if e.RequestID != "" {
		s += " (request_id " + e.RequestID + ")"
	}
	return s
}

func (e *TrackifiedError) Unwrap() error { return e.Err }

// AuthenticationError is returned for 401 responses.
type AuthenticationError struct{ *TrackifiedError }

// PermissionError is returned for 403 responses.
type PermissionError struct{ *TrackifiedError }

// NotFoundError is returned for 404 responses.
type NotFoundError struct{ *TrackifiedError }

// ValidationError is returned for 400 and 422 responses.
type ValidationError struct{ *TrackifiedError }

// RateLimitError is returned for 429 responses.
type RateLimitError struct {
	*TrackifiedError
	RetryAfter time.Duration // from the retry-after header; 0 if absent
}

// APIError is returned for 5xx responses.
type APIError struct{ *TrackifiedError }

// ConnectionError is returned when the request never got an HTTP response.
type ConnectionError struct{ *TrackifiedError }

func (e *AuthenticationError) Unwrap() error { return e.TrackifiedError }
func (e *PermissionError) Unwrap() error     { return e.TrackifiedError }
func (e *NotFoundError) Unwrap() error       { return e.TrackifiedError }
func (e *ValidationError) Unwrap() error     { return e.TrackifiedError }
func (e *RateLimitError) Unwrap() error      { return e.TrackifiedError }
func (e *APIError) Unwrap() error            { return e.TrackifiedError }
func (e *ConnectionError) Unwrap() error     { return e.TrackifiedError }
