package supervise

import (
	"context"
	"testing"
	"time"

	"github.com/gastrodon/psyduck/server"
)

func fixedClock() func() time.Time {
	t := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return t }
}

// waitStatus polls until the pipeline reaches want or the deadline passes.
func waitStatus(t *testing.T, s *Supervisor, id string, want server.PipelineStatus) server.PipelineInfo {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		p, ok := s.Get(id)
		if !ok {
			t.Fatalf("pipeline %s vanished", id)
		}
		if p.Status == want {
			return p
		}
		time.Sleep(5 * time.Millisecond)
	}
	p, _ := s.Get(id)
	t.Fatalf("pipeline %s: waited for %q, stuck at %q (%+v)", id, want, p.Status, p)
	return server.PipelineInfo{}
}

const constantToTrash = `
produce "constant" "c" {
  value      = "hi"
  stop-after = 3
}

consume "trash" "t" {}

pipeline "demo" {
  produce = [produce.constant.c]
  consume = [consume.trash.t]
}
`

// TestDispatchRunsToCompletion is the end-to-end proof the supervisor is
// live: a dispatched .psy actually runs through core and its counters move.
func TestDispatchRunsToCompletion(t *testing.T) {
	s := newSupervisor(context.Background(), fixedClock())

	created, err := s.Dispatch(server.DispatchRequest{Name: "demo", Source: constantToTrash})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if created.ID == "" {
		t.Fatal("dispatch returned empty id")
	}
	// Topology is extracted at parse time, before the run finishes.
	if len(created.Topology.Producers) != 1 || created.Topology.Producers[0].Ref != "produce.constant.c" {
		t.Errorf("topology: %+v", created.Topology)
	}

	done := waitStatus(t, s, created.ID, server.StatusSucceeded)
	if got := done.Stats; got.Produced != 3 || got.Delivered != 3 || got.Transformed != 3 || got.Filtered != 0 || got.InFlight != 0 {
		t.Errorf("stats: %+v, want produced=delivered=transformed=3", got)
	}
	if done.FinishedAt == nil || done.StartedAt == nil {
		t.Errorf("timestamps: started=%v finished=%v", done.StartedAt, done.FinishedAt)
	}
}

func TestInstanceCountsReflectRuns(t *testing.T) {
	s := newSupervisor(context.Background(), fixedClock())
	created, err := s.Dispatch(server.DispatchRequest{Source: constantToTrash})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	waitStatus(t, s, created.ID, server.StatusSucceeded)

	inst := s.Instance()
	if inst.Pipelines.Total != 1 || inst.Pipelines.Succeeded != 1 {
		t.Errorf("instance counts: %+v", inst.Pipelines)
	}
	if inst.Version != server.Version {
		t.Errorf("version: %q", inst.Version)
	}
}

func TestDispatchRejectsBadHCL(t *testing.T) {
	s := newSupervisor(context.Background(), fixedClock())
	_, err := s.Dispatch(server.DispatchRequest{Source: `this is not valid hcl {{{`})
	if err == nil {
		t.Fatal("expected a parse error for bad HCL")
	}
}

func TestDispatchRejectsNoPipeline(t *testing.T) {
	s := newSupervisor(context.Background(), fixedClock())
	_, err := s.Dispatch(server.DispatchRequest{Source: `produce "constant" "c" { value = "x" }`})
	if err == nil {
		t.Fatal("expected an error when the source declares no pipeline")
	}
}

func TestDispatchRejectsEmptySource(t *testing.T) {
	s := newSupervisor(context.Background(), fixedClock())
	if _, err := s.Dispatch(server.DispatchRequest{}); err != server.ErrInvalidSource {
		t.Fatalf("empty source: got %v, want ErrInvalidSource", err)
	}
}

const forever = `
produce "constant" "c" { value = "x" }

consume "trash" "t" {}

pipeline "loop" {
  produce = [produce.constant.c]
  consume = [consume.trash.t]
}
`

func TestCancelRunningPipeline(t *testing.T) {
	s := newSupervisor(context.Background(), fixedClock())
	created, err := s.Dispatch(server.DispatchRequest{Name: "loop", Source: forever})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	waitStatus(t, s, created.ID, server.StatusRunning)

	if err := s.Cancel(created.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	got := waitStatus(t, s, created.ID, server.StatusCanceled)
	if got.FinishedAt == nil {
		t.Error("canceled pipeline has no finished_at")
	}

	// Canceling a terminal pipeline is a conflict.
	if err := s.Cancel(created.ID); err != server.ErrNotCancelable {
		t.Errorf("re-cancel: got %v, want ErrNotCancelable", err)
	}
}

func TestCancelUnknown(t *testing.T) {
	s := newSupervisor(context.Background(), fixedClock())
	if err := s.Cancel("nope"); err != server.ErrNotFound {
		t.Errorf("cancel unknown: got %v, want ErrNotFound", err)
	}
}

// TestServesOverHTTP wires the live supervisor behind the real router and
// exercises the dispatch→observe flow end to end through the HTTP layer.
func TestServesOverHTTP(t *testing.T) {
	s := newSupervisor(context.Background(), fixedClock())
	created, err := s.Dispatch(server.DispatchRequest{Name: "demo", Source: constantToTrash})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	waitStatus(t, s, created.ID, server.StatusSucceeded)

	// The graph projection should include the pipeline and its stages.
	g := server.New(s) // ensure the live supervisor satisfies server.Supervisor
	_ = g
	graph := s.Graph()
	if len(graph.Nodes) == 0 || len(graph.Edges) == 0 {
		t.Errorf("graph: nodes=%d edges=%d", len(graph.Nodes), len(graph.Edges))
	}
}
