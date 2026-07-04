package transform

import (
	"sync"
	"time"

	"github.com/psyduck-etl/sdk"
)

// This file holds the mid-pipeline flow-control transformers. They pace or
// prune a message stream from a transform stage; the host-owned per-minute /
// stop-after meta apply the same idea at the block level.

type waitConfig struct {
	Milliseconds int `psy:"milliseconds"`
}

// Wait sleeps a fixed duration before passing each message through — a simple
// throttle/spacer.
func Wait(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(waitConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	d := time.Duration(config.Milliseconds) * time.Millisecond
	return func(in []byte) ([]byte, error) {
		time.Sleep(d)
		return in, nil
	}, nil
}

type throttleConfig struct {
	PerSecond int `psy:"per-second"`
}

// Throttle rate-limits the stream to per-second messages, blocking as needed.
func Throttle(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(throttleConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	if config.PerSecond <= 0 {
		return func(in []byte) ([]byte, error) { return in, nil }, nil
	}
	tick := time.NewTicker(time.Second / time.Duration(config.PerSecond))
	return func(in []byte) ([]byte, error) {
		<-tick.C
		return in, nil
	}, nil
}

type headConfig struct {
	Count int `psy:"count"`
}

// Head passes the first count messages through and drops the rest.
func Head(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(headConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	var mu sync.Mutex
	seen := 0
	return func(in []byte) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		if seen >= config.Count {
			return nil, nil
		}
		seen++
		return in, nil
	}, nil
}

type tailConfig struct {
	Skip int `psy:"skip"`
}

// Tail drops the first skip messages and passes the rest through.
func Tail(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(tailConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	var mu sync.Mutex
	seen := 0
	return func(in []byte) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		if seen < config.Skip {
			seen++
			return nil, nil
		}
		return in, nil
	}, nil
}

type sampleConfig struct {
	Rate int `psy:"rate"`
}

// Sample keeps one message in every `rate` (statistical downsampling).
func Sample(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(sampleConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	rate := config.Rate
	if rate <= 1 {
		return func(in []byte) ([]byte, error) { return in, nil }, nil
	}
	var mu sync.Mutex
	n := 0
	return func(in []byte) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		keep := n%rate == 0
		n++
		if keep {
			return in, nil
		}
		return nil, nil
	}, nil
}
