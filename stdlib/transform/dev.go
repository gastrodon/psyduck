package transform

import (
	"fmt"
	"strconv"
	"sync/atomic"

	"github.com/psyduck-etl/sdk"
	"github.com/psyduck-etl/sdk/data"
)

type assertConfig struct {
	Expression string `psy:"expression"`
	Message    string `psy:"message"`
}

// Assert validates each message against a jq predicate, erroring (not dropping)
// when the predicate is false or null. The message passes through unchanged on
// success. Useful for tripwires in a pipeline under test.
func Assert(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(assertConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	query, err := data.CompileJQ(config.Expression)
	if err != nil {
		return nil, fmt.Errorf("assert: parse %q: %w", config.Expression, err)
	}
	msg := config.Message
	if msg == "" {
		msg = "assertion failed"
	}

	return sdk.Map(func(in []byte) ([]byte, error) {
		v, err := data.Decode(in, "json")
		if err != nil {
			return nil, err
		}
		got, ok, err := data.EvalJQ(query, v)
		if err != nil {
			return nil, err
		}
		if !ok || falsey(got) {
			return nil, fmt.Errorf("%s: %s", msg, config.Expression)
		}
		return in, nil
	}), nil
}

func falsey(v data.Value) bool {
	if lit, ok := v.(data.Lit); ok {
		switch t := lit.V.(type) {
		case nil:
			return true
		case bool:
			return !t
		}
	}
	return false
}

type countConfig struct {
	Every  int    `psy:"every"`
	Prefix string `psy:"prefix"`
}

// Count tallies messages and, every `every` messages (default 1), replaces the
// message with the running count (optionally prefixed). Between checkpoints the
// message passes through unchanged.
func Count(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(countConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	every := config.Every
	if every <= 0 {
		every = 1
	}

	// n is shared across every invocation, so the tally is global: the count
	// spans all parallel callers. atomic.Add keeps the increment race-free and
	// is cheaper than a mutex for a bare counter.
	var n atomic.Uint64

	return sdk.Map(func(msg []byte) ([]byte, error) {
		cur := n.Add(1)
		if cur%uint64(every) == 0 {
			return []byte(config.Prefix + strconv.FormatUint(cur, 10)), nil
		}
		return msg, nil
	}), nil
}
