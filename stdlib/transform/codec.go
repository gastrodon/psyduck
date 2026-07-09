package transform

import (
	"context"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib/data"
)

// mapTransform adapts a per-message function into a channel-based
// sdk.Transformer: it reads in one message at a time, applies f, and writes
// any non-nil result to out (a nil result filters the message out),
// closing out when in closes or ctx ends. Every stdlib transformer built
// from mapTransform is therefore single-threaded internally, same as the
// engine's old per-message call.
func mapTransform(f func(in []byte) ([]byte, error)) sdk.Transformer {
	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		for {
			select {
			case msg, ok := <-in:
				if !ok {
					return
				}
				transformed, err := f(msg)
				if err != nil {
					select {
					case errs <- err:
					case <-ctx.Done():
						return
					}
					continue
				}
				if transformed == nil {
					continue
				}
				select {
				case out <- transformed:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}
}

// codecTransformer is the shared skeleton for every codec-aware transformer.
// It decodes the input per `decode`, runs op over the resulting Value, and
// encodes the result per `encode`. op returning a nil Value drops the message.
// onError decides what happens when any stage fails; a nil onError defaults
// to data.Raise.
func codecTransformer(decode, encode string, onError data.OnError, op func(data.Value) (data.Value, error)) sdk.Transformer {
	if decode == "" {
		decode = "json"
	}
	if encode == "" {
		encode = decode
	}
	if onError == nil {
		onError = data.Raise
	}

	fail := func(err error) ([]byte, error) { return nil, onError(err) }

	return mapTransform(func(in []byte) ([]byte, error) {
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
	})
}
