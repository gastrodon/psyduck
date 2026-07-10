package server

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Supervisor is everything the HTTP layer needs from the pipeline runtime.
// It owns the pipelines this instance is running and answers questions about
// them; the server package only ever talks to this interface, never to core
// or parse. That keeps the API testable against a stub and lets the live,
// pipeline-owning implementation land later without touching any handler.
//
// Every method must be safe for concurrent use — the HTTP server calls them
// from many goroutines.
type Supervisor interface {
	// Instance returns this instance's identity and rolled-up pipeline
	// counts.
	Instance() InstanceInfo

	// List returns every pipeline this instance knows about — running,
	// pending, and recently terminal — in a stable order.
	List() []PipelineInfo

	// Get returns one pipeline by id. The bool is false if no such pipeline
	// exists.
	Get(id string) (PipelineInfo, bool)

	// Dispatch accepts a pipeline to run and returns its freshly-created
	// record (status pending). It validates the request but does not block
	// on the pipeline actually starting.
	Dispatch(req DispatchRequest) (PipelineInfo, error)

	// Cancel stops a running or pending pipeline. It returns ErrNotFound if
	// the id is unknown and ErrNotCancelable if the pipeline has already
	// reached a terminal state.
	Cancel(id string) error

	// Graph returns the node/edge projection of everything running, for a
	// visualization board.
	Graph() Graph
}

// Supervisor error sentinels. Handlers map these onto HTTP status codes.
var (
	ErrNotFound      = errors.New("pipeline not found")
	ErrNotCancelable = errors.New("pipeline is not cancelable")
	ErrInvalidSource = errors.New("dispatch source is empty")
)

// StubSupervisor is an in-memory Supervisor that keeps the API coherent
// without a running pipeline behind it: it seeds a couple of representative
// pipelines, accepts Dispatch (recording a pending pipeline but running
// nothing), and honors Cancel. It exists so the HTTP surface is real and
// exercisable — by tests, by a UI being built against it, by curl — before
// the runtime grows a real supervisor. Every value it returns has the exact
// shape the live implementation will.
//
// What it deliberately does NOT do: parse Source, build a core.Pipeline,
// run anything, or move counters on its own. Those are the "wiring it live"
// follow-ups in docs/http-api.md.
type StubSupervisor struct {
	id        string
	startedAt time.Time

	mu    sync.Mutex
	seq   atomic.Uint64
	byID  map[string]*PipelineInfo
	order []string // insertion order, for stable listing
	nowFn func() time.Time
}

var _ Supervisor = (*StubSupervisor)(nil)

// NewStubSupervisor builds a stub seeded with a small, representative set of
// pipelines so the endpoints return something recognizable out of the box.
func NewStubSupervisor() *StubSupervisor {
	return newStubSupervisor(time.Now)
}

// newStubSupervisor is the injectable-clock constructor tests use for
// deterministic timestamps.
func newStubSupervisor(now func() time.Time) *StubSupervisor {
	s := &StubSupervisor{
		id:        "psyduck-local",
		startedAt: now(),
		byID:      make(map[string]*PipelineInfo),
		nowFn:     now,
	}
	s.seed()
	return s
}

// nextID hands out stable, sortable ids without needing a random source.
func (s *StubSupervisor) nextID() string {
	return fmt.Sprintf("pipe-%06d", s.seq.Add(1))
}

// seed installs two demo pipelines: one running with live-looking counters,
// one already succeeded. They give every read endpoint non-trivial data.
func (s *StubSupervisor) seed() {
	start := s.startedAt
	running := &PipelineInfo{
		ID:     s.nextID(),
		Name:   "ingest",
		Status: StatusRunning,
		Source: "file:examples/ingest.psy",
		Topology: Topology{
			Producers:    []ResourceRef{{Ref: "produce.http-listen.hook", Kind: StageProduce}},
			Transformers: []ResourceRef{{Ref: "transform.jq.shape", Kind: StageTransform}, {Ref: "transform.set.tag", Kind: StageTransform}},
			Consumers:    []ResourceRef{{Ref: "consume.socket.bus", Kind: StageConsume}},
		},
		Stats:     Stats{Produced: 1280, Transformed: 1204, Filtered: 76, Delivered: 1204, Errors: 2, InFlight: 0},
		CreatedAt: start,
		StartedAt: &start,
	}
	fin := start.Add(90 * time.Second)
	done := &PipelineInfo{
		ID:     s.nextID(),
		Name:   "backfill",
		Status: StatusSucceeded,
		Source: "dispatch:nightly-backfill",
		Labels: map[string]string{"run": "nightly"},
		Topology: Topology{
			RemoteSeed:   &ResourceRef{Ref: "produce.listen.jobs", Kind: StageProduce},
			Transformers: []ResourceRef{{Ref: "transform.pick-map.core", Kind: StageTransform}},
			Consumers:    []ResourceRef{{Ref: "consume.file.results", Kind: StageConsume}},
		},
		Stats:      Stats{Produced: 5000, Transformed: 5000, Filtered: 0, Delivered: 5000, Errors: 0, InFlight: 0},
		CreatedAt:  start,
		StartedAt:  &start,
		FinishedAt: &fin,
	}
	s.byID[running.ID] = running
	s.byID[done.ID] = done
	s.order = append(s.order, running.ID, done.ID)
}

func (s *StubSupervisor) Instance() InstanceInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return InstanceInfo{
		ID:            s.id,
		Version:       Version,
		StartedAt:     s.startedAt,
		UptimeSeconds: s.nowFn().Sub(s.startedAt).Seconds(),
		Pipelines:     s.countsLocked(),
	}
}

// countsLocked tallies pipelines by status. Caller holds s.mu.
func (s *StubSupervisor) countsLocked() PipelineCounts {
	var c PipelineCounts
	for _, id := range s.order {
		c.Total++
		switch s.byID[id].Status {
		case StatusRunning:
			c.Running++
		case StatusPending:
			c.Pending++
		case StatusSucceeded:
			c.Succeeded++
		case StatusFailed:
			c.Failed++
		case StatusCanceled:
			c.Canceled++
		}
	}
	return c
}

func (s *StubSupervisor) List() []PipelineInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PipelineInfo, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, *s.byID[id])
	}
	return out
}

func (s *StubSupervisor) Get(id string) (PipelineInfo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.byID[id]
	if !ok {
		return PipelineInfo{}, false
	}
	return *p, true
}

func (s *StubSupervisor) Dispatch(req DispatchRequest) (PipelineInfo, error) {
	if req.Source == "" {
		return PipelineInfo{}, ErrInvalidSource
	}
	name := req.Name
	if name == "" {
		name = "dispatched"
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.nowFn()
	p := &PipelineInfo{
		ID:        s.nextID(),
		Name:      name,
		Status:    StatusPending,
		Source:    "dispatch:" + name,
		Labels:    req.Labels,
		CreatedAt: now,
		// Topology and Stats stay zero until a real supervisor parses
		// Source and starts the run.
	}
	s.byID[p.ID] = p
	s.order = append(s.order, p.ID)
	return *p, nil
}

func (s *StubSupervisor) Cancel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.byID[id]
	if !ok {
		return ErrNotFound
	}
	if p.Status != StatusRunning && p.Status != StatusPending {
		return ErrNotCancelable
	}
	p.Status = StatusCanceled
	fin := s.nowFn()
	p.FinishedAt = &fin
	return nil
}

func (s *StubSupervisor) Graph() Graph {
	return buildGraph(s.List())
}

// buildGraph projects a set of pipelines into nodes and edges. It is pure
// (no supervisor state), so the live supervisor can reuse it verbatim. Each
// pipeline becomes a container node; each of its stages becomes a node wired
// in declaration order producer(s) → transform(s) → consumer(s). A
// produce-from seed is rendered as the single head node.
func buildGraph(pipelines []PipelineInfo) Graph {
	g := Graph{Nodes: []GraphNode{}, Edges: []GraphEdge{}}
	for _, p := range pipelines {
		stats := p.Stats
		g.Nodes = append(g.Nodes, GraphNode{
			ID:     p.ID,
			Kind:   "pipeline",
			Label:  p.Name,
			Status: string(p.Status),
			Stats:  &stats,
		})

		// Flatten the pipeline's stages into an ordered chain of node ids.
		var chain []string
		add := func(refs []ResourceRef) {
			for i, r := range refs {
				nid := fmt.Sprintf("%s:%s:%d", p.ID, r.Kind, i)
				g.Nodes = append(g.Nodes, GraphNode{ID: nid, Kind: string(r.Kind), Label: r.Ref, Pipeline: p.ID})
				chain = append(chain, nid)
			}
		}
		if p.Topology.RemoteSeed != nil {
			add([]ResourceRef{*p.Topology.RemoteSeed})
		} else {
			add(p.Topology.Producers)
		}
		add(p.Topology.Transformers)
		add(p.Topology.Consumers)

		for i := 0; i+1 < len(chain); i++ {
			g.Edges = append(g.Edges, GraphEdge{From: chain[i], To: chain[i+1], Messages: p.Stats.Delivered})
		}
	}
	// Stable node order regardless of map iteration in the source.
	sort.SliceStable(g.Nodes, func(i, j int) bool { return g.Nodes[i].ID < g.Nodes[j].ID })
	return g
}
