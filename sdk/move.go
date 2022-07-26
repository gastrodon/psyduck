package sdk

import (
	"time"
)

const EACH_MINUTE = 60_000

type PipelineConfig struct {
	PerMinute int `psy:"per_minute"`
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
func ProduceChunk(next func() ([]byte, bool), parse func(interface{}) error, data chan []byte, signal chan string) {
	config := mustParse(parse)
	alive := make(chan bool, 1)
	alive <- true

	for {
		dataNext, hasNext := next()
		if !hasNext {
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
func ConsumeChunk(next func([]byte) bool, parse func(interface{}) error, data chan []byte, signal chan string) {
	config := mustParse(parse)

	for {
		select {
		case received := <-signal:
			panic(received)
		case dataNext := <-data:
			if !next(dataNext) {
				return
			}
		}

		ratelimit(config.PerMinute)
	}
}
