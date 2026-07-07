package transform

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib/data"
)

type renderConfig struct {
	Engine  string `psy:"engine"`
	Format  string `psy:"format"`
	Decode  string `psy:"decode"`
	Encode  string `psy:"encode"`
	OnError string `psy:"on-error"`
}

// Render formats the message with a chosen engine, merging the old sprintf and
// template transformers behind one `engine` knob:
//
//   - "template": Go text/template, with the decoded message as the dot
//     ({{.field}} works when the message is a JSON object).
//   - "printf":   fmt.Sprintf; a list message spreads into the verbs, any other
//     value is a single argument.
//   - "jq":       a jq expression (the format string) evaluated over the message.
//
// The decoded input feeds every engine, so all three see structured data.
func Render(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(renderConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	if config.Encode == "" {
		config.Encode = "bytes"
	}

	engine := config.Engine
	if engine == "" {
		engine = "template"
	}

	var op func(data.Value) (data.Value, error)
	switch engine {
	case "template":
		tmpl, err := template.New("render").Parse(config.Format)
		if err != nil {
			return nil, fmt.Errorf("render: parse template: %w", err)
		}
		op = func(v data.Value) (data.Value, error) {
			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, data.Native(v)); err != nil {
				return nil, fmt.Errorf("render: template: %w", err)
			}
			return data.Str(buf.String()), nil
		}
	case "printf":
		op = func(v data.Value) (data.Value, error) {
			args := printfArgs(v)
			return data.Str(fmt.Sprintf(config.Format, args...)), nil
		}
	case "jq":
		query, err := data.CompileJQ(config.Format)
		if err != nil {
			return nil, fmt.Errorf("render: parse jq: %w", err)
		}
		op = func(v data.Value) (data.Value, error) {
			out, ok, err := data.EvalJQ(query, v)
			if err != nil || !ok {
				return nil, err
			}
			return out, nil
		}
	default:
		return nil, fmt.Errorf("render: unknown engine %q (want template, printf, or jq)", engine)
	}

	onError, err := data.ParseOnError(config.OnError)
	if err != nil {
		return nil, err
	}
	return codecTransformer(config.Decode, config.Encode, onError, op), nil
}

// printfArgs spreads a list message into separate verbs; any other value is a
// single argument.
func printfArgs(v data.Value) []any {
	if l, ok := v.(data.List); ok {
		args := make([]any, len(l))
		for i, e := range l {
			args[i] = data.Native(e)
		}
		return args
	}
	return []any{data.Native(v)}
}
