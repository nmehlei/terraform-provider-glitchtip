// src/glitchtip/errors.go
package glitchtip

import (
	"errors"
	"fmt"
)

var errorsAs = errors.As

// ConfigError indicates a problem with local client configuration —
// a bad endpoint, missing credential, or unsupported API version. It is
// never returned for a problem on the server side; that's APIError.
type ConfigError struct {
	Msg string
	Err error
}

func (e *ConfigError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("glitchtip: %s: %v", e.Msg, e.Err)
	}
	return fmt.Sprintf("glitchtip: %s", e.Msg)
}

func (e *ConfigError) Unwrap() error { return e.Err }

// APIError is a non-2xx response from the GlitchTip API.
type APIError struct {
	Method     string
	URL        string
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("glitchtip: %s %s: unexpected status %d: %s", e.Method, e.URL, e.StatusCode, e.Body)
}

// NotFound reports whether the server responded 404, the signal every
// resource's Read method uses to detect out-of-band deletion.
func (e *APIError) NotFound() bool {
	return e.StatusCode == 404
}

// asAPIError is a small errors.As wrapper kept in this package so client
// tests can assert on *APIError without importing "errors" in every file.
func asAPIError(err error, target **APIError) bool {
	return errorsAs(err, target)
}
