package server

import "time"

// PipelineStatus is the lifecycle state of a pipeline known to this
// instance. A dispatched pipeline starts pending, becomes running once the
// supervisor has built and started it, and lands in exactly one terminal
// state.
type PipelineStatus string

const (
	StatusPending   PipelineStatus = "pending"   // accepted, not yet started
	StatusRunning   PipelineStatus = "running"   // producing/consuming right now
	StatusSucceeded PipelineStatus = "succeeded" // producers exhausted, consumers flushed
	StatusFailed    PipelineStatus = "failed"    // stopped by an error (see PipelineInfo.Error)
	StatusCanceled  PipelineStatus = "canceled"  // stopped by a caller (DELETE) or shutdown
)

// StageKind names which of the three pipeline slots a resource fills. It
// mirrors sdk.Kind's producer/transformer/consumer split, as a string the
// API can hand to a UI.
type StageKind string

const (
	StageProduce   StageKind = "produce"
	StageTransform StageKind = "transform"
	StageConsume   StageKind = "consume"
)

// ResourceRef is one resource in a pipeline's topology, named by its
// qualified reference (e.g. "produce.request.api"). It is display metadata
// only — the API never exposes evaluated config, which can hold secrets.
type ResourceRef struct {
	Ref  string    `json:"ref"`
	Kind StageKind `json:"kind"`
}

// Topology is a pipeline's declared shape: the resources in each slot, in
// declaration order. RemoteSeed is set instead of Producers when the
// pipeline uses produce-from (a dynamic/meta producer) — see
// parse.PipelineSpec, which this mirrors.
type Topology struct {
	Producers    []ResourceRef `json:"producers"`
	Transformers []ResourceRef `json:"transformers"`
	Consumers    []ResourceRef `json:"consumers"`
	RemoteSeed   *ResourceRef  `json:"remote_seed,omitempty"`
}

// Stats is a point-in-time counter snapshot for one pipeline. The counters
// map onto the observable events in core.RunPipeline's loop: a message is
// produced, then either filtered by a transformer (nil result today),
// delivered to consumers, or turns into an error. InFlight is a coarse lag
// gauge — produced minus everything that has since left the pipeline.
//
// Counters are monotonic within a single run; a UI computes rates by
// diffing snapshots over time.
type Stats struct {
	Produced    uint64 `json:"produced"`
	Transformed uint64 `json:"transformed"`
	Filtered    uint64 `json:"filtered"`
	Delivered   uint64 `json:"delivered"`
	Errors      uint64 `json:"errors"`
	InFlight    int64  `json:"in_flight"`
}

// PipelineInfo is the full API view of one pipeline. It is what
// GET /api/v1/pipelines/{id} returns and what the list endpoint returns per
// entry.
type PipelineInfo struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Status     PipelineStatus    `json:"status"`
	Source     string            `json:"source"` // "file:<path>" or "dispatch:<label>"
	Labels     map[string]string `json:"labels,omitempty"`
	Topology   Topology          `json:"topology"`
	Stats      Stats             `json:"stats"`
	Error      string            `json:"error,omitempty"` // set iff Status == failed
	CreatedAt  time.Time         `json:"created_at"`
	StartedAt  *time.Time        `json:"started_at,omitempty"`
	FinishedAt *time.Time        `json:"finished_at,omitempty"`
}

// PipelineCounts is the by-status breakdown reported on the instance
// summary, so a caller can render "3 running, 1 failed" without listing.
type PipelineCounts struct {
	Running   int `json:"running"`
	Pending   int `json:"pending"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Canceled  int `json:"canceled"`
	Total     int `json:"total"`
}

// InstanceInfo describes this instance to a caller (or a peer, later). It's
// what GET /api/v1/instance returns and the identity a sibling would learn
// in stage 2.
type InstanceInfo struct {
	ID            string         `json:"id"`
	Version       string         `json:"version"`
	StartedAt     time.Time      `json:"started_at"`
	UptimeSeconds float64        `json:"uptime_seconds"`
	Pipelines     PipelineCounts `json:"pipelines"`
}

// DispatchRequest is the body of POST /api/v1/pipelines: a pipeline to run
// on this instance. Source is a complete .psy document (HCL) — the same
// text a `psyduck run` file holds — which the supervisor parses, builds,
// and runs. This is the HTTP front door to the meta-pipeline/remote-worker
// pattern documented in docs/patterns.md ("Meta-pipelines"): instead of a
// socket or queue feeding produce-from, an operator (or a peer) POSTs work.
//
// Name and Labels are advisory metadata for listing/filtering; if Name is
// empty the supervisor derives it from the pipeline{} block(s) in Source.
type DispatchRequest struct {
	Name   string            `json:"name,omitempty"`
	Source string            `json:"source"`
	Labels map[string]string `json:"labels,omitempty"`
}

// Graph is the graph-board view of everything this instance is running:
// each pipeline and its stages as nodes, wired producer→transform→consumer
// as edges, with live counters on both. It's a denormalized projection of
// the same data the pipeline endpoints return, shaped for a node/edge
// visualization rather than for polling one pipeline.
type Graph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// GraphNode is one vertex: either a pipeline container or a single stage
// within it. Kind is "pipeline" or one of the StageKind values.
type GraphNode struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	Pipeline string `json:"pipeline,omitempty"` // owning pipeline id, for stage nodes
	Status   string `json:"status,omitempty"`   // for pipeline nodes
	Stats    *Stats `json:"stats,omitempty"`    // for pipeline nodes
}

// GraphEdge is a directed link between two GraphNodes. Messages, when known,
// is the count that has crossed it — enough to weight/animate the edge.
type GraphEdge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Messages uint64 `json:"messages,omitempty"`
}

// Peer is a sibling instance this one has learned about. RESERVED FOR
// STAGE 2 — it is defined here so the peer-to-peer shape is visible while
// that design is settled, but nothing populates it yet and the /api/v1/peers
// route returns 501. See docs/http-api.md, "Stage 2: peers".
type Peer struct {
	ID       string    `json:"id"`
	Address  string    `json:"address"`
	LastSeen time.Time `json:"last_seen"`
}
