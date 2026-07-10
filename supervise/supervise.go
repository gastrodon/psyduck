// Package supervise is the live [server.Supervisor]: it owns the pipelines a
// psyduck serve instance is running, parsing dispatched .psy documents,
// building them with core, running each in its own goroutine, and reporting
// live status and stats.
//
// It sits below the server package in the dependency graph — server defines
// the HTTP surface and the Supervisor interface and imports neither core nor
// parse; supervise implements that interface using core, parse, and stdlib.
// That's the boundary docs/http-api.md describes: the HTTP layer never learns
// about pipelines, only about the interface.
package supervise

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/core"
	"github.com/gastrodon/psyduck/parse"
	"github.com/gastrodon/psyduck/parse/hcl"
	"github.com/gastrodon/psyduck/server"
	"github.com/gastrodon/psyduck/stdlib"
)

// Supervisor is the in-process, live implementation of server.Supervisor. It
// runs every dispatched pipeline under baseCtx, so canceling baseCtx (the
// serve command's SIGINT/SIGTERM context) winds every pipeline down.
//
// Scope note: dispatched pipelines resolve against the plugins handed to New
// — stdlib by default. A dispatched source that references an external
// plugin{} fails at build with a clear "no plugin loaded" error, because the
// clone/compile store flow (psyduck init) isn't part of the serve path yet.
// That's a documented phase-1 limitation, not a design decision.
type Supervisor struct {
	id      string
	baseCtx context.Context
	plugins []sdk.Plugin
	now     func() time.Time

	startedAt time.Time
	seq       atomic.Uint64

	mu    sync.Mutex
	byID  map[string]*managed
	order []string
}

var _ server.Supervisor = (*Supervisor)(nil)

// New builds a live supervisor. baseCtx bounds every pipeline it runs;
// plugins are the resource plugins dispatched pipelines may use (stdlib is
// always included, so passing none is fine).
func New(baseCtx context.Context, plugins ...sdk.Plugin) *Supervisor {
	return newSupervisor(baseCtx, time.Now, plugins...)
}

// newSupervisor is the injectable-clock constructor for tests.
func newSupervisor(baseCtx context.Context, now func() time.Time, plugins ...sdk.Plugin) *Supervisor {
	loaded := append([]sdk.Plugin{stdlib.Plugin()}, plugins...)
	return &Supervisor{
		id:        "psyduck-local",
		baseCtx:   baseCtx,
		plugins:   loaded,
		now:       now,
		startedAt: now(),
		byID:      make(map[string]*managed),
	}
}

// managed is one supervised pipeline: its immutable identity, its live core
// stats, and its mutable lifecycle state behind mu.
type managed struct {
	id        string
	name      string
	source    string
	labels    map[string]string
	topology  server.Topology
	createdAt time.Time
	cancel    context.CancelFunc

	mu         sync.Mutex
	status     server.PipelineStatus
	errMsg     string
	startedAt  *time.Time
	finishedAt *time.Time
	stats      *core.Stats // nil until the pipeline is built
}

func (s *Supervisor) Instance() server.InstanceInfo {
	list := s.List()
	var c server.PipelineCounts
	for _, p := range list {
		c.Total++
		switch p.Status {
		case server.StatusRunning:
			c.Running++
		case server.StatusPending:
			c.Pending++
		case server.StatusSucceeded:
			c.Succeeded++
		case server.StatusFailed:
			c.Failed++
		case server.StatusCanceled:
			c.Canceled++
		}
	}
	return server.InstanceInfo{
		ID:            s.id,
		Version:       server.Version,
		StartedAt:     s.startedAt,
		UptimeSeconds: s.now().Sub(s.startedAt).Seconds(),
		Pipelines:     c,
	}
}

func (s *Supervisor) List() []server.PipelineInfo {
	s.mu.Lock()
	ms := make([]*managed, 0, len(s.order))
	for _, id := range s.order {
		ms = append(ms, s.byID[id])
	}
	s.mu.Unlock()

	out := make([]server.PipelineInfo, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.snapshot())
	}
	return out
}

func (s *Supervisor) Get(id string) (server.PipelineInfo, bool) {
	s.mu.Lock()
	m, ok := s.byID[id]
	s.mu.Unlock()
	if !ok {
		return server.PipelineInfo{}, false
	}
	return m.snapshot(), true
}

func (s *Supervisor) Graph() server.Graph { return server.BuildGraph(s.List()) }

// Dispatch parses and validates the request synchronously — so a malformed
// .psy comes straight back as an error the handler turns into 400 — then
// runs the pipeline asynchronously and returns its pending record.
func (s *Supervisor) Dispatch(req server.DispatchRequest) (server.PipelineInfo, error) {
	if req.Source == "" {
		return server.PipelineInfo{}, server.ErrInvalidSource
	}

	id := fmt.Sprintf("pipe-%06d", s.seq.Add(1))
	pipe, err := s.parseOne(id, req.Source)
	if err != nil {
		return server.PipelineInfo{}, err
	}

	name := req.Name
	if name == "" {
		name = pipe.Name
	}

	// The cancel is created here, before the goroutine starts, so Cancel
	// always has something to call even if the pipeline is still pending.
	ctx, cancel := context.WithCancel(s.baseCtx)
	m := &managed{
		id:        id,
		name:      name,
		source:    "dispatch:" + name,
		labels:    req.Labels,
		topology:  topologyOf(pipe.Spec),
		createdAt: s.now(),
		cancel:    cancel,
		status:    server.StatusPending,
	}

	s.mu.Lock()
	s.byID[id] = m
	s.order = append(s.order, id)
	s.mu.Unlock()

	go s.run(ctx, m, pipe)

	return m.snapshot(), nil
}

// parseOne parses one dispatched .psy document and requires it to declare
// exactly one pipeline. The source is fed through an in-memory Loader keyed
// by a synthetic entry name; imports are rejected (dispatched pipelines must
// be self-contained).
func (s *Supervisor) parseOne(id, source string) (parse.Pipeline, error) {
	entry := "dispatch:" + id
	name := parse.ResolveImportPath("", entry) // Parse resolves the entry the same way
	loader := func(path string) (parse.Source, error) {
		if path == name {
			return parse.Source{Name: name, Content: []byte(source)}, nil
		}
		return parse.Source{}, fmt.Errorf("dispatch %s: cannot import %q; dispatched pipelines must be self-contained", id, path)
	}

	pipes, err := hcl.NewParserHCL().Parse(s.baseCtx, entry, loader, s.plugins)
	if err != nil {
		return parse.Pipeline{}, fmt.Errorf("parsing dispatched pipeline: %w", err)
	}
	switch len(pipes) {
	case 1:
		for _, p := range pipes {
			return p, nil
		}
	case 0:
		return parse.Pipeline{}, errors.New("dispatched source declares no pipeline")
	}
	return parse.Pipeline{}, fmt.Errorf("dispatched source declares %d pipelines; dispatch exactly one", len(pipes))
}

// run builds and runs a dispatched pipeline to a terminal state. It is the
// live counterpart of the CLI run path: build (which may block binding a
// produce-from seed), then RunPipeline, then record how it ended.
func (s *Supervisor) run(ctx context.Context, m *managed, pipe parse.Pipeline) {
	m.mu.Lock()
	if m.status == server.StatusCanceled { // canceled while pending
		m.mu.Unlock()
		return
	}
	m.status = server.StatusRunning
	started := s.now()
	m.startedAt = &started
	m.mu.Unlock()

	built, err := core.BuildPipeline(ctx, pipe, s.plugins)
	if err != nil {
		s.finish(m, fmt.Errorf("building pipeline: %w", err))
		return
	}

	m.mu.Lock()
	m.stats = built.Stats // now reads see live counters
	m.mu.Unlock()

	s.finish(m, core.RunPipeline(ctx, built))
}

// finish records the terminal state, unless Cancel already did. A ctx-driven
// stop (RunPipeline returns a context error) reads as canceled, not failed.
func (s *Supervisor) finish(m *managed, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status == server.StatusCanceled {
		return
	}
	fin := s.now()
	m.finishedAt = &fin
	switch {
	case err == nil:
		m.status = server.StatusSucceeded
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		m.status = server.StatusCanceled
	default:
		m.status = server.StatusFailed
		m.errMsg = err.Error()
	}
}

func (s *Supervisor) Cancel(id string) error {
	s.mu.Lock()
	m, ok := s.byID[id]
	s.mu.Unlock()
	if !ok {
		return server.ErrNotFound
	}

	m.mu.Lock()
	if m.status != server.StatusRunning && m.status != server.StatusPending {
		m.mu.Unlock()
		return server.ErrNotCancelable
	}
	m.status = server.StatusCanceled
	fin := s.now()
	m.finishedAt = &fin
	m.mu.Unlock()

	m.cancel()
	return nil
}

// snapshot renders the managed pipeline as the API view, reading live
// counters at the instant of the call.
func (m *managed) snapshot() server.PipelineInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	info := server.PipelineInfo{
		ID:         m.id,
		Name:       m.name,
		Status:     m.status,
		Source:     m.source,
		Labels:     m.labels,
		Topology:   m.topology,
		Error:      m.errMsg,
		CreatedAt:  m.createdAt,
		StartedAt:  m.startedAt,
		FinishedAt: m.finishedAt,
	}
	if m.stats != nil {
		snap := m.stats.Snapshot()
		info.Stats = server.Stats{
			Produced:    snap.Produced,
			Transformed: snap.Transformed,
			Filtered:    snap.Filtered,
			Delivered:   snap.Delivered,
			Errors:      snap.Errors,
			InFlight:    inFlight(snap),
		}
	}
	return info
}

// inFlight is the coarse lag gauge: produced messages that have neither been
// delivered nor filtered yet. Clamped at zero — transform-side errors (rare)
// would otherwise let it read slightly negative.
func inFlight(s core.StatsSnapshot) int64 {
	n := int64(s.Produced) - int64(s.Delivered) - int64(s.Filtered)
	if n < 0 {
		return 0
	}
	return n
}

// topologyOf projects a parsed pipeline's declared resources into the API's
// topology view. Refs only — never evaluated config, which can hold secrets.
func topologyOf(spec parse.PipelineSpec) server.Topology {
	t := server.Topology{}
	for _, r := range spec.Producers {
		t.Producers = append(t.Producers, server.ResourceRef{Ref: r.Ref, Kind: server.StageProduce})
	}
	if spec.RemoteSeed != nil {
		t.RemoteSeed = &server.ResourceRef{Ref: spec.RemoteSeed.Ref, Kind: server.StageProduce}
	}
	for _, r := range spec.Transformers {
		t.Transformers = append(t.Transformers, server.ResourceRef{Ref: r.Ref, Kind: server.StageTransform})
	}
	for _, r := range spec.Consumers {
		t.Consumers = append(t.Consumers, server.ResourceRef{Ref: r.Ref, Kind: server.StageConsume})
	}
	return t
}
