package transform

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"text/template"

	"github.com/psyduck-etl/sdk"
	"github.com/psyduck-etl/sdk/data"

	"github.com/gastrodon/psyduck/stdlib/transport"
)

// fetchConfig decodes into the same transport.RequestConfig the request
// producer/consumer use — method, headers, body, query-params, basic-auth,
// timeout-ms, and success-codes all carry over unchanged. interval-ms is a
// producer-only polling knob and isn't part of this resource's spec at all,
// so it's simply never set here. url, body, and header values are templates
// rather than literals: each is rendered per message, with the decoded
// message as the template's dot.
type fetchConfig struct {
	transport.RequestConfig
	Decode   string `psy:"decode"`
	OnError  string `psy:"on-error"`
	Parallel int    `psy:"parallel"`
}

// Fetch is the transformer half of an HTTP-pull round trip: decode each
// input message, template it into a request (url, and optionally body and
// headers), perform the call, and emit the response body as the transformed
// message — turning "list of identifiers" into "list of identifiers, each
// replaced by what fetching it returned" for a downstream jq/pick stage to
// reshape. It differs from the request producer only in when the call
// happens (once per input message, not polled) and in templating the
// request from that message.
//
// parallel runs that many copies of the fetch concurrently, all pulling from
// the same input — greedy fan-out, unordered output, the same shape
// docs/hcl.md describes for a pipeline's own concurrency knobs. It defaults
// to 1 (one request in flight at a time); psyduck's transformer stage never
// runs a resource's Transform more than once itself, so this is the only
// place that concurrency can come from for a per-message HTTP call.
func Fetch(ctx context.Context, parse sdk.Parser) (sdk.Transformer, error) {
	config := new(fetchConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	if config.Decode == "" {
		config.Decode = "bytes"
	}
	onError, err := data.ParseOnError(config.OnError)
	if err != nil {
		return nil, err
	}

	urlTmpl, err := template.New("fetch-url").Parse(config.URL)
	if err != nil {
		return nil, fmt.Errorf("fetch: parse url template: %w", err)
	}

	var bodyTmpl *template.Template
	if config.Body != "" {
		bodyTmpl, err = template.New("fetch-body").Parse(config.Body)
		if err != nil {
			return nil, fmt.Errorf("fetch: parse body template: %w", err)
		}
	}

	headerTmpls := make(map[string]*template.Template, len(config.Headers))
	for name, format := range config.Headers {
		tmpl, err := template.New("fetch-header").Parse(format)
		if err != nil {
			return nil, fmt.Errorf("fetch: parse header %q template: %w", name, err)
		}
		headerTmpls[name] = tmpl
	}

	base := config.HTTP()
	client := base.Client()

	parallel := config.Parallel
	if parallel <= 0 {
		parallel = 1
	}

	do := func(ctx context.Context, msg []byte) ([]byte, error) {
		v, err := data.Decode(msg, config.Decode)
		if err != nil {
			return nil, fmt.Errorf("fetch: decode: %w", err)
		}
		dot := data.Native(v)

		url, err := renderTemplate(urlTmpl, dot)
		if err != nil {
			return nil, fmt.Errorf("fetch: render url: %w", err)
		}

		h := base
		h.URL = url
		if len(headerTmpls) > 0 {
			headers := make(map[string]string, len(headerTmpls))
			for name, tmpl := range headerTmpls {
				rendered, err := renderTemplate(tmpl, dot)
				if err != nil {
					return nil, fmt.Errorf("fetch: render header %q: %w", name, err)
				}
				headers[name] = rendered
			}
			h.Headers = headers
		}

		var body []byte
		if bodyTmpl != nil {
			rendered, err := renderTemplate(bodyTmpl, dot)
			if err != nil {
				return nil, fmt.Errorf("fetch: render body: %w", err)
			}
			body = []byte(rendered)
		}

		return h.Do(ctx, client, body)
	}

	return fanOut(parallel, func(ctx context.Context, msg []byte) ([]byte, error) {
		out, err := do(ctx, msg)
		if err == nil {
			return out, nil
		}
		if err = onError(err); err != nil {
			return nil, err
		}
		return nil, nil
	}), nil
}

func renderTemplate(tmpl *template.Template, dot any) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, dot); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// fanOut runs n concurrent copies of fn, all reading from the same in
// channel and writing to the same out channel — the shared-input, greedy,
// unordered-output shape a "parallel" transform is supposed to have. It's
// sdk.MapContext generalized to n workers: fn returning (nil, nil) drops the
// message, and an error is forwarded on errs without stopping the stage.
func fanOut(n int, fn func(context.Context, []byte) ([]byte, error)) sdk.Transformer {
	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)

		var wg sync.WaitGroup
		wg.Add(n)
		for range n {
			go func() {
				defer wg.Done()
				for {
					select {
					case msg, ok := <-in:
						if !ok {
							return
						}
						result, err := fn(ctx, msg)
						if err != nil {
							select {
							case errs <- err:
							case <-ctx.Done():
								return
							}
							continue
						}
						if result == nil {
							continue
						}
						select {
						case out <- result:
						case <-ctx.Done():
							return
						}
					case <-ctx.Done():
						return
					}
				}
			}()
		}
		wg.Wait()
	}
}
