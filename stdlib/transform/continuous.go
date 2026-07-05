package transform

import (
	"fmt"
	"math"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib/data"
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
func Slice(parse sdk.Parser) (sdk.Transformer, error) {
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

// Chunk splits continuous data into size-length windows and emits them as a
// list (a transformer is 1→1, so the chunks arrive together as one message).
func Chunk(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(chunkConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	if config.Decode == "" {
		config.Decode = "bytes"
	}
	if config.Encode == "" {
		config.Encode = "json"
	}
	onError, err := data.ParseOnError(config.OnError)
	if err != nil {
		return nil, err
	}

	op := func(v data.Value) (data.Value, error) {
		c, err := asContinuous(v, "chunk")
		if err != nil {
			return nil, err
		}
		return windows(c.Chunk(config.Size, config.KeepTail)), nil
	}
	return codecTransformer(config.Decode, config.Encode, onError, op), nil
}

// ── every ──────────────────────────────────────────────────────────────────

type everyConfig struct {
	Step    int    `psy:"step"`
	Size    int    `psy:"size"`
	Decode  string `psy:"decode"`
	Encode  string `psy:"encode"`
	OnError string `psy:"on-error"`
}

// Every emits size-length sliding windows advanced by step, as a list.
func Every(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(everyConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	if config.Decode == "" {
		config.Decode = "bytes"
	}
	if config.Encode == "" {
		config.Encode = "json"
	}
	onError, err := data.ParseOnError(config.OnError)
	if err != nil {
		return nil, err
	}

	op := func(v data.Value) (data.Value, error) {
		c, err := asContinuous(v, "every")
		if err != nil {
			return nil, err
		}
		return windows(c.Every(config.Step, config.Size)), nil
	}
	return codecTransformer(config.Decode, config.Encode, onError, op), nil
}

// windows lifts a slice of continuous windows into a data.List.
func windows(cs []data.Continuous) data.List {
	list := make(data.List, len(cs))
	for i, c := range cs {
		list[i] = c
	}
	return list
}
