package transform

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/psyduck-etl/sdk"
)

type parseHTTPRequestConfig struct {
	Decode  string `psy:"decode"`
	Encode  string `psy:"encode"`
	OnError string `psy:"on-error"`
}

// ParsedHTTPRequest is the output structure from parse-http-request transformer.
type ParsedHTTPRequest struct {
	Method     string            `json:"method"`
	Path       string            `json:"path"`
	Query      map[string]string `json:"query"`
	Headers    map[string]string `json:"headers"`
	RemoteAddr string            `json:"remote_addr"`
	Body       string            `json:"body"` // base64-encoded body
}

// ParseHTTPRequest parses raw HTTP request bytes into structured JSON.
// Input: raw HTTP request bytes (wire format: request line + headers + body).
// Output: JSON object with method, path, query, headers, remote_addr, and body (base64).
//
// This transformer enables composable HTTP handling: http-listen emits raw bytes,
// and downstream transformers extract specific fields as needed, similar to
// socket/tcp/udp transports.
func ParseHTTPRequest(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(parseHTTPRequestConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		defer close(errs)

		for {
			select {
			case msg, ok := <-in:
				if !ok {
					return
				}

				parsed, err := parseHTTPWireFormat(msg)
				if err != nil {
					errs <- fmt.Errorf("failed to parse HTTP request: %w", err)
					continue
				}

				// Encode to JSON
				jsonBytes, err := json.Marshal(parsed)
				if err != nil {
					errs <- fmt.Errorf("failed to marshal parsed request: %w", err)
					continue
				}

				select {
				case out <- jsonBytes:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}, nil
}

// parseHTTPWireFormat parses raw HTTP request bytes into a ParsedHTTPRequest struct.
// Expected format: "METHOD PATH HTTP/1.1\r\nHEADER: value\r\n...\r\n\r\nbody..."
func parseHTTPWireFormat(data []byte) (*ParsedHTTPRequest, error) {
	// Split headers from body at the blank line
	parts := bytes.SplitN(data, []byte("\r\n\r\n"), 2)
	if len(parts) == 0 {
		return nil, fmt.Errorf("invalid HTTP request format")
	}

	headerBytes := parts[0]
	bodyBytes := []byte{}
	if len(parts) > 1 {
		bodyBytes = parts[1]
	}

	// Parse request line and headers
	reader := bufio.NewReader(bytes.NewReader(headerBytes))

	// First line: METHOD PATH HTTP/VERSION
	requestLine, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to read request line: %w", err)
	}
	requestLine = strings.TrimSpace(requestLine)

	// Parse request line
	method, path, err := parseRequestLine(requestLine)
	if err != nil {
		return nil, err
	}

	// Parse path and query
	pathOnly := path
	queryParams := make(map[string]string)
	if idx := strings.IndexByte(path, '?'); idx >= 0 {
		pathOnly = path[:idx]
		queryStr := path[idx+1:]
		queryParams = parseQueryParams(queryStr)
	}

	// Parse headers
	headers := make(map[string]string)
	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("failed to read header: %w", err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}

		// Parse "Header: value"
		if idx := strings.Index(line, ":"); idx >= 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			// Normalize header keys to lowercase
			headers[strings.ToLower(key)] = value
		}
	}

	// Encode body as base64
	encodedBody := base64.StdEncoding.EncodeToString(bodyBytes)

	result := &ParsedHTTPRequest{
		Method:  method,
		Path:    pathOnly,
		Query:   queryParams,
		Headers: headers,
		Body:    encodedBody,
	}

	return result, nil
}

// parseRequestLine extracts method and path from "METHOD PATH HTTP/VERSION"
func parseRequestLine(line string) (string, string, error) {
	parts := strings.Fields(line)
	if len(parts) < 3 {
		return "", "", fmt.Errorf("invalid request line format: %s", line)
	}
	method := parts[0]
	path := parts[1]
	// parts[2] is HTTP version, ignore for now
	return method, path, nil
}

// parseQueryParams parses a query string into a map
func parseQueryParams(query string) map[string]string {
	result := make(map[string]string)
	if query == "" {
		return result
	}

	for _, pair := range strings.Split(query, "&") {
		if idx := strings.IndexByte(pair, '='); idx >= 0 {
			key := pair[:idx]
			value := pair[idx+1:]
			result[key] = value
		} else {
			result[pair] = ""
		}
	}
	return result
}
