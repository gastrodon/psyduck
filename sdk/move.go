package sdk

import (
	"time"

	"github.com/zclconf/go-cty/cty"
)

const EACH_MINUTE = 60_000

type PipelineConfig struct {
	PerMinute   int  `cty:"per-minute"`
	ExitOnError bool `cty:"exit-on-error"`
}

func specPipelineConfig() SpecMap {
	return SpecMap{
		"per-minute": &Spec{
			Name:        "per-minute",
			Description: "target producing/consuming n items per minute ( or 0 for unrestricted )",
			Type:        cty.Number,
			Required:    false,
			Default:     cty.NumberIntVal(0),
		},
		"exit-on-error": &Spec{
			Name:        "exit-on-error",
			Description: "stop producing/consuming if we encounter an error",
			Type:        cty.Bool,
			Required:    false,
			Default:     cty.BoolVal(true),
		},
	}
}

func mustParse(parse SpecParser) *PipelineConfig {
	config := new(PipelineConfig)
	if err := parse(specPipelineConfig(), config); err != nil {
		panic(err)
	}

	return config
}

func ratelimit(perMinute int) {
	if perMinute == 0 {
		return
	}

	time.Sleep(time.Millisecond * time.Duration(EACH_MINUTE/perMinute))
}

// Produce data returned from successive calls to next
func ProduceChunk(next func() ([]byte, bool, error), parse SpecParser, data chan []byte, errors chan error, signal chan string) {
	config := mustParse(parse)
	alive := make(chan bool, 1)
	alive <- true

	for {
		dataNext, more, err := next()
		if err != nil {
			errors <- err

			if config.ExitOnError {
				return
			}

			continue
		}

		if !more {
			return
		}

		data <- dataNext

		select {
		case received := <-signal:
			panic(received)
		case <-alive:
			alive <- true
			ratelimit(config.PerMinute)
		}
	}
}

// Consume data streamed and call next on it
func ConsumeChunk(next func([]byte) (bool, error), parse SpecParser, data chan []byte, errors chan error, signal chan string) {
	config := mustParse(parse)

	for {
		select {
		case received := <-signal:
			panic(received)
		case dataNext := <-data:
			more, err := next(dataNext)
			if err != nil {
				errors <- err

				if config.ExitOnError {
					return
				}

				continue
			}

			if !more {
				return
			}
		}

		ratelimit(config.PerMinute)
	}
}
