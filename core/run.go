package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

/*
RunPipeline drives a built pipeline until its producers are exhausted, all
of its consumers finish, StopAfter is reached, or ctx ends.

Producers are bound at run time by the pipeline's ProducerSource and run
through a worker pool that merges their output into one iter.Seq2 stream
(see produce); each message runs through the transformer stack and fans out
to every consumer still accepting (see sink). Errors from any side are
logged; with ExitOnError set the first one also cancels the pipeline and is
returned. A stream that never yields a producer fails the run with
ErrNoProducers regardless of ExitOnError. Cancelling ctx stops the run
promptly and returns ctx's error.

Every goroutine the engine starts is signaled to stop before RunPipeline
returns, and the engine's own goroutines are joined. Plugin goroutines are
signaled but not joined on the cancellation path: producers and consumers
receive the pipeline's ctx and are contractually required to select on
ctx.Done() alongside their sends, so a conforming plugin exits promptly on
cancellation — but one that ignores ctx may still be winding down (or
parked forever) after RunPipeline returns.
*/
func RunPipeline(outer context.Context, pipeline *Pipeline) error {
	ctx, cancel := context.WithCancel(outer)
	defer cancel()

	logger := pipeline.logger
	if logger == nil {
		logger = pipelineLogger()
	}

	if pipeline.Producers == nil {
		return fmt.Errorf("pipeline has no producers: %w", ErrNoProducers)
	}

	var failMu sync.Mutex
	var failure error
	report := func(err error) {
		logger.Error(err)
		if pipeline.ExitOnError {
			failMu.Lock()
			if failure == nil {
				failure = err
				cancel()
			}
			failMu.Unlock()
		}
	}

	transform := pipeline.Transformer
	if transform == nil {
		transform = func(msg []byte) ([]byte, error) { return msg, nil }
	}

	consumers := startSink(ctx, pipeline.Consumers, report)

	feed, feedErrs := pipeline.Producers(ctx)

	delivered := 0
	for msg, err := range produce(ctx, feed, feedErrs, pipeline.Parallel) {
		if err != nil {
			if errors.Is(err, ErrNoProducers) {
				// A pipeline that never ran a producer is a failure, no
				// matter what exit-on-error says. Keep any cause already
				// reported (e.g. a seed timeout) as the returned failure.
				failMu.Lock()
				if failure == nil {
					failure = err
				}
				failMu.Unlock()
				cancel()
				break
			}
			report(fmt.Errorf("producer supplied error: %w", err))
			continue
		}

		transformed, err := transform(msg)
		if err != nil {
			report(fmt.Errorf("transformer supplied error: %w", err))
			continue
		}
		if transformed == nil {
			continue // filtered out
		}

		if !consumers.send(ctx, transformed) {
			break // every consumer finished, or ctx ended
		}

		delivered++
		if pipeline.StopAfter > 0 && delivered >= pipeline.StopAfter {
			break
		}
	}

	consumers.close()
	consumers.flush(ctx)
	cancel() // release error forwarders whose channels never close
	consumers.waitErrs()

	failMu.Lock()
	defer failMu.Unlock()
	if failure != nil {
		return failure
	}
	return outer.Err() // non-nil only when the caller's ctx ended the run
}
