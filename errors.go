package typesafe

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var (
	// ErrInvalidConfig is returned by New.
	ErrInvalidConfig = errors.New("typesafe: invalid configuration")

	// ErrInvalidRequest matches every ValidationError.
	ErrInvalidRequest = errors.New("typesafe: invalid request")
)

// Sentinels matched by *APIError according to its status code.
var (
	ErrBadRequest          = errors.New("typesafe: bad request")           // 400
	ErrAuthentication      = errors.New("typesafe: authentication failed") // 401
	ErrPermissionDenied    = errors.New("typesafe: permission denied")     // 403
	ErrNotFound            = errors.New("typesafe: not found")             // 404
	ErrUnprocessableEntity = errors.New("typesafe: unprocessable entity")  // 422
	ErrRateLimited         = errors.New("typesafe: rate limited")          // 429
	ErrServer              = errors.New("typesafe: server error")          // 5xx
)

// ValidationError is a problem with a request found before sending it.
type ValidationError struct {
	Path string // e.g. questions["grade"].criteria
	Err  error
}

func invalid(path, format string, args ...any) *ValidationError {
	return &ValidationError{Path: path, Err: fmt.Errorf(format, args...)}
}

func (e *ValidationError) Error() string {
	if e.Path == "" {
		return "typesafe: invalid request: " + e.Err.Error()
	}
	return "typesafe: invalid request: " + e.Path + ": " + e.Err.Error()
}

func (e *ValidationError) Unwrap() error        { return e.Err }
func (e *ValidationError) Is(target error) bool { return target == ErrInvalidRequest }

// APIError is a non-2xx response.
type APIError struct {
	StatusCode int
	Method     string
	URL        string
	RequestID  string
	Message    string // Provider message for explicit inspection. Excluded from Error().
	Header     http.Header
	Body       []byte // truncated to 64 KiB
}

// RetryAfter returns the delay requested by the retry-after-ms or Retry-After
// header, if any.
func (e *APIError) RetryAfter() (time.Duration, bool) {
	return parseRetryAfter(e.Header, time.Now())
}

func (e *APIError) Error() string {
	s := fmt.Sprintf("typesafe: %s %s: %d %s", e.Method, e.URL, e.StatusCode, http.StatusText(e.StatusCode))
	if e.RequestID != "" {
		s += " (request " + e.RequestID + ")"
	}
	return s
}

func (e *APIError) Is(target error) bool {
	switch target {
	case ErrBadRequest:
		return e.StatusCode == http.StatusBadRequest
	case ErrAuthentication:
		return e.StatusCode == http.StatusUnauthorized
	case ErrPermissionDenied:
		return e.StatusCode == http.StatusForbidden
	case ErrNotFound:
		return e.StatusCode == http.StatusNotFound
	case ErrUnprocessableEntity:
		return e.StatusCode == http.StatusUnprocessableEntity
	case ErrRateLimited:
		return e.StatusCode == http.StatusTooManyRequests
	case ErrServer:
		return e.StatusCode >= 500 && e.StatusCode <= 599
	}
	return false
}

// ConnectionError is a request that failed without a response.
type ConnectionError struct {
	Method string
	URL    string
	Err    error
}

func (e *ConnectionError) Error() string {
	return fmt.Sprintf("typesafe: %s %s: connection error: %v", e.Method, e.URL, e.Err)
}

func (e *ConnectionError) Unwrap() error { return e.Err }

// TimeoutError is an attempt that exceeded the client or request timeout. A
// deadline on the caller's context is returned as the context error instead.
type TimeoutError struct {
	Method  string
	URL     string
	Timeout time.Duration
	Err     error
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("typesafe: %s %s: timed out after %s", e.Method, e.URL, e.Timeout)
}

func (e *TimeoutError) Unwrap() error { return e.Err }

// ResponseValidationError is a 2xx response that is malformed or does not
// answer the request.
type ResponseValidationError struct {
	Method     string
	URL        string
	StatusCode int
	RequestID  string
	Path       string // e.g. answers["grade"]; empty for the whole body
	Err        error
	Header     http.Header
	Body       []byte // truncated to 64 KiB
}

func (e *ResponseValidationError) Error() string {
	s := fmt.Sprintf("typesafe: %s %s: invalid response: ", e.Method, e.URL)
	if e.Path != "" {
		s += e.Path + ": "
	}
	s += e.Err.Error()
	if e.RequestID != "" {
		s += " (request " + e.RequestID + ")"
	}
	return s
}

func (e *ResponseValidationError) Unwrap() error { return e.Err }

// IsRetryable reports whether DefaultRetryPolicy retries err.
func IsRetryable(err error) bool {
	return DefaultRetryPolicy().retryable(err)
}

// errorMessage extracts a message from common error body shapes, including
// FastAPI validation errors.
func errorMessage(body []byte) string {
	var v struct{ Error, Message, Detail any }
	if json.Unmarshal(body, &v) != nil {
		return ""
	}
	for _, field := range []any{v.Error, v.Message, v.Detail} {
		switch f := field.(type) {
		case string:
			return f
		case map[string]any:
			if s, ok := f["message"].(string); ok {
				return s
			}
		case []any:
			var msgs []string
			for _, item := range f {
				if m, ok := item.(map[string]any); ok {
					if s, ok := m["msg"].(string); ok {
						msgs = append(msgs, s)
					}
				}
			}
			if len(msgs) > 0 {
				return strings.Join(msgs, "; ")
			}
		}
	}
	return ""
}
