package xdk

import (
	"errors"
	"fmt"
)

var (
	ErrOAuth2NotConfigured = errors.New("oauth2 credentials not configured")
)

// APIError is returned for non-2xx HTTP responses.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("x api error: status=%d body=%s", e.StatusCode, e.Body)
}
