package core

import (
	"context"
	"fmt"

	"github.com/psyduck-etl/sdk"
	"github.com/sirupsen/logrus"

	"github.com/gastrodon/psyduck/parse"
	"github.com/gastrodon/psyduck/stdlib/flow"
)

// pulled is one delivery from the stream puller: a chunk of parsed
// resources, or an error. Errors do not necessarily end the pipeline —
// they are forwarded and the meta producer keeps running whatever work it
// already holds.
type pulled struct {
	resources []parse.Resource
	err       error
}

// metaProducer wraps a live producer stream in a single ordinary
// sdk.Producer, so core's fan-in (see produce in stream.go) cannot tell a
// produce-from pipeline apart from a literal one. Behind that face it does
// the meta-producer work:
//
//   - a puller goroutine drains tail — blocking for as long as the seed
//     stays quiet — and feeds parsed resources inward;
//   - arrivals are buffered, bound against their plugins, and run in
//     groups via the same produce fan-in the engine itself uses, so each
//     group inherits the engine's delivery and shutdown guarantees;
//   - groups run sequentially: the next group is formed only once the
//     current one is exhausted, from everything that arrived meanwhile.
//     parallel caps a group's size; 0 means a group takes everything
//     buffered at formation time.
//
// bootstrap producers (pre-bound by BuildPipeline for build-time error
// surfacing) join the first group. Errors — a failed seed, an unparsable
// message, a failed bind — are forwarded on errs and the show goes on;
// with exit-on-error set, RunPipeline's report cancels ctx and unwinds
// everything. The returned producer closes send once the stream is
// exhausted and every group has run, and always releases the stream (and
// with it the seed producer) on the way out.
func metaProducer(
	bootstrap []sdk.Producer,
	tail parse.ResourceFunc,
	plugins map[string]sdk.Plugin,
	parallel int,
	logger *logrus.Logger,
) sdk.Producer {
	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		defer close(send)

		feed := make(chan pulled)
		go func() {
			defer close(feed)
			for {
				chunk, err := tail(ctx, bindChunk)
				if err != nil {
					select {
					case feed <- pulled{err: err}:
					case <-ctx.Done():
					}
					return
				}
				if len(chunk) == 0 {
					return // exhausted
				}
				select {
				case feed <- pulled{resources: chunk}:
				case <-ctx.Done():
					return
				}
			}
		}()

		// The puller above is joined by draining feed; only then is
		// releasing the stream safe (a ResourceFunc is not reentrant). The
		// release stops a seed the puller did not get to observe ending —
		// without it, a run cancelled between pulls would leave the seed
		// producer parked forever.
		defer func() {
			for range feed {
			}
			tail(context.Background(), 0)
		}()

		forward := func(err error) bool {
			select {
			case errs <- err:
				return true
			case <-ctx.Done():
				return false
			}
		}

		ready := bootstrap
		var pending []parse.Resource
		feedOpen := true

		take := func(p pulled) {
			if p.err != nil {
				forward(p.err)
				return
			}
			pending = append(pending, p.resources...)
		}

		for {
			// top up pending with everything already delivered, without
			// blocking — a quiet seed must not delay work already in hand
			for feedOpen {
				var topped bool
				select {
				case p, ok := <-feed:
					if !ok {
						feedOpen = false
					} else {
						take(p)
						topped = true
					}
				default:
				}
				if !topped {
					break
				}
			}

			// bind pending resources into ready, up to the group cap
			for len(pending) > 0 && (parallel < 1 || len(ready) < parallel) {
				b := pending[0]
				pending = pending[1:]
				p, err := bindProducer(b, plugins)
				if err != nil {
					if !forward(err) {
						return
					}
					continue
				}
				ready = append(ready, p)
			}

			group := ready
			if parallel > 0 && len(group) > parallel {
				group = group[:parallel]
				ready = ready[parallel:]
			} else {
				ready = nil
			}

			if len(group) == 0 {
				if !feedOpen {
					return // stream exhausted and every group has run
				}
				// nothing to run: block for the seed's next delivery
				select {
				case p, ok := <-feed:
					if !ok {
						feedOpen = false
					} else {
						take(p)
					}
				case <-ctx.Done():
					return
				}
				continue
			}

			logger.WithField("group", len(group)).Debug("meta producer starting group")
			for msg, err := range produce(ctx, group) {
				if err != nil {
					if !forward(err) {
						return
					}
					continue
				}
				select {
				case send <- msg:
				case <-ctx.Done():
					return
				}
			}
			if ctx.Err() != nil {
				return
			}
		}
	}
}

// bindProducer resolves b against its owning plugin and wraps the instance
// with its host-owned flow gates (per-minute, stop-after).
func bindProducer(b parse.Resource, plugins map[string]sdk.Plugin) (sdk.Producer, error) {
	plugin, ok := plugins[b.PluginID]
	if !ok {
		return nil, fmt.Errorf("%s: %s: no plugin %q loaded", b.Block.Origin(), b.Ref, b.PluginID)
	}
	instance, err := plugin.Bind(b.Kind, b.Resource.Name, b.Block)
	if err != nil {
		return nil, fmt.Errorf("%s: %s: %w", b.Block.Origin(), b.Ref, err)
	}
	return flow.Producer(instance.Produce, b.Meta.PerMinute, 0, b.Meta.StopAfter), nil
}
