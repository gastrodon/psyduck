package core

import (
	"context"
	"fmt"
	"sync"

	"github.com/psyduck-etl/sdk"
)

// sink fans messages out to a set of running consumers and tracks which of
// them are still accepting. A consumer signals completion by closing its
// done channel — usually after the sink closes its input, but a consumer
// may finish early on its own (a stop-after wrapper, for example), and the
// sink simply stops sending to it rather than blocking the pipeline.
type sink struct {
	ins      []chan []byte
	dones    []chan struct{}
	finished []bool
	live     int
	errsWG   sync.WaitGroup
}

// startSink launches every consumer and an error forwarder for each.
// Forwarders hand errors to report and exit when their channel closes or
// ctx ends — consumers are not required to close their errs channel.
func startSink(ctx context.Context, consumers []sdk.Consumer, report func(error)) *sink {
	s := &sink{
		ins:      make([]chan []byte, len(consumers)),
		dones:    make([]chan struct{}, len(consumers)),
		finished: make([]bool, len(consumers)),
		live:     len(consumers),
	}

	for i, consume := range consumers {
		in, errs, done := make(chan []byte), make(chan error), make(chan struct{})
		s.ins[i], s.dones[i] = in, done
		go consume(in, errs, done)

		s.errsWG.Add(1)
		go func() {
			defer s.errsWG.Done()
			for {
				select {
				case err, ok := <-errs:
					if !ok {
						return
					}
					if err != nil {
						report(fmt.Errorf("consumer supplied error: %w", err))
					}
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	return s
}

// send delivers msg to every consumer still accepting. It reports whether
// any consumer remains live — false means the pipeline has nowhere left to
// deliver and should stop producing. A false return also covers ctx ending
// mid-delivery.
func (s *sink) send(ctx context.Context, msg []byte) bool {
	for i := range s.ins {
		if s.finished[i] {
			continue
		}
		select {
		case s.ins[i] <- msg:
		case <-s.dones[i]:
			s.finished[i] = true
			s.live--
		case <-ctx.Done():
			return false
		}
	}
	return s.live > 0
}

// close ends the message stream: every consumer's input channel is closed,
// which is its signal to flush and close done.
func (s *sink) close() {
	for _, in := range s.ins {
		close(in)
	}
}

// flush waits for every consumer to signal done, giving up when ctx ends.
func (s *sink) flush(ctx context.Context) {
	for i := range s.dones {
		if s.finished[i] {
			continue
		}
		select {
		case <-s.dones[i]:
		case <-ctx.Done():
			return
		}
	}
}

// waitErrs blocks until every error forwarder has exited. Call after
// cancelling the pipeline context so forwarders whose errs channel never
// closes are released.
func (s *sink) waitErrs() {
	s.errsWG.Wait()
}
