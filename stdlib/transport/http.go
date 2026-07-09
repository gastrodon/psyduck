package transport

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RequestConfig is the shared decode target for the dual-role `request`
// resource. Both the producer and consumer decode into this one struct, so the
// field set is declared once rather than duplicated. Body and IntervalMs are
// used only when producing (polling); the consumer ignores them.
type RequestConfig struct {
	URL          string            `psy:"url"`
	Method       string            `psy:"method"`
	Headers      map[string]string `psy:"headers"`
	Body         string            `psy:"body"`
	QueryParams  map[string]string `psy:"query-params"`
	BasicAuth    string            `psy:"basic-auth"`
	TimeoutMs    int               `psy:"timeout-ms"`
	SuccessCodes []int             `psy:"success-codes"`
	IntervalMs   int               `psy:"interval-ms"`
}

// HTTP projects the request-shaping options out of a RequestConfig (dropping
// the producer-only Body/IntervalMs, which the caller handles).
func (c RequestConfig) HTTP() HTTP {
	return HTTP{
		URL:          c.URL,
		Method:       c.Method,
		Headers:      c.Headers,
		QueryParams:  c.QueryParams,
		BasicAuth:    c.BasicAuth,
		TimeoutMs:    c.TimeoutMs,
		SuccessCodes: c.SuccessCodes,
	}
}

// HTTP holds the request options shared by the request producer and consumer.
// It is decoded from a flat config and turned into configured requests via
// Do — the "give me a closure" surface for HTTP, so producer polling and
// consumer posting share one code path.
type HTTP struct {
	URL          string
	Method       string
	Headers      map[string]string
	QueryParams  map[string]string
	BasicAuth    string // "user:pass"
	TimeoutMs    int
	SuccessCodes []int
}

// Client builds an http.Client honoring the configured timeout.
func (h HTTP) Client() *http.Client {
	timeout := time.Duration(h.TimeoutMs) * time.Millisecond
	if h.TimeoutMs <= 0 {
		timeout = 30 * time.Second
	}
	return &http.Client{Timeout: timeout}
}

// Do issues one request with the given body (nil for none) and returns the
// response body. A status code outside SuccessCodes is an error. ctx bounds
// the request — cancelling it aborts an in-flight Do promptly instead of
// waiting out the client's timeout.
func (h HTTP) Do(ctx context.Context, client *http.Client, body []byte) ([]byte, error) {
	target, err := url.Parse(h.URL)
	if err != nil {
		return nil, fmt.Errorf("http: bad url %q: %w", h.URL, err)
	}
	if len(h.QueryParams) > 0 {
		q := target.Query()
		for k, v := range h.QueryParams {
			q.Set(k, v)
		}
		target.RawQuery = q.Encode()
	}

	var reader io.Reader
	if body != nil {
		reader = strings.NewReader(string(body))
	}

	method := h.Method
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), reader)
	if err != nil {
		return nil, fmt.Errorf("http: build request: %w", err)
	}
	for k, v := range h.Headers {
		req.Header.Set(k, v)
	}
	if h.BasicAuth != "" {
		user, pass, _ := strings.Cut(h.BasicAuth, ":")
		req.SetBasicAuth(user, pass)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("http: read body: %w", err)
	}
	if !h.accepts(resp.StatusCode) {
		return data, fmt.Errorf("http: %s returned %d", h.URL, resp.StatusCode)
	}
	return data, nil
}

func (h HTTP) accepts(code int) bool {
	codes := h.SuccessCodes
	if len(codes) == 0 {
		codes = []int{200}
	}
	for _, c := range codes {
		if c == code {
			return true
		}
	}
	return false
}
