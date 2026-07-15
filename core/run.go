package core

import (
	"context"
	"fmt"
	"sync"
)

/*
RunPipeline drives a built pipeline until its producers are exhausted, all
of its consumers finish, or ctx ends.

Producers are bound at run time by the pipeline's ProducerSource and run
through a worker pool that merges their output into one iter.Seq2 stream
(see produce); each message runs through the transformer stack and fans out
to every consumer still accepting (see sink). Errors from any side —
including a produce-from seed that timed out or closed without ever declaring
a producer — are logged uniformly; with ExitOnError set the first one also
cancels the pipeline and is returned, and without it the run finishes
normally. A stream that never yields a producer is not itself an error: the
run simply exits having delivered nothing. Cancelling ctx stops the run
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
		return fmt.Errorf("pipeline has no producers")
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

	consumers := startSink(ctx, pipeline.Consumers, report)

	feed, feedErrs := pipeline.Producers(ctx)

	if pipeline.Transformer == nil {
		for msg, err := range produce(ctx, feed, feedErrs, pipeline.Parallel) {
			if err != nil {
				report(fmt.Errorf("producer supplied error: %w", err))
				continue
			}

			if !consumers.send(ctx, msg) {
				break // every consumer finished, or ctx ended
			}
		}
	} else {
		stage := startTransform(ctx, pipeline.Transformer, feed, feedErrs, pipeline.Parallel, report)
		for msg := range stage.out {
			if !consumers.send(ctx, msg) {
				break // every consumer finished, or ctx ended
			}
		}
		stage.cancel()
		stage.wg.Wait()
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
