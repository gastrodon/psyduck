package transform

import (
	"bytes"
	"context"
	"fmt"
	"text/template"

	"github.com/psyduck-etl/sdk"
	"github.com/psyduck-etl/sdk/data"

	"github.com/gastrodon/psyduck/stdlib/transport"
)

// Request is the transformer half of the triple-role `request` resource: it
// turns "list of identifiers" into "list of identifiers, each fetched" in one
// stage. Each input message is decoded per `decode`, then url/body/header
// values are rendered as Go templates against the decoded value (the same
// engine `render`'s "template" mode uses — the decoded message is the dot),
// the request is issued, and the response body is emitted downstream for a
// `jq`/`pick`/`pick-map` stage to reshape. A template with no `{{ }}` actions
// renders unchanged, so a static url/body/header still works.
//
// interval-ms is a producer-only attribute and rejected here. Requests run
// one at a time, in input order, same as any other transformer stage.
func Request(ctx context.Context, parse sdk.Parser) (sdk.Transformer, error) {
	config := new(transport.RequestConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	if config.IntervalMs != 0 {
		return nil, fmt.Errorf("request transform: interval-ms is a producer-only attribute")
	}

	urlTmpl, err := parseRequestTemplate("url", config.URL)
	if err != nil {
		return nil, err
	}
	var bodyTmpl *template.Template
	if config.Body != "" {
		if bodyTmpl, err = parseRequestTemplate("body", config.Body); err != nil {
			return nil, err
		}
	}
	headerTmpls := make(map[string]*template.Template, len(config.Headers))
	for k, v := range config.Headers {
		t, err := parseRequestTemplate("header "+k, v)
		if err != nil {
			return nil, err
		}
		headerTmpls[k] = t
	}

	decode := config.Decode
	if decode == "" {
		decode = "bytes"
	}
	onError, err := data.ParseOnError(config.OnError)
	if err != nil {
		return nil, err
	}

	h := config.HTTP()
	client := h.Client()

	return mapErrContext(onError, func(ctx context.Context, msg []byte) ([]byte, error) {
		v, err := data.Decode(msg, decode)
		if err != nil {
			return nil, err
		}
		dot := data.Native(v)

		url, err := execRequestTemplate(urlTmpl, dot)
		if err != nil {
			return nil, fmt.Errorf("request transform: render url: %w", err)
		}

		var body []byte
		if bodyTmpl != nil {
			rendered, err := execRequestTemplate(bodyTmpl, dot)
			if err != nil {
				return nil, fmt.Errorf("request transform: render body: %w", err)
			}
			body = []byte(rendered)
		}

		req := h
		req.URL = url
		if len(headerTmpls) > 0 {
			headers := make(map[string]string, len(headerTmpls))
			for k, t := range headerTmpls {
				rendered, err := execRequestTemplate(t, dot)
				if err != nil {
					return nil, fmt.Errorf("request transform: render header %q: %w", k, err)
				}
				headers[k] = rendered
			}
			req.Headers = headers
		}

		return req.Do(ctx, client, body)
	}), nil
}

func parseRequestTemplate(name, format string) (*template.Template, error) {
	tmpl, err := template.New(name).Parse(format)
	if err != nil {
		return nil, fmt.Errorf("request transform: parse %s template: %w", name, err)
	}
	return tmpl, nil
}

func execRequestTemplate(t *template.Template, dot any) (string, error) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, dot); err != nil {
		return "", err
	}
	return buf.String(), nil
}
