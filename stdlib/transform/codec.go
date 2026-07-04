package transform

import (
	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib/data"
)

// codecTransformer is the shared skeleton for every codec-aware transformer.
// It decodes the input per `decode`, runs op over the resulting Value, and
// encodes the result per `encode`. op returning a nil Value drops the message.
// `onError` ("err" | "drop") decides what happens when any stage fails.
func codecTransformer(decode, encode, onError string, op func(data.Value) (data.Value, error)) (sdk.Transformer, error) {
	if decode == "" {
		decode = "json"
	}
	if encode == "" {
		encode = decode
	}
	mode, err := data.ParseErrMode(onError)
	if err != nil {
		return nil, err
	}

	fail := func(err error) ([]byte, error) {
		if mode == data.ErrDrop {
			return nil, nil
		}
		return nil, err
	}

	return func(in []byte) ([]byte, error) {
		v, err := data.Decode(in, decode)
		if err != nil {
			return fail(err)
		}
		out, err := op(v)
		if err != nil {
			return fail(err)
		}
		if out == nil {
			return nil, nil // op chose to drop
		}
		b, err := data.Encode(out, encode)
		if err != nil {
			return fail(err)
		}
		return b, nil
	}, nil
}
