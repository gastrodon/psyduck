/*
Package server is the HTTP control/observability surface for a single
psyduck instance.

It is deliberately split from the pipeline runtime: everything here talks to
a [Supervisor], an interface that owns whatever pipelines this instance is
running and answers questions about them. The HTTP layer only marshals — it
never touches core, parse, or the plugin store directly. That boundary is
what lets the same routes serve a stub today (see [StubSupervisor]) and a
live, pipeline-owning supervisor once the runtime grows one (see
docs/http-api.md, "Wiring it live").

Scope: this package is the *single-instance* API — observe the pipelines
this process runs, dispatch new ones to it, and expose metrics. Peer-to-peer
(an instance learning about siblings and splitting a job across them) is a
deliberate second stage; the peer types and the /api/v1/peers route exist
here only as reserved, not-yet-implemented placeholders so the shape is
visible while the design is settled.
*/
package server

// Version is the API/instance version reported by GET /api/v1/instance and
// the /metrics build info. It tracks the HTTP surface, not the psyduck
// binary as a whole.
const Version = "0.1.0-dev"
