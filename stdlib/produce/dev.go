package produce

import (
	"strconv"
	"time"

	"github.com/psyduck-etl/sdk"
)

type sequenceConfig struct {
	Start     int `psy:"start"`
	Step      int `psy:"step"`
	StopAfter int `psy:"stop-after"`
}

// Sequence emits an arithmetic sequence of integers as decimal strings:
// start, start+step, start+2*step, … It supersedes the old increment producer.
func Sequence(parse sdk.Parser) (sdk.Producer, error) {
	config := new(sequenceConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	step := config.Step
	if step == 0 {
		step = 1
	}

	return func(send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)

		n := config.Start
		for i := 0; config.StopAfter == 0 || i < config.StopAfter; i++ {
			send <- []byte(strconv.Itoa(n))
			n += step
		}
	}, nil
}

type generateConfig struct {
	Values    []string `psy:"values"`
	Loop      bool     `psy:"loop"`
	StopAfter int      `psy:"stop-after"`
}

// Generate emits a fixed list of literal values in order. With loop=true it
// cycles forever (bounded by stop-after or host-owned meta).
func Generate(parse sdk.Parser) (sdk.Producer, error) {
	config := new(generateConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)

		emitted := 0
		for {
			for _, v := range config.Values {
				send <- []byte(v)
				emitted++
				if config.StopAfter > 0 && emitted >= config.StopAfter {
					return
				}
			}
			if !config.Loop {
				return
			}
		}
	}, nil
}

type tickerConfig struct {
	IntervalMs int    `psy:"interval-ms"`
	Format     string `psy:"format"`
	StopAfter  int    `psy:"stop-after"`
}

// Ticker emits the current timestamp at a fixed interval. Format is one of
// "unix", "unix-ms", or "rfc3339".
func Ticker(parse sdk.Parser) (sdk.Producer, error) {
	config := new(tickerConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	interval := time.Duration(config.IntervalMs) * time.Millisecond
	if interval <= 0 {
		interval = time.Second
	}
	format := config.Format
	if format == "" {
		format = "unix-ms"
	}

	return func(send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)

		tick := time.NewTicker(interval)
		defer tick.Stop()

		for i := 0; config.StopAfter == 0 || i < config.StopAfter; i++ {
			now := time.Now()
			send <- []byte(formatTime(now, format))
			<-tick.C
		}
	}, nil
}

func formatTime(t time.Time, format string) string {
	switch format {
	case "unix":
		return strconv.FormatInt(t.Unix(), 10)
	case "rfc3339":
		return t.Format(time.RFC3339)
	default: // unix-ms
		return strconv.FormatInt(t.UnixMilli(), 10)
	}
}
