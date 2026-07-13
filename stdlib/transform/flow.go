package transform

import (
	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib/flow"
)

// These mid-pipeline flow-control transformers are thin config wrappers over
// the shared stdlib/flow combinators — the same code the core engine applies
// for host-owned per-minute meta (producers and consumers) and stop-after
// (producers only). Transform blocks accept no host-owned meta of their own;
// a pipeline wanting these behaviors mid-stream declares them explicitly here.

type waitConfig struct {
	Milliseconds int `psy:"milliseconds"`
}

// Wait sleeps a fixed duration before passing each message through.
func Wait(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(waitConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	return flow.Wait(config.Milliseconds), nil
}

type throttleConfig struct {
	PerSecond int `psy:"per-second"`
}

// Throttle rate-limits the stream to per-second messages.
func Throttle(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(throttleConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	return flow.Throttle(config.PerSecond), nil
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
	return flow.Head(config.Count), nil
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
	return flow.Tail(config.Skip), nil
}

type sampleConfig struct {
	Rate int `psy:"rate"`
}

// Sample keeps one message in every rate (statistical downsampling).
func Sample(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(sampleConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	return flow.Sample(config.Rate), nil
}
