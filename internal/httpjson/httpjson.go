// Package httpjson is the one place the providers talk HTTP: a JSON request,
// a size-capped JSON response, and a typed error for non-2xx statuses.
package httpjson

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const maxBody = 1 << 20

// NewClient returns a client with a timeout, which http.DefaultClient lacks.
func NewClient() *http.Client { return &http.Client{Timeout: 20 * time.Second} }

// StatusError is a non-2xx response. Error() leaves the body out: bodies of
// these private endpoints can carry account identifiers that must not reach
// logs. Callers that need it (an OAuth "invalid_grant") read Body.
type StatusError struct {
	Code int
	Body []byte
}

func (e *StatusError) Error() string { return fmt.Sprintf("unexpected HTTP status %d", e.Code) }

// Do sends body as JSON (when non-nil) and decodes a 2xx response into out
// (when non-nil).
func Do(ctx context.Context, c *http.Client, method, url string, header http.Header, body, out any) error {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, req.URL.Host, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &StatusError{Code: resp.StatusCode, Body: raw}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
