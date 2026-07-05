package data

import "fmt"

// OnError decides what happens when an operation fails on a message. It
// receives the error and returns what should propagate: a non-nil error is
// forwarded (core propagates whatever a transformer returns unconditionally,
// so "err" and "drop" need no separate downstream path); nil swallows it and
// drops the message.
//
// A handler that itself fails while handling (logging, a metrics call, ...)
// returns that failure rather than silently discarding one error or the
// other — Drop surfaces the handling error in its place; a handler built on
// top of Propagate should compose the two with WrapHandlerErr.
//
// OnError is a plain function type, not a single-method interface — the same
// choice sdk.Parser and http.HandlerFunc make for a one-method contract. Every
// call site binds it by capturing the value in a closure at construction
// time; there are no long-lived transformer structs to hang an interface
// field off of, and a closure already gives a handler private state (a drop
// counter, a log destination) without an interface's indirection.
type OnError func(error) error

// Propagate forwards the error unchanged. It backs the "err" mode, the
// default when on-error is unset.
func Propagate(err error) error { return err }

// Drop swallows the error and drops the message, forwarding nil. It backs the
// "drop" mode.
func Drop(error) error { return nil }

// WrapHandlerErr composes an error encountered while handling with the
// original, so a handler that performs a fallible action doesn't have to
// choose which one to discard.
func WrapHandlerErr(original, handling error) error {
	return fmt.Errorf("while handling error %q: encountered error %q", original, handling)
}

// ParseOnError parses the config spelling of an error handler ("err" | "drop",
// "" defaults to "err") into an OnError callback.
func ParseOnError(s string) (OnError, error) {
	switch s {
	case "", "err":
		return Propagate, nil
	case "drop":
		return Drop, nil
	default:
		return nil, fmt.Errorf("unknown error mode %q (want \"err\" or \"drop\")", s)
	}
}
