package produce

import (
	"context"
	"reflect"
	"testing"
	"time"
)

// parser builds an sdk.Parser that populates a struct's psy-tagged fields
// from vals via reflection, the same stand-in stdlib/integration uses to
// stay independent of the HCL config layer.
func parser(vals map[string]any) func(dst any) error {
	return func(dst any) error {
		rv := reflect.ValueOf(dst).Elem()
		rt := rv.Type()
		for i := 0; i < rt.NumField(); i++ {
			tag := rt.Field(i).Tag.Get("psy")
			if tag == "" {
				continue
			}
			v, ok := vals[tag]
			if !ok || v == nil {
				continue
			}
			rv.Field(i).Set(reflect.ValueOf(v))
		}
		return nil
	}
}

// mustStop runs p under a cancelled-shortly context and fails the test if
// it doesn't return (both channels closed) within the bound — a hang here
// means the producer isn't actually selecting on ctx.Done().
func mustStop(t *testing.T, p func(context.Context, chan<- []byte, chan<- error)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	send, errs := make(chan []byte), make(chan error)
	done := make(chan struct{})
	go func() {
		defer close(done)
		p(ctx, send, errs)
	}()

	// drain whatever the producer sends, same as a real consumer would,
	// until it signals done by returning (which it can only do here via
	// its deferred close(send)/close(errs) once ctx ends).
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-send:
			continue
		case <-errs:
			continue
		case <-done:
			return
		case <-deadline:
			t.Fatal("producer did not stop after ctx cancellation")
		}
	}
}

func TestConstant_StopsOnCancel(t *testing.T) {
	p, err := Constant(parser(map[string]any{"value": "x", "stop-after": 0}))
	if err != nil {
		t.Fatal(err)
	}
	mustStop(t, p)
}

func TestSequence_StopsOnCancel(t *testing.T) {
	p, err := Sequence(parser(map[string]any{"start": 0, "step": 1, "stop-after": 0}))
	if err != nil {
		t.Fatal(err)
	}
	mustStop(t, p)
}

func TestGenerate_StopsOnCancel(t *testing.T) {
	p, err := Generate(parser(map[string]any{"values": []string{"a", "b"}, "loop": true, "stop-after": 0}))
	if err != nil {
		t.Fatal(err)
	}
	mustStop(t, p)
}

func TestTicker_StopsOnCancel(t *testing.T) {
	p, err := Ticker(parser(map[string]any{"interval-ms": 1, "format": "unix-ms", "stop-after": 0}))
	if err != nil {
		t.Fatal(err)
	}
	mustStop(t, p)
}
