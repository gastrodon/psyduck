package transform

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/itchyny/gojq"
	"github.com/psyduck-etl/sdk"
)

type dedupeConfig struct {
	By     string `psy:"by"`
	Window int    `psy:"window"`
}

// Dedupe drops messages whose computed key (from the jq "by" expression) has
// been seen within the last "window" messages. The original message is always
// passed through unchanged — the jq expression is only used to compute the key.
func Dedupe(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(dedupeConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	if config.By == "" {
		config.By = "."
	}
	if config.Window <= 0 {
		config.Window = 10000
	}

	query, err := gojq.Parse(config.By)
	if err != nil {
		return nil, fmt.Errorf("dedupe: parse by expression %q: %w", config.By, err)
	}

	var mu sync.Mutex
	seen := make(map[string]struct{}, config.Window)
	ring := make([]string, 0, config.Window)

	return func(in []byte) ([]byte, error) {
		v, err := runJQ(query, in)
		if err != nil {
			return nil, err
		}

		// Compute a string key from the jq result.
		var key string
		switch val := v.(type) {
		case string:
			key = val
		case nil:
			key = "null"
		default:
			b, err := json.Marshal(val)
			if err != nil {
				return nil, fmt.Errorf("dedupe: marshal key: %w", err)
			}
			key = string(b)
		}

		mu.Lock()
		defer mu.Unlock()

		if _, dup := seen[key]; dup {
			return nil, nil // drop duplicate
		}

		// Add to ring buffer and evict oldest if full.
		if len(ring) >= config.Window {
			oldest := ring[0]
			ring = ring[1:]
			delete(seen, oldest)
		}

		seen[key] = struct{}{}
		ring = append(ring, key)
		return in, nil
	}, nil
}
