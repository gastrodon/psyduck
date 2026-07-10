package server

import (
	"errors"
	"net/http"
	"time"
)

// maxPluginBody caps a plugin request body. A plugin spec is a name, a
// source URL, and a tag — tiny.
const maxPluginBody = 64 << 10

// PluginStatus is the lifecycle state of a plugin in the instance's
// manifest. Registering a plugin means cloning + compiling it into the
// content-addressed store (a partial `psyduck init`) — a file operation that
// takes real time, so it is asynchronous like a dispatch. "ready" means the
// binary is built and available for jobs to load; it does not mean the
// plugin is resident in the process (loading happens per job, at dispatch).
type PluginStatus string

const (
	PluginLoading PluginStatus = "loading" // clone/build into the store in progress
	PluginReady   PluginStatus = "ready"   // built and in the store; jobs may declare it
	PluginFailed  PluginStatus = "failed"  // clone/build failed (see Error)
)

// PluginRequest is the body of POST /api/v1/plugins (add) and
// PUT /api/v1/plugins/{name} (update). It mirrors a `plugin {}` block:
// a name, a git URL or local path, and an optional ref to check out.
type PluginRequest struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Tag    string `json:"tag,omitempty"`
}

// PluginInfo is the list/summary view of a loaded (or loading) plugin.
type PluginInfo struct {
	Name   string       `json:"name"`
	Source string       `json:"source"`
	Tag    string       `json:"tag,omitempty"` // requested ref, if any
	Ref    string       `json:"ref,omitempty"` // ref actually resolved at build
	Hash   string       `json:"hash,omitempty"`
	Status PluginStatus `json:"status"`
	Error  string       `json:"error,omitempty"`
	// Resources is the count this plugin exposes; the full manifest is on
	// GET /api/v1/plugins/{name}.
	Resources int `json:"resources"`
	// RestartRequired is set when an update rebuilt the binary but couldn't
	// activate it in-process — Go plugins can't be unloaded or reloaded, so
	// the new version is staged in the store and takes effect on restart
	// while the currently-loaded one keeps serving.
	RestartRequired bool       `json:"restart_required,omitempty"`
	AddedAt         time.Time  `json:"added_at"`
	LoadedAt        *time.Time `json:"loaded_at,omitempty"`
}

// PluginManifest is the detail view (GET /api/v1/plugins/{name}): the
// resources a plugin offers and every config option each accepts. This is
// the "what can I configure" surface a UI or a pipeline author reads before
// writing a `produce`/`transform`/`consume` block against the plugin.
type PluginManifest struct {
	Name      string           `json:"name"`
	Source    string           `json:"source"`
	Ref       string           `json:"ref,omitempty"`
	Status    PluginStatus     `json:"status"`
	Resources []PluginResource `json:"resources"`
}

// PluginResource is one resource a plugin offers — a producer, transformer,
// and/or consumer — and its configurable options.
type PluginResource struct {
	Name    string         `json:"name"`
	Kinds   []string       `json:"kinds"` // any of "produce", "transform", "consume"
	Options []PluginOption `json:"options"`
}

// PluginOption describes one configuration field of a resource. It is the
// API projection of sdk.Spec: nested list/map element types surface as Elem,
// object fields as Fields.
type PluginOption struct {
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Description string         `json:"description,omitempty"`
	Required    bool           `json:"required,omitempty"`
	Default     any            `json:"default,omitempty"`
	Elem        *PluginOption  `json:"elem,omitempty"`
	Fields      []PluginOption `json:"fields,omitempty"`
}

// Plugin management error sentinels. Handlers map these onto status codes.
var (
	ErrPluginNotFound = errors.New("plugin not found")
	ErrPluginExists   = errors.New("plugin already loaded")
	ErrInvalidPlugin  = errors.New("plugin name and source are required")
)

// --- handlers ---------------------------------------------------------------

func (s *Server) handleListPlugins(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"plugins": s.sup.Plugins()})
}

func (s *Server) handleGetPlugin(w http.ResponseWriter, r *http.Request) {
	m, ok := s.sup.Plugin(r.PathValue("name"))
	if !ok {
		writeError(w, http.StatusNotFound, "no such plugin")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleAddPlugin(w http.ResponseWriter, r *http.Request) {
	var req PluginRequest
	if err := decodeJSON(r, &req, maxPluginBody); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := s.sup.AddPlugin(req)
	switch {
	case err == nil:
		w.Header().Set("Location", "/api/v1/plugins/"+p.Name)
		writeJSON(w, http.StatusAccepted, p)
	case errors.Is(err, ErrInvalidPlugin):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrPluginExists):
		writeError(w, http.StatusConflict, "a plugin with that name is already loaded; use PUT to update it")
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

func (s *Server) handleUpdatePlugin(w http.ResponseWriter, r *http.Request) {
	var req PluginRequest
	if err := decodeJSON(r, &req, maxPluginBody); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := s.sup.UpdatePlugin(r.PathValue("name"), req)
	switch {
	case err == nil:
		writeJSON(w, http.StatusAccepted, p)
	case errors.Is(err, ErrPluginNotFound):
		writeError(w, http.StatusNotFound, "no such plugin")
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

func (s *Server) handleRemovePlugin(w http.ResponseWriter, r *http.Request) {
	p, err := s.sup.RemovePlugin(r.PathValue("name"))
	switch {
	case err == nil:
		// Removing a plugin edits the manifest — new jobs can no longer
		// declare it. It does not disturb running pipelines (they hold their
		// own snapshot), and the built binary stays in the store cache; any
		// copy already loaded by a job stays resident until restart (Go
		// plugins cannot be unloaded).
		writeJSON(w, http.StatusOK, map[string]any{
			"plugin": p,
			"note":   "removed from the manifest; running pipelines are unaffected, and the built binary remains in the store cache",
		})
	case errors.Is(err, ErrPluginNotFound):
		writeError(w, http.StatusNotFound, "no such plugin")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
