package supervise

import (
	"fmt"
	"sync"
	"time"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/parse"
	"github.com/gastrodon/psyduck/server"
)

// pluginEntry is one plugin in this instance's manifest: a name bound to a
// source, and — once built — the git ref and content hash of the .so sitting
// in the store. "ready" means the binary is built and content-addressed into
// the store, available for a job to load; it does NOT mean the plugin is
// resident in this process. Managing plugins is a store/manifest file op;
// loading is deferred to the jobs that declare them.
type pluginEntry struct {
	name    string
	addedAt time.Time

	mu              sync.Mutex
	source          string
	tag             string
	ref             string
	hash            string
	resources       int // known resource count, filled the first time it's introspected
	status          server.PluginStatus
	errMsg          string
	builtAt         *time.Time
	restartRequired bool
}

func (e *pluginEntry) info() server.PluginInfo {
	e.mu.Lock()
	defer e.mu.Unlock()
	return server.PluginInfo{
		Name:            e.name,
		Source:          e.source,
		Tag:             e.tag,
		Ref:             e.ref,
		Hash:            e.hash,
		Status:          e.status,
		Error:           e.errMsg,
		Resources:       e.resources,
		RestartRequired: e.restartRequired,
		AddedAt:         e.addedAt,
		LoadedAt:        e.builtAt,
	}
}

func (s *Supervisor) Plugins() []server.PluginInfo {
	s.pmu.Lock()
	es := make([]*pluginEntry, 0, len(s.pOrder))
	for _, name := range s.pOrder {
		es = append(es, s.pByName[name])
	}
	s.pmu.Unlock()

	out := make([]server.PluginInfo, 0, len(es))
	for _, e := range es {
		out = append(out, e.info())
	}
	return out
}

// Plugin returns a plugin's manifest. Reading the resource list means
// opening the .so (Resources() is only knowable from the loaded plugin), so
// this endpoint is what first makes a plugin resident; the opened handle is
// cached, and the resource count is remembered on the entry.
func (s *Supervisor) Plugin(name string) (server.PluginManifest, bool) {
	s.pmu.Lock()
	e, ok := s.pByName[name]
	s.pmu.Unlock()
	if !ok {
		return server.PluginManifest{}, false
	}

	e.mu.Lock()
	man := server.PluginManifest{Name: e.name, Source: e.source, Ref: e.ref, Status: e.status}
	source, ref, hash, ready := e.source, e.ref, e.hash, e.status == server.PluginReady
	e.mu.Unlock()
	if !ready {
		return man, true // nothing built to introspect yet
	}

	p, err := s.openCached(name, source, ref, hash)
	if err != nil {
		man.Status = server.PluginFailed
		return man, true
	}
	man.Resources = manifestResources(p)

	e.mu.Lock()
	e.resources = len(man.Resources)
	e.mu.Unlock()
	return man, true
}

// AddPlugin registers a plugin and builds it into the store. It is a
// file/manifest operation: clone + compile + content-address, then record
// the resolved ref/hash. It does NOT load the plugin into this process —
// that happens per job, at dispatch. Building is slow, so it runs
// asynchronously; the returned record is status loading.
func (s *Supervisor) AddPlugin(req server.PluginRequest) (server.PluginInfo, error) {
	if req.Name == "" || req.Source == "" {
		return server.PluginInfo{}, server.ErrInvalidPlugin
	}

	s.pmu.Lock()
	if _, ok := s.pByName[req.Name]; ok {
		s.pmu.Unlock()
		return server.PluginInfo{}, server.ErrPluginExists
	}
	e := &pluginEntry{
		name:    req.Name,
		addedAt: s.now(),
		source:  req.Source,
		tag:     req.Tag,
		status:  server.PluginLoading,
	}
	s.pByName[req.Name] = e
	s.pOrder = append(s.pOrder, req.Name)
	s.pmu.Unlock()

	go s.buildEntry(e, "")
	return e.info(), nil
}

// UpdatePlugin re-points an existing plugin at a new source/tag and rebuilds
// it into the store. Editing a plugin changes what future jobs can use;
// pipelines already running keep the plugin snapshot they were dispatched
// with, so none of them are disturbed. If a previous version of this plugin
// is already resident in the process, the rebuilt binary can't be
// hot-swapped (Go can't reload a package) — it lands in the store and the
// record is flagged RestartRequired, taking effect for new jobs after a
// restart.
func (s *Supervisor) UpdatePlugin(name string, req server.PluginRequest) (server.PluginInfo, error) {
	s.pmu.Lock()
	e, ok := s.pByName[name]
	s.pmu.Unlock()
	if !ok {
		return server.PluginInfo{}, server.ErrPluginNotFound
	}

	e.mu.Lock()
	prevHash := e.hash
	if req.Source != "" {
		e.source = req.Source
	}
	e.tag = req.Tag
	e.status = server.PluginLoading
	e.errMsg = ""
	e.mu.Unlock()

	go s.buildEntry(e, prevHash)
	return e.info(), nil
}

// RemovePlugin deregisters a plugin from the manifest, so new jobs can no
// longer declare it. It does not touch running pipelines (they hold their
// own snapshot), the content-addressed binary in the store (a cache), or any
// copy already resident in this process (Go can't unload it).
func (s *Supervisor) RemovePlugin(name string) (server.PluginInfo, error) {
	s.pmu.Lock()
	defer s.pmu.Unlock()
	e, ok := s.pByName[name]
	if !ok {
		return server.PluginInfo{}, server.ErrPluginNotFound
	}
	delete(s.pByName, name)
	for i, n := range s.pOrder {
		if n == name {
			s.pOrder = append(s.pOrder[:i], s.pOrder[i+1:]...)
			break
		}
	}
	return e.info(), nil
}

// buildEntry fetches and compiles a plugin into the store, then records the
// resolved ref/hash. prevHash is the entry's hash before an update (empty on
// a first add) — used to flag RestartRequired when the old version was
// already resident.
func (s *Supervisor) buildEntry(e *pluginEntry, prevHash string) {
	e.mu.Lock()
	spec := parse.Plugin{Name: e.name, Source: e.source, Tag: e.tag}
	e.mu.Unlock()

	ref, hash, err := s.buildPlugin(spec)

	e.mu.Lock()
	defer e.mu.Unlock()
	if err != nil {
		e.status = server.PluginFailed
		e.errMsg = err.Error()
		return
	}
	e.ref, e.hash = ref, hash
	t := s.now()
	e.builtAt = &t
	e.status = server.PluginReady
	e.errMsg = ""
	e.restartRequired = false
	if prevHash != "" && prevHash != hash {
		s.pmu.Lock()
		_, opened := s.openedPlugin[prevHash]
		s.pmu.Unlock()
		e.restartRequired = opened
	}
}

// loadForJob resolves a job's plugin{} specs against the manifest and opens
// the corresponding binaries from the store, returning stdlib plus the
// loaded plugins. A spec that isn't registered, isn't built, or doesn't
// match the manifest is a clear error — a job must declare plugins the
// instance actually has (mirroring how `psyduck run` needs a lock file).
func (s *Supervisor) loadForJob(specs []parse.Plugin) ([]sdk.Plugin, error) {
	out := append([]sdk.Plugin(nil), s.base...)
	for _, sp := range specs {
		s.pmu.Lock()
		e, ok := s.pByName[sp.Name]
		s.pmu.Unlock()
		if !ok {
			return nil, fmt.Errorf("plugin %q is not registered on this instance; POST /api/v1/plugins to add it", sp.Name)
		}

		e.mu.Lock()
		st, src, ref, hash, tag := e.status, e.source, e.ref, e.hash, e.tag
		e.mu.Unlock()
		if st != server.PluginReady {
			return nil, fmt.Errorf("plugin %q is not ready (status: %s)", sp.Name, st)
		}
		if sp.Source != "" && sp.Source != src {
			return nil, fmt.Errorf("plugin %q source %q does not match the registered %q", sp.Name, sp.Source, src)
		}
		if sp.Tag != "" && sp.Tag != tag {
			return nil, fmt.Errorf("plugin %q tag %q does not match the registered %q", sp.Name, sp.Tag, tag)
		}

		p, err := s.openCached(sp.Name, src, ref, hash)
		if err != nil {
			return nil, fmt.Errorf("loading plugin %q: %w", sp.Name, err)
		}
		out = append(out, p)
	}
	return out, nil
}

// openCached opens a plugin binary from the store, memoized by content hash.
// The first open makes that binary resident for the life of the process
// (Go plugins can't be unloaded); every later request for the same hash
// reuses it — which also means a job re-declaring an already-loaded plugin
// version costs nothing.
func (s *Supervisor) openCached(name, source, ref, hash string) (sdk.Plugin, error) {
	s.pmu.Lock()
	if p, ok := s.openedPlugin[hash]; ok {
		s.pmu.Unlock()
		return p, nil
	}
	s.pmu.Unlock()

	p, err := s.openPlugin(parse.Plugin{Name: name, Source: source}, ref, hash)
	if err != nil {
		return nil, err
	}

	s.pmu.Lock()
	s.openedPlugin[hash] = p
	s.pmu.Unlock()
	return p, nil
}

// manifestResources projects a loaded plugin's resource descriptors into the
// API manifest shape.
func manifestResources(p sdk.Plugin) []server.PluginResource {
	descs := p.Resources()
	out := make([]server.PluginResource, 0, len(descs))
	for _, d := range descs {
		r := server.PluginResource{Name: d.Name, Kinds: kindStrings(d.Kinds)}
		for _, sp := range d.Spec {
			r.Options = append(r.Options, optionOf(sp))
		}
		out = append(out, r)
	}
	return out
}

// kindStrings renders a sdk.Kind bitmask as the verb forms the rest of the
// API uses ("produce"/"transform"/"consume").
func kindStrings(k sdk.Kind) []string {
	var out []string
	if k&sdk.PRODUCER != 0 {
		out = append(out, "produce")
	}
	if k&sdk.TRANSFORMER != 0 {
		out = append(out, "transform")
	}
	if k&sdk.CONSUMER != 0 {
		out = append(out, "consume")
	}
	return out
}

// optionOf projects one sdk.Spec (recursively, for list/map elements and
// object fields) into a PluginOption.
func optionOf(sp *sdk.Spec) server.PluginOption {
	o := server.PluginOption{
		Name:        sp.Name,
		Type:        sp.Type.String(),
		Description: sp.Description,
		Required:    sp.Required,
		Default:     sp.Default,
	}
	if sp.ElemType != nil {
		e := optionOf(sp.ElemType)
		o.Elem = &e
	}
	for _, f := range sp.Fields {
		o.Fields = append(o.Fields, optionOf(f))
	}
	return o
}
