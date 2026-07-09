package core

import (
	"context"
	"fmt"
	"sync"
)

/*
RunPipeline drives a built pipeline until its producers are exhausted, all
of its consumers finish, StopAfter is reached, or ctx ends.

Producers are merged into one iter.Seq2 stream (see produce); each message
runs through the transformer stack and fans out to every consumer still
accepting (see sink). Errors from any side are logged; with ExitOnError set
the first one also cancels the pipeline and is returned. Cancelling ctx
stops the run promptly and returns ctx's error.

Every goroutine the engine starts — its own and every plugin's — is
released before RunPipeline returns: producers and consumers receive the
pipeline's ctx and are contractually required to select on ctx.Done()
alongside their sends, so an abandoned plugin is expected to exit on
cancellation rather than parking on its last send.
*/
func RunPipeline(outer context.Context, pipeline *Pipeline) error {
	ctx, cancel := context.WithCancel(outer)
	defer cancel()

	logger := pipeline.logger
	if logger == nil {
		logger = pipelineLogger()
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

	delivered := 0
	for msg, err := range produce(ctx, pipeline.Producers) {
		if err != nil {
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
