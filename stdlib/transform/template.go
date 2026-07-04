package transform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"text/template"

	"github.com/psyduck-etl/sdk"
)

type templateConfig struct {
	Format string `psy:"format"`
}

// Template renders a Go text/template string against the message.
// If the message is valid JSON it is decoded to a map first so that
// template expressions like {{.field}} work naturally.
func Template(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(templateConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	tmpl, err := template.New("").Parse(config.Format)
	if err != nil {
		return nil, fmt.Errorf("template: parse: %w", err)
	}

	return func(in []byte) ([]byte, error) {
		var data interface{}
		if err := json.Unmarshal(in, &data); err != nil {
			// not JSON — pass raw bytes as the template dot
			data = string(in)
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return nil, fmt.Errorf("template: execute: %w", err)
		}

		return buf.Bytes(), nil
	}, nil
}
