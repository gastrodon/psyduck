package transport

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/psyduck-etl/sdk"
)

// Innermost fallback layer: used when neither a message's own request
// descriptor (Cp) nor a resource's static spec (Ct) sets a field.
const (
	defaultMethod    = http.MethodGet
	defaultTimeoutMs = 30000
)

var defaultSuccessCodes = []int{200}

// RequestSpec is one layer of HTTP request config: a resource's static
// spec block (Ct, decoded from HCL) or a single message's own request
// descriptor (Cp, decoded from that message's JSON via
// sdk.DecodeJSONTagged — same psy tags, either source). A zero field means
// unset at that layer, so Merge/Resolve know to fall through;
// FollowRedirects is a *bool since false is a real value, not "unset".
type RequestSpec struct {
	URL             string            `psy:"url"`
	Method          string            `psy:"method"`
	Headers         map[string]string `psy:"headers"`
	Body            string            `psy:"body"`
	QueryParams     map[string]string `psy:"query-params"`
	TimeoutMs       int               `psy:"timeout-ms"`
	SuccessCodes    []int             `psy:"success-codes"`
	FollowRedirects *bool             `psy:"follow-redirects"`
}

// Merge layers spec (Cp) over base (Ct): a field spec leaves unset falls
// through to base. Headers and query-params merge key by key rather than
// replace wholesale, so a message can add/override one header without
// repeating the rest.
func (spec RequestSpec) Merge(base RequestSpec) RequestSpec {
	out := base
	if spec.URL != "" {
		out.URL = spec.URL
	}
	if spec.Method != "" {
		out.Method = spec.Method
	}
	if len(spec.Headers) > 0 {
		out.Headers = mergeStrMap(base.Headers, spec.Headers)
	}
	if spec.Body != "" {
		out.Body = spec.Body
	}
	if len(spec.QueryParams) > 0 {
		out.QueryParams = mergeStrMap(base.QueryParams, spec.QueryParams)
	}
	if spec.TimeoutMs > 0 {
		out.TimeoutMs = spec.TimeoutMs
	}
	if len(spec.SuccessCodes) > 0 {
		out.SuccessCodes = spec.SuccessCodes
	}
	if spec.FollowRedirects != nil {
		out.FollowRedirects = spec.FollowRedirects
	}
	return out
}

func mergeStrMap(base, over map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(over))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}

// Resolve fills in the built-in defaults (GET, 30s timeout, success on
// 200, redirects followed) for anything still unset and returns an HTTP
// ready to run. Call Merge first, then Resolve: Cp.Merge(ct).Resolve().
func (spec RequestSpec) Resolve() HTTP {
	method := spec.Method
	if method == "" {
		method = defaultMethod
	}
	timeout := spec.TimeoutMs
	if timeout <= 0 {
		timeout = defaultTimeoutMs
	}
	codes := spec.SuccessCodes
	if len(codes) == 0 {
		codes = defaultSuccessCodes
	}
	follow := true
	if spec.FollowRedirects != nil {
		follow = *spec.FollowRedirects
	}
	return HTTP{
		URL:             spec.URL,
		Method:          method,
		Headers:         spec.Headers,
		Body:            spec.Body,
		QueryParams:     spec.QueryParams,
		TimeoutMs:       timeout,
		SuccessCodes:    codes,
		FollowRedirects: follow,
	}
}

// RequestConfig is RequestSpec plus IntervalMs, the request producer's
// polling-only knob (meaningless to the consumer, which rejects it).
type RequestConfig struct {
	RequestSpec
	IntervalMs int `psy:"interval-ms"`
}

// Compose decodes msg as a JSON request descriptor and merges it over ct
// (see RequestSpec.Merge), resolves it, and validates the result. Shared by
// fetch and the request consumer, which differ only in what they do with
// the response; the request producer routes its own fixed config through
// it too (with msg nil), so url is checked in one place for all three. An
// empty msg composes ct verbatim.
func Compose(msg []byte, ct RequestSpec) (HTTP, error) {
	cp := RequestSpec{}
	if len(bytes.TrimSpace(msg)) > 0 {
		if err := sdk.DecodeJSONTagged(msg, &cp); err != nil {
			return HTTP{}, fmt.Errorf("transport: decode request descriptor: %w", err)
		}
	}
	h := cp.Merge(ct).Resolve()
	if err := h.Validate(); err != nil {
		return HTTP{}, fmt.Errorf("transport: compose request: %w", err)
	}
	return h, nil
}

// Dispatch composes msg against ct (see Compose) and performs the request.
func Dispatch(ctx context.Context, msg []byte, ct RequestSpec) ([]byte, error) {
	h, err := Compose(msg, ct)
	if err != nil {
		return nil, err
	}
	return h.Do(ctx, h.Client())
}

// HTTP is a fully-resolved request, ready to run — every field already has
// its final value (RequestSpec.Resolve is the only place that's done).
type HTTP struct {
	URL             string
	Method          string
	Headers         map[string]string
	Body            string
	QueryParams     map[string]string
	TimeoutMs       int
	SuccessCodes    []int
	FollowRedirects bool
}

// Validate checks that a fully-resolved request is well-formed.
func (h HTTP) Validate() error {
	if h.URL == "" {
		return fmt.Errorf("url is required")
	}
	return nil
}

// Client builds an http.Client for the configured timeout and redirect
// policy. Transport is left unset, so a fresh client per request (Dispatch
// builds one each time, since these can vary per message) still shares
// http.DefaultTransport's connection pool.
func (h HTTP) Client() *http.Client {
	client := &http.Client{Timeout: time.Duration(h.TimeoutMs) * time.Millisecond}
	if !h.FollowRedirects {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return client
}

// Do issues the request and returns the response body. A status code
// outside SuccessCodes is an error. ctx bounds the request — cancelling it
// aborts an in-flight Do promptly instead of waiting out the client's
// timeout.
func (h HTTP) Do(ctx context.Context, client *http.Client) ([]byte, error) {
	target, err := url.Parse(h.URL)
	if err != nil {
		return nil, fmt.Errorf("http: parse url %q: %w", h.URL, err)
	}
	if len(h.QueryParams) > 0 {
		q := target.Query()
		for k, v := range h.QueryParams {
			q.Set(k, v)
		}
		target.RawQuery = q.Encode()
	}

	var reader io.Reader
	if h.Body != "" {
		reader = strings.NewReader(h.Body)
	}

	req, err := http.NewRequestWithContext(ctx, h.Method, target.String(), reader)
	if err != nil {
		return nil, fmt.Errorf("http: build %s request to %s: %w", h.Method, target, err)
	}
	for k, v := range h.Headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %s %s: %w", h.Method, target, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("http: %s %s: read response body: %w", h.Method, target, err)
	}
	if !h.accepts(resp.StatusCode) {
		return data, fmt.Errorf("http: %s %s: unexpected status %d", h.Method, target, resp.StatusCode)
	}
	return data, nil
}

func (h HTTP) accepts(code int) bool {
	for _, c := range h.SuccessCodes {
		if c == code {
			return true
		}
	}
	return false
}
