package sdk

import (
	"time"
)

const EACH_MINUTE = 60_000

type PipelineConfig struct {
	PerMinute   int  `psy:"per-minute"`
	ExitOnError bool `psy:"exit-on-error"`
}

func mustParse(parse func(interface{}) error) *PipelineConfig {
	config := &PipelineConfig{
		PerMinute: 120,
	}

	if err := parse(config); err != nil {
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
func ProduceChunk(next func() ([]byte, bool, error), parse func(interface{}) error, data chan []byte, errors chan error, signal chan string) {
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
func ConsumeChunk(next func([]byte) (bool, error), parse func(interface{}) error, data chan []byte, errors chan error, signal chan string) {
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
