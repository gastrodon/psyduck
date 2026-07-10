package core

import "sync/atomic"

// Stats holds live counters for one running pipeline. RunPipeline bumps the
// fields as messages flow; an observer (the HTTP supervisor) reads a
// consistent copy with Snapshot while the pipeline is still running. All
// access is atomic, so a *Stats can be shared across goroutines without
// further locking.
//
// The counters correspond to the observable events in RunPipeline's loop:
// a message is Produced, then it is Filtered (a transformer dropped it),
// Transformed (it passed the whole stack) and Delivered (handed to
// consumers), or it turns into an Error. Errors also counts producer- and
// consumer-side errors, which have no Produced/Delivered of their own.
type Stats struct {
	produced    atomic.Uint64
	transformed atomic.Uint64
	filtered    atomic.Uint64
	delivered   atomic.Uint64
	errors      atomic.Uint64
}

// StatsSnapshot is an immutable copy of a Stats at one instant, safe to
// hand outside the engine.
type StatsSnapshot struct {
	Produced    uint64
	Transformed uint64
	Filtered    uint64
	Delivered   uint64
	Errors      uint64
}

// Snapshot reads every counter. The reads are individually atomic but not
// mutually consistent — a snapshot taken mid-flight may show a message
// counted as produced but not yet delivered, which is exactly the in-flight
// state a lag gauge wants.
func (s *Stats) Snapshot() StatsSnapshot {
	if s == nil {
		return StatsSnapshot{}
	}
	return StatsSnapshot{
		Produced:    s.produced.Load(),
		Transformed: s.transformed.Load(),
		Filtered:    s.filtered.Load(),
		Delivered:   s.delivered.Load(),
		Errors:      s.errors.Load(),
	}
}
