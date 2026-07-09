package core

import (
	"context"
	"fmt"
	"sync"

	"github.com/psyduck-etl/sdk"
)

// transformStage wires the pipeline's transformer between the merged
// producer stream and the sink.
//
// A feeder goroutine drains the producer iterator onto the transformer's
// input, closing it when producers are exhausted or ctx ends. The
// transformer itself runs fire-and-forget in its own goroutine, exactly
// like a producer or consumer — startTransform does not wait for it to
// return, only for the feeder and its own error forwarder. That mirrors
// startSink's shape, and for the same reason: a transformer that ignores
// ctx must not be able to hang RunPipeline.
type transformStage struct {
	out    chan []byte
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// startTransform launches the feeder, the transformer, and an error
// forwarder. Once the caller is done draining out, it must call cancel and
// then wg.Wait to release the feeder and error forwarder.
func startTransform(ctx context.Context, transform sdk.Transformer, producers []sdk.Producer, report func(error)) *transformStage {
	ctx, cancel := context.WithCancel(ctx)
	in := make(chan []byte)
	out := make(chan []byte)
	errs := make(chan error)

	s := &transformStage{out: out, cancel: cancel}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer close(in)
		for msg, err := range produce(ctx, producers) {
			if err != nil {
				report(fmt.Errorf("producer supplied error: %w", err))
				continue
			}
			select {
			case in <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()

	go transform(ctx, in, out, errs)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case err, ok := <-errs:
				if !ok {
					return
				}
				if err != nil {
					report(fmt.Errorf("transformer supplied error: %w", err))
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return s
}
