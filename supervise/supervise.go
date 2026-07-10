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
	"github.com/gastrodon/psyduck/plugins"
	"github.com/gastrodon/psyduck/server"
	"github.com/gastrodon/psyduck/stdlib"
)

// Supervisor is the in-process, live implementation of server.Supervisor. It
// runs every dispatched pipeline under baseCtx, so canceling baseCtx (the
// serve command's SIGINT/SIGTERM context) winds every pipeline down.
//
// Plugins: an instance keeps a manifest of plugins (see plugins.go) that
// operators add/update/remove over the API — a store/file operation that
// clones and compiles each into the content-addressed store. A dispatched
// job declares the plugins it needs with plugin{} blocks, exactly like a
// .psy file for `psyduck run`; at dispatch those blocks are resolved against
// the manifest and loaded from the store for that job. Editing the manifest
// changes what future jobs can use and never disturbs a running pipeline.
type Supervisor struct {
	id      string
	baseCtx context.Context
	base    []sdk.Plugin // always-available plugins (stdlib + any passed to New)
	now     func() time.Time

	startedAt time.Time
	seq       atomic.Uint64

	mu    sync.Mutex
	byID  map[string]*managed
	order []string

	// Plugin registry: dynamically loaded plugins available to new
	// dispatches. store holds the content-addressed binaries; buildPlugin
	// and openPlugin are the fetch/compile and plugin.Open steps, split so
	// an update can rebuild-for-restart (build only) without reopening. Both
	// are injectable for tests.
	store       *plugins.Store
	buildPlugin func(spec parse.Plugin) (ref, hash string, err error)
	openPlugin  func(spec parse.Plugin, ref, hash string) (sdk.Plugin, error)

	pmu     sync.Mutex
	pByName map[string]*pluginEntry
	pOrder  []string
	// openedPlugin caches every plugin binary this process has opened, keyed
	// by content hash. Go's plugin.Open cannot unload, so an opened plugin
	// stays here for the life of the process; the cache lets repeated jobs
	// reuse it and lets an update detect that a prior version is resident.
	openedPlugin map[string]sdk.Plugin
}

var _ server.Supervisor = (*Supervisor)(nil)

// New builds a live supervisor. baseCtx bounds every pipeline it runs;
// storeRoot is the directory (e.g. ".psyduck") where dynamically added
// plugins are cloned, built, and content-addressed; base are always-present
// plugins dispatched pipelines may use (stdlib is always included, so
// passing none is fine).
func New(baseCtx context.Context, storeRoot string, base ...sdk.Plugin) *Supervisor {
	return newSupervisor(baseCtx, time.Now, storeRoot, base...)
}

// newSupervisor is the injectable-clock constructor for tests.
func newSupervisor(baseCtx context.Context, now func() time.Time, storeRoot string, base ...sdk.Plugin) *Supervisor {
	s := &Supervisor{
		id:           "psyduck-local",
		baseCtx:      baseCtx,
		base:         append([]sdk.Plugin{stdlib.Plugin()}, base...),
		now:          now,
		startedAt:    now(),
		byID:         make(map[string]*managed),
		store:        plugins.NewStore(storeRoot),
		pByName:      make(map[string]*pluginEntry),
		openedPlugin: make(map[string]sdk.Plugin),
	}
	// Real fetch/compile/open, backed by the content-addressed store. Tests
	// override these to avoid cloning and CGO.
	s.buildPlugin = func(spec parse.Plugin) (string, string, error) {
		locked, err := s.store.Build([]parse.Plugin{spec})
		if err != nil {
			return "", "", err
		}
		entry, ok := locked[spec.Name]
		if !ok {
			return "", "", fmt.Errorf("build produced no entry for %q", spec.Name)
		}
		return entry.Ref, entry.Hash, nil
	}
	s.openPlugin = func(spec parse.Plugin, ref, hash string) (sdk.Plugin, error) {
		loaded, err := s.store.Load(map[string]plugins.LockedPlugin{
			spec.Name: {Source: spec.Source, Ref: ref, Hash: hash},
		})
		if err != nil {
			return nil, err
		}
		return loaded[0], nil
	}
	return s
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
	// plugins is the set this pipeline was dispatched against (stdlib plus
	// whatever its plugin{} blocks resolved to). It's captured at dispatch
	// and used for its build, so plugin edits after dispatch never affect a
	// running pipeline — only future jobs.
	plugins []sdk.Plugin

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
	entry, loader := s.loaderFor(id, req.Source)

	// The job declares which plugins it needs via plugin{} blocks, exactly
	// like a .psy file for `psyduck run`. Resolve those against this
	// instance's manifest and load them from the store (stdlib is always
	// present). This snapshot is what the pipeline runs against for its whole
	// life — later plugin edits change future jobs, never this one.
	jobSpecs, err := hcl.NewParserHCL().Plugins(entry, loader)
	if err != nil {
		return server.PipelineInfo{}, fmt.Errorf("reading plugin blocks: %w", err)
	}
	loaded, err := s.loadForJob(jobSpecs)
	if err != nil {
		return server.PipelineInfo{}, err
	}

	pipe, err := parsePipeline(s.baseCtx, entry, loader, loaded)
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
		plugins:   loaded,
	}

	s.mu.Lock()
	s.byID[id] = m
	s.order = append(s.order, id)
	s.mu.Unlock()

	go s.run(ctx, m, pipe)

	return m.snapshot(), nil
}

// loaderFor builds the in-memory entry name and Loader for a dispatched
// source: the source is served under a synthetic entry name, and imports are
// rejected (dispatched pipelines must be self-contained). The same loader
// feeds both plugin-block extraction and the full parse, so they agree.
func (s *Supervisor) loaderFor(id, source string) (entry string, load parse.Loader) {
	entry = "dispatch:" + id
	name := parse.ResolveImportPath("", entry) // Parse resolves the entry the same way
	return entry, func(path string) (parse.Source, error) {
		if path == name {
			return parse.Source{Name: name, Content: []byte(source)}, nil
		}
		return parse.Source{}, fmt.Errorf("dispatch %s: cannot import %q; dispatched pipelines must be self-contained", id, path)
	}
}

// parsePipeline parses a dispatched source against loaded plugins and
// requires it to declare exactly one pipeline.
func parsePipeline(ctx context.Context, entry string, loader parse.Loader, loaded []sdk.Plugin) (parse.Pipeline, error) {
	pipes, err := hcl.NewParserHCL().Parse(ctx, entry, loader, loaded)
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

	built, err := core.BuildPipeline(ctx, pipe, m.plugins)
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
