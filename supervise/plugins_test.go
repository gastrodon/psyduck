package supervise

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/parse"
	"github.com/gastrodon/psyduck/server"
)

// fakePlugin is a working in-proc plugin offering a single "emit" producer,
// so a dispatched job can actually load and run it without cloning or CGO.
func fakePlugin(name string) sdk.Plugin {
	return sdk.NewInProc(name, &sdk.Resource{
		Name:  "emit",
		Kinds: sdk.PRODUCER,
		Spec: []*sdk.Spec{
			{Name: "times", Description: "how many to emit", Type: sdk.TypeInt, Default: 1},
		},
		ProvideProducer: func(p sdk.Parser) (sdk.Producer, error) {
			cfg := struct {
				Times int `psy:"times"`
			}{}
			if err := p(&cfg); err != nil {
				return nil, err
			}
			return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
				defer close(send)
				defer close(errs)
				for i := 0; i < cfg.Times; i++ {
					select {
					case send <- []byte("x"):
					case <-ctx.Done():
						return
					}
				}
			}, nil
		},
	})
}

// pluginTestSupervisor is a supervisor whose build/open steps are faked:
// build derives a deterministic ref/hash (and fails for a "boom" source),
// open returns a fakePlugin. No git, no go build, no plugin.Open.
func pluginTestSupervisor(t *testing.T) *Supervisor {
	t.Helper()
	s := newSupervisor(context.Background(), fixedClock(), t.TempDir())
	s.buildPlugin = func(spec parse.Plugin) (string, string, error) {
		if strings.Contains(spec.Source, "boom") {
			return "", "", fmt.Errorf("build blew up")
		}
		return "refs/tags/" + orDefault(spec.Tag, "v0"), "hash-" + spec.Name + "-" + spec.Tag, nil
	}
	s.openPlugin = func(spec parse.Plugin, ref, hash string) (sdk.Plugin, error) {
		return fakePlugin(spec.Name), nil
	}
	return s
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func waitPlugin(t *testing.T, s *Supervisor, name string, want server.PluginStatus) server.PluginInfo {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, p := range s.Plugins() {
			if p.Name == name && p.Status == want {
				return p
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("plugin %s never reached %q; have %+v", name, want, s.Plugins())
	return server.PluginInfo{}
}

func TestPluginLifecycle(t *testing.T) {
	s := pluginTestSupervisor(t)

	added, err := s.AddPlugin(server.PluginRequest{Name: "fake", Source: "file:///fake", Tag: "v1"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if added.Status != server.PluginLoading {
		t.Errorf("add status: got %q, want loading", added.Status)
	}

	ready := waitPlugin(t, s, "fake", server.PluginReady)
	if ready.Hash == "" || ready.Ref == "" {
		t.Errorf("ready plugin missing ref/hash: %+v", ready)
	}

	// Manifest introspection opens the plugin and reports its resources.
	man, ok := s.Plugin("fake")
	if !ok {
		t.Fatal("manifest missing")
	}
	if len(man.Resources) != 1 || man.Resources[0].Name != "emit" {
		t.Fatalf("manifest resources: %+v", man.Resources)
	}
	if got := man.Resources[0].Kinds; len(got) != 1 || got[0] != "produce" {
		t.Errorf("kinds: %v", got)
	}
	if len(man.Resources[0].Options) != 1 || man.Resources[0].Options[0].Name != "times" {
		t.Errorf("options: %+v", man.Resources[0].Options)
	}

	// Update re-points it; still ready afterward.
	if _, err := s.UpdatePlugin("fake", server.PluginRequest{Source: "file:///fake", Tag: "v2"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	upd := waitPlugin(t, s, "fake", server.PluginReady)
	if upd.Tag != "v2" {
		t.Errorf("update tag: got %q, want v2", upd.Tag)
	}

	// Remove deregisters it.
	if _, err := s.RemovePlugin("fake"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(s.Plugins()) != 0 {
		t.Errorf("plugins after remove: %+v", s.Plugins())
	}
	if _, ok := s.Plugin("fake"); ok {
		t.Error("manifest still present after remove")
	}
}

func TestAddPluginValidation(t *testing.T) {
	s := pluginTestSupervisor(t)
	if _, err := s.AddPlugin(server.PluginRequest{Name: "x"}); err != server.ErrInvalidPlugin {
		t.Errorf("missing source: got %v", err)
	}
	if _, err := s.AddPlugin(server.PluginRequest{Name: "fake", Source: "file:///fake"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := s.AddPlugin(server.PluginRequest{Name: "fake", Source: "file:///fake"}); err != server.ErrPluginExists {
		t.Errorf("duplicate: got %v, want ErrPluginExists", err)
	}
}

func TestAddPluginBuildFailure(t *testing.T) {
	s := pluginTestSupervisor(t)
	if _, err := s.AddPlugin(server.PluginRequest{Name: "bad", Source: "file:///boom"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	failed := waitPlugin(t, s, "bad", server.PluginFailed)
	if failed.Error == "" {
		t.Error("failed plugin has no error message")
	}
}

func TestUpdateRemoveUnknown(t *testing.T) {
	s := pluginTestSupervisor(t)
	if _, err := s.UpdatePlugin("nope", server.PluginRequest{Source: "x"}); err != server.ErrPluginNotFound {
		t.Errorf("update unknown: got %v", err)
	}
	if _, err := s.RemovePlugin("nope"); err != server.ErrPluginNotFound {
		t.Errorf("remove unknown: got %v", err)
	}
}

const jobWithPlugin = `
plugin "fake" {
  source = "file:///fake"
}

produce "emit" "e" {
  times = 4
}

consume "trash" "t" {}

pipeline "uses-plugin" {
  produce = [produce.emit.e]
  consume = [consume.trash.t]
}
`

// TestDispatchLoadsRegisteredPlugin proves the per-job loading model: a
// dispatched job that declares a plugin{} block runs against the plugin the
// instance has in its manifest.
func TestDispatchLoadsRegisteredPlugin(t *testing.T) {
	s := pluginTestSupervisor(t)
	if _, err := s.AddPlugin(server.PluginRequest{Name: "fake", Source: "file:///fake"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	waitPlugin(t, s, "fake", server.PluginReady)

	created, err := s.Dispatch(server.DispatchRequest{Source: jobWithPlugin})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	done := waitStatus(t, s, created.ID, server.StatusSucceeded)
	if done.Stats.Produced != 4 || done.Stats.Delivered != 4 {
		t.Errorf("stats: %+v, want produced=delivered=4", done.Stats)
	}
}

// TestDispatchUnregisteredPluginFails: a job may only declare plugins the
// instance actually has.
func TestDispatchUnregisteredPluginFails(t *testing.T) {
	s := pluginTestSupervisor(t)
	_, err := s.Dispatch(server.DispatchRequest{Source: jobWithPlugin})
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("expected a not-registered error, got %v", err)
	}
}

// TestDispatchPluginSourceMismatch: the job's plugin block must match the
// manifest, not just name-match.
func TestDispatchPluginSourceMismatch(t *testing.T) {
	s := pluginTestSupervisor(t)
	if _, err := s.AddPlugin(server.PluginRequest{Name: "fake", Source: "file:///other"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	waitPlugin(t, s, "fake", server.PluginReady)

	_, err := s.Dispatch(server.DispatchRequest{Source: jobWithPlugin})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected a source-mismatch error, got %v", err)
	}
}
