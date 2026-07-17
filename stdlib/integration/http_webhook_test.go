package integration

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gastrodon/psyduck/stdlib/produce"
	"github.com/gastrodon/psyduck/stdlib/transform"
)

// TestHTTPWebhookReceiver exercises produce-listen as an inbound TCP endpoint,
// receiving raw HTTP bytes. N concurrent POST requests must all be received exactly once
// as raw HTTP request wire format. Each request is sent on its own connection,
// which provides natural message framing (connection close = end of message).
func TestHTTPWebhookReceiver(t *testing.T) {
	const N = 12
	addr := freePort(t)
	tcpAddr := "tcp://" + addr

	// Use produce.Listen on TCP. Since each HTTP request comes on its own connection,
	// the connection closure provides natural framing (no delimiter needed, but we must provide one).
	// Use an unlikely sequence as delimiter so we capture the full HTTP request in one message.
	p, err := produce.Listen(parser(map[string]any{
		"location": tcpAddr,
		"sep":      "\x00", // NUL byte - unlikely in HTTP text
	}))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	// Buffered so concurrent requests aren't blocked by slow channel reads
	send := make(chan []byte, N)
	errs := make(chan error, N)
	go p(t.Context(), send, errs)
	drainErrs(errs)

	// Wait for listener to be ready
	waitForTCP(t, addr)

	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			body := fmt.Sprintf(`{"event":%d}`, i)
			// Send raw HTTP request via TCP (each on its own connection)
			headers := map[string]string{
				"Content-Type": "application/json",
			}
			sendRawHTTPRequest(t, addr, "POST", "/hook", headers, body)
		}()
	}
	wg.Wait()

	msgs := readN(t, send, N, 5*time.Second)
	// Verify that messages are raw HTTP bytes (contain request line and headers)
	for _, msg := range msgs {
		msgStr := msg
		if !strings.Contains(msgStr, "POST /hook HTTP/1.1") {
			t.Errorf("message does not contain request line: %q", msgStr[:min(80, len(msgStr))])
		}
		if !strings.Contains(msgStr, "Content-Type: application/json") {
			t.Errorf("message missing Content-Type header: %q", msgStr)
		}
	}
	if len(msgs) != N {
		t.Errorf("got %d messages, want %d", len(msgs), N)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// sendRawHTTPRequest sends a raw HTTP request via TCP.
// The request is sent as complete wire format and connection is closed after.
func sendRawHTTPRequest(t *testing.T, addr, method, path string, headers map[string]string, body string) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	// Build raw HTTP request with explicit Content-Length for body
	headerLine := fmt.Sprintf("%s %s HTTP/1.1\r\nHost: %s\r\n", method, path, addr)
	for k, v := range headers {
		headerLine += fmt.Sprintf("%s: %s\r\n", k, v)
	}
	// Add Content-Length header
	headerLine += fmt.Sprintf("Content-Length: %d\r\n", len(body))
	// Blank line separates headers from body
	headerLine += "\r\n"

	req := headerLine + body

	// Send entire request
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Connection closes automatically on return, which signals end of request to the server
}

// TestHTTPWebhookBodyCap verifies raw TCP socket reception with variable body sizes.
func TestHTTPWebhookBodyCap(t *testing.T) {
	addr := freePort(t)
	tcpAddr := "tcp://" + addr

	p, err := produce.Listen(parser(map[string]any{
		"location": tcpAddr,
		"sep":      "\x00",
	}))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	send := make(chan []byte, 2)
	errs := make(chan error, 1)
	go p(t.Context(), send, errs)
	drainErrs(errs)

	waitForTCP(t, addr)

	// Send a small request
	sendRawHTTPRequest(t, addr, "POST", "/ingest", map[string]string{"Content-Type": "text/plain"}, "okay")

	// Send a larger request
	sendRawHTTPRequest(t, addr, "POST", "/ingest", map[string]string{"Content-Type": "text/plain"}, "this body is definitely over eight bytes")

	msgs := readN(t, send, 2, 2*time.Second)
	// Both messages should be received as raw HTTP bytes
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages, got %d", len(msgs))
	}
	// First message contains "okay"
	if !strings.Contains(msgs[0], "okay") {
		t.Errorf("body not found in message: %q", msgs[0])
	}
	// Second message contains larger body
	if !strings.Contains(msgs[1], "this body is definitely") {
		t.Errorf("large body not found in message: %q", msgs[1])
	}
}

// TestHTTPWebhookMethodGating verifies that raw TCP accepts all HTTP methods.
// TCP transport doesn't filter by HTTP verb — that's for downstream transformers to handle.
func TestHTTPWebhookMethodGating(t *testing.T) {
	addr := freePort(t)
	tcpAddr := "tcp://" + addr

	p, err := produce.Listen(parser(map[string]any{
		"location": tcpAddr,
		"sep":      "\x00",
	}))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	send := make(chan []byte, 4)
	errs := make(chan error, 1)
	go p(t.Context(), send, errs)
	drainErrs(errs)

	waitForTCP(t, addr)

	// A GET request (no body)
	sendRawHTTPRequest(t, addr, "GET", "/ingest", map[string]string{}, "")

	// A POST request with body
	sendRawHTTPRequest(t, addr, "POST", "/ingest", map[string]string{"Content-Type": "text/plain"}, "ok")

	msgs := readN(t, send, 2, 2*time.Second)
	// Both GET and POST are received as raw HTTP bytes
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages, got %d", len(msgs))
	}
	// First message is GET
	if !strings.Contains(msgs[0], "GET /ingest") {
		t.Errorf("GET request not found: %q", msgs[0])
	}
	// Second message is POST with body
	if !strings.Contains(msgs[1], "POST /ingest") {
		t.Errorf("POST request not found: %q", msgs[1])
	}
	if !strings.Contains(msgs[1], "ok") {
		t.Errorf("body not found in POST message: %q", msgs[1])
	}
}

// TestHTTPRawBytes verifies that produce-listen emits raw HTTP request bytes
// (request line + headers + body) from TCP socket.
func TestHTTPRawBytes(t *testing.T) {
	addr := freePort(t)
	tcpAddr := "tcp://" + addr

	p, err := produce.Listen(parser(map[string]any{
		"location": tcpAddr,
		"sep":      "\x00",
	}))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	send := make(chan []byte, 5)
	errs := make(chan error, 1)
	go p(t.Context(), send, errs)
	drainErrs(errs)

	waitForTCP(t, addr)

	// POST with query params and custom headers
	body := "hello world"
	headers := map[string]string{
		"Authorization": "Bearer token123",
		"Content-Type":  "text/plain",
	}
	// Note: sendRawHTTPRequest doesn't encode query params directly, so we pass them in path
	sendRawHTTPRequest(t, addr, "POST", "/api/jobs?timeout=30s&priority=high", headers, body)

	msgs := readN(t, send, 1, 2*time.Second)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}

	// Verify raw HTTP format: request line, headers, blank line, body
	msgStr := msgs[0]
	if !strings.Contains(msgStr, "POST /api/jobs?timeout=30s&priority=high HTTP/1.1") {
		t.Errorf("message does not contain correct request line: %q", msgStr[:min(80, len(msgStr))])
	}
	if !strings.Contains(msgStr, "Authorization: Bearer token123") {
		t.Errorf("message missing Authorization header: %q", msgStr)
	}
	if !strings.Contains(msgStr, "Content-Type: text/plain") {
		t.Errorf("message missing Content-Type header: %q", msgStr)
	}
	if !strings.Contains(msgStr, "hello world") {
		t.Errorf("message does not contain correct body: %q", msgStr[len(msgStr)-20:])
	}
}

// TestParseHTTPRequest verifies that parse-http-request transformer
// correctly parses raw HTTP bytes into structured JSON.
func TestParseHTTPRequest(t *testing.T) {
	addr := freePort(t)
	tcpAddr := "tcp://" + addr

	// Set up produce.Listen to emit raw HTTP bytes
	p, err := produce.Listen(parser(map[string]any{
		"location": tcpAddr,
		"sep":      "\x00",
	}))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	send := make(chan []byte, 5)
	errs := make(chan error, 1)
	go p(t.Context(), send, errs)
	drainErrs(errs)

	waitForTCP(t, addr)

	// Send a request with query params and headers
	body := "test payload"
	headers := map[string]string{
		"Authorization": "Bearer abc123",
		"Content-Type":  "application/json",
	}
	sendRawHTTPRequest(t, addr, "POST", "/api/jobs?key=value&foo=bar", headers, body)

	msgs := readN(t, send, 1, 2*time.Second)
	rawMsg := []byte(msgs[0])

	// Test transformer: wire up the parse-http-request transformer
	transformer, err := transform.ParseHTTPRequest(parser(map[string]any{}))
	if err != nil {
		t.Fatalf("ParseHTTPRequest: %v", err)
	}

	inChan := make(chan []byte, 1)
	outChan := make(chan []byte, 1)
	errChan := make(chan error, 1)

	go transformer(t.Context(), inChan, outChan, errChan)
	inChan <- rawMsg
	close(inChan)

	// Read the parsed result
	select {
	case parsedJSON := <-outChan:
		parsed := &transform.ParsedHTTPRequest{}
		if err := json.Unmarshal(parsedJSON, parsed); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}

		// Verify parsed fields
		if parsed.Method != "POST" {
			t.Errorf("method = %q, want POST", parsed.Method)
		}
		if parsed.Path != "/api/jobs" {
			t.Errorf("path = %q, want /api/jobs", parsed.Path)
		}
		if parsed.Query["key"] != "value" {
			t.Errorf("query[key] = %q, want value", parsed.Query["key"])
		}
		if parsed.Query["foo"] != "bar" {
			t.Errorf("query[foo] = %q, want bar", parsed.Query["foo"])
		}
		if strings.ToLower(parsed.Headers["authorization"]) != "bearer abc123" {
			t.Errorf("authorization header = %q, want bearer abc123", parsed.Headers["authorization"])
		}
		if strings.ToLower(parsed.Headers["content-type"]) != "application/json" {
			t.Errorf("content-type header = %q, want application/json", parsed.Headers["content-type"])
		}
		// Body should be base64-encoded
		if parsed.Body == "" {
			t.Errorf("body is empty")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for parsed output")
	}
}
