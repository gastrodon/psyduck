package transform

import (
	"fmt"
	"strconv"
	"sync"

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

	return func(in []byte) ([]byte, error) {
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
	}, nil
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

	var mu sync.Mutex
	n := 0

	return func(in []byte) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		n++
		if n%every != 0 {
			return in, nil
		}
		return []byte(config.Prefix + strconv.Itoa(n)), nil
	}, nil
}
