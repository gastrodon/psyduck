package consume

import (
	"context"
	"fmt"
	"io"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib/transport"
)

// closeOnDone closes c the moment ctx ends, unless the returned stop fires
// first. It exists to unblock a synchronous, otherwise uncancellable Write()
// on a writer/connection when the consumer's ctx is cancelled.
func closeOnDone(ctx context.Context, c io.Closer) (stop func()) {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			c.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}

type fileConfig struct {
	Location     string  `psy:"location"`
	Follow       bool    `psy:"follow"` // producer-only
	Append       bool    `psy:"append"`
	Create       bool    `psy:"create"`
	Sep          *string `psy:"sep"`
	SepByte      *int    `psy:"sep-byte"`
	SepByteIndex *int    `psy:"sep-byte-index"`
	Group        int     `psy:"group"`
}

// File writes each message to a location (file path, "-" stdout, "--" stderr,
// or a socket URI), joining messages with the configured separator. It is the
// consumer half of the dual-role `file` resource — you write files the way you
// read them.
func File(parse sdk.Parser) (sdk.Consumer, error) {
	config := new(fileConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	if config.Follow {
		return nil, fmt.Errorf("file consumer: follow is a producer-only attribute")
	}

	d := transport.Delimit{
		Sep:          config.Sep,
		SepByte:      config.SepByte,
		SepByteIndex: config.SepByteIndex,
		Group:        config.Group,
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}

	return func(ctx context.Context, recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		defer close(done)
		defer close(errs)

		wc, err := transport.OpenWriter(config.Location, config.Append, config.Create)
		if err != nil {
			errs <- err
			return
		}
		defer wc.Close()

		stop := closeOnDone(ctx, wc)
		defer stop()

		joiner := d.Joiner(wc)
		for msg := range recv {
			if err := joiner.Write(msg); err != nil {
				if ctx.Err() != nil {
					return
				}
				errs <- err
			}
		}
		if err := joiner.Close(); err != nil && ctx.Err() == nil {
			errs <- err
		}
	}, nil
}
