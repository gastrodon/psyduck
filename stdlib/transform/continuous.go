package transform

import (
	"context"
	"fmt"
	"math"

	"github.com/psyduck-etl/sdk"
	"github.com/psyduck-etl/sdk/data"
)

func asContinuous(v data.Value, who string) (data.Continuous, error) {
	c, ok := v.(data.Continuous)
	if !ok {
		return nil, fmt.Errorf("%s: want continuous data, got %s", who, v.Kind())
	}
	return c, nil
}

// ── slice ──────────────────────────────────────────────────────────────────

type sliceConfig struct {
	Start   int    `psy:"start"`
	Stop    int    `psy:"stop"`
	Step    int    `psy:"step"`
	Decode  string `psy:"decode"`
	Encode  string `psy:"encode"`
	OnError string `psy:"on-error"`
}

// Slice extracts a sub-range of continuous data (bytes, string, or list).
// start/stop are plain offsets, not indices relative to a calculable end: a
// Continuous is not assumed to know its length up front, so there is no
// negative "count from the end" and stop is never auto-filled from one. A
// stop of 0 or below means "through the end" — Slice clamps out-of-range
// bounds rather than erroring, so any sufficiently large stop has the same
// effect as an explicit length.
func Slice(ctx context.Context, parse sdk.Parser) (sdk.Transformer, error) {
	config := new(sliceConfig)
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

	stop := config.Stop
	if stop <= 0 {
		stop = math.MaxInt
	}

	op := func(v data.Value) (data.Value, error) {
		c, err := asContinuous(v, "slice")
		if err != nil {
			return nil, err
		}
		out, _ := c.Slice(config.Start, stop, config.Step)
		return out, nil
	}
	return codecTransformer(config.Decode, config.Encode, onError, op), nil
}

// ── chunk ──────────────────────────────────────────────────────────────────

type chunkConfig struct {
	Size     int    `psy:"size"`
	KeepTail bool   `psy:"keep-tail"`
	Decode   string `psy:"decode"`
	Encode   string `psy:"encode"`
	OnError  string `psy:"on-error"`
}

// Chunk splits continuous data into size-length windows and emits each window
// as a separate message. It is a true 1→N transformer: one input yields multiple
// outputs. Each window is encoded per the encode setting.
func Chunk(ctx context.Context, parse sdk.Parser) (sdk.Transformer, error) {
	config := new(chunkConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	if config.Decode == "" {
		config.Decode = "bytes"
	}
	if config.Encode == "" {
		config.Encode = "bytes"
	}
	onError, err := data.ParseOnError(config.OnError)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		emitErr := func(err error) (stop bool) {
			select {
			case errs <- err:
				return false
			case <-ctx.Done():
				return true
			}
		}
		for {
			select {
			case msg, ok := <-in:
				if !ok {
					return
				}
				// Decode the input message
				v, err := data.Decode(msg, config.Decode)
				if err != nil {
					if emitErr(fmt.Errorf("chunk: decode: %w", err)) {
						return
					}
					continue
				}
				// Get continuous data
				c, err := asContinuous(v, "chunk")
				if err != nil {
					if emitErr(fmt.Errorf("chunk: %w", err)) {
						return
					}
					continue
				}
				// Chunk it and emit each window as a separate message
				chunks := c.Chunk(config.Size, config.KeepTail)
				for _, chunk := range chunks {
					encoded, err := data.Encode(chunk, config.Encode)
					if err != nil {
						if emitErr(fmt.Errorf("chunk: encode: %w", err)) {
							return
						}
						continue
					}
					select {
					case out <- encoded:
					case <-ctx.Done():
						return
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}, nil
}

// ── every ──────────────────────────────────────────────────────────────────

type everyConfig struct {
	Step    int    `psy:"step"`
	Size    int    `psy:"size"`
	Decode  string `psy:"decode"`
	Encode  string `psy:"encode"`
	OnError string `psy:"on-error"`
}

// Every emits size-length sliding windows advanced by step. It is a true 1→N
// transformer: one input yields multiple outputs. Each window is encoded per
// the encode setting.
func Every(ctx context.Context, parse sdk.Parser) (sdk.Transformer, error) {
	config := new(everyConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	if config.Decode == "" {
		config.Decode = "bytes"
	}
	if config.Encode == "" {
		config.Encode = "bytes"
	}
	onError, err := data.ParseOnError(config.OnError)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		emitErr := func(err error) (stop bool) {
			select {
			case errs <- err:
				return false
			case <-ctx.Done():
				return true
			}
		}
		for {
			select {
			case msg, ok := <-in:
				if !ok {
					return
				}
				// Decode the input message
				v, err := data.Decode(msg, config.Decode)
				if err != nil {
					if emitErr(fmt.Errorf("every: decode: %w", err)) {
						return
					}
					continue
				}
				// Get continuous data
				c, err := asContinuous(v, "every")
				if err != nil {
					if emitErr(fmt.Errorf("every: %w", err)) {
						return
					}
					continue
				}
				// Emit each sliding window as a separate message
				everies := c.Every(config.Step, config.Size)
				for _, every := range everies {
					encoded, err := data.Encode(every, config.Encode)
					if err != nil {
						if emitErr(fmt.Errorf("every: encode: %w", err)) {
							return
						}
						continue
					}
					select {
					case out <- encoded:
					case <-ctx.Done():
						return
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}, nil
}

