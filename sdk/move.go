package sdk

import (
	"time"
)

const EACH_MINUTE = 60_000

type PipelineConfig struct {
	PerMinute   int  `psy:"per-minute"`
	ExitOnError bool `psy:"exit-on-error"`
}

func specPipelineConfig() SpecMap {
	return SpecMap{
		"per-minute":    SpecPerMinute(180),
		"exit-on-error": SpecExitOnError(true),
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
func ProduceChunk(next func() ([]byte, bool, error), parse SpecParser, data chan []byte, errors chan error, signal Signal) {
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
func ConsumeChunk(next func([]byte) (bool, error), parse SpecParser, data chan []byte, errors chan error, signal Signal) {
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
