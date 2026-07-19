//go:build linux || darwin

package plugins

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/parse"
)

// jsonBlock is a ConfigBlock backed by a JSON blob, mirroring the SDK's own
// test pattern. The example plugin's config struct carries json: tags
// alongside psy: so this decodes without pulling in the host's parser.
type jsonBlock struct{ data []byte }

func (b jsonBlock) Origin() sdk.SourceRange { return sdk.SourceRange{SourceName: "test"} }
func (b jsonBlock) Encode() ([]byte, error) {
	if len(b.data) == 0 {
		return []byte("{}"), nil
	}
	return b.data, nil
}
func (b jsonBlock) Decode(dst any) error {
	if len(b.data) == 0 {
		return nil
	}
	return json.Unmarshal(b.data, dst)
}

// examplePluginDir locates cmd/example-plugin. Tests run from the package
// directory, so the module root is one level up.
func examplePluginDir(t *testing.T) string {
	t.Helper()
	src, err := filepath.Abs("../cmd/example-plugin")
	if err != nil {
		t.Fatalf("abs example plugin path: %v", err)
	}
	return src
}

func TestLoad_Integration(t *testing.T) {
	store := NewStore(t.TempDir())

	locked, err := store.Build([]parse.Plugin{{Name: "example-plugin", Source: examplePluginDir(t)}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	plugins, err := store.Load(locked)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(store.Close)
	if len(plugins) != 1 {
		t.Fatalf("loaded %d plugins, want 1", len(plugins))
	}
	p := plugins[0]

	if p.Name() != "example-plugin" {
		t.Errorf("Name = %q, want %q", p.Name(), "example-plugin")
	}

	var constantRes sdk.ResourceDescriptor
	for _, r := range p.Resources() {
		if r.Name == "constant" {
			constantRes = r
			break
		}
	}
	if constantRes.Name == "" {
		t.Fatal("plugin has no 'constant' resource")
	}
	if constantRes.Kinds&sdk.PRODUCER == 0 {
		t.Errorf("'constant' is not a producer")
	}

	block := jsonBlock{data: []byte(`{"value":"hello","count":2}`)}
	inst, err := p.Bind(context.Background(), sdk.PRODUCER, "constant", block)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	send := make(chan []byte, 4)
	errs := make(chan error, 1)
	go inst.Produce(t.Context(), send, errs)

	var got [][]byte
	timeout := time.After(2 * time.Second)
Loop:
	for {
		select {
		case msg, ok := <-send:
			if !ok {
				break Loop
			}
			got = append(got, msg)
		case err := <-errs:
			if err != nil {
				t.Fatalf("producer error: %v", err)
			}
		case <-timeout:
			t.Fatalf("producer timeout after %d messages", len(got))
		}
	}

	if len(got) != 2 {
		t.Errorf("got %d messages, want 2", len(got))
	}
	for i, msg := range got {
		if string(msg) != "hello" {
			t.Errorf("message %d = %q, want %q", i, msg, "hello")
		}
	}
}

// TestLoad_CloseIdempotent verifies Close can be called more than once
// without panicking — the doc promises it, and main.go relies on it
// (Store.Close may run, then plugins.CleanupClients reaps again).
func TestLoad_CloseIdempotent(t *testing.T) {
	store := NewStore(t.TempDir())

	locked, err := store.Build([]parse.Plugin{{Name: "example-plugin", Source: examplePluginDir(t)}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, err := store.Load(locked); err != nil {
		t.Fatalf("Load: %v", err)
	}

	store.Close()
	store.Close() // must be a no-op, not a double-kill panic
}

// TestLoad_PartialFailureTearsDown checks Load's teardown promise: when
// one entry launches but a later one fails verification, the launched
// subprocess must not leak. The failure is a hash mismatch (a drifted
// store) — the branch that used to return without tearing down. Map order
// is random, so this asserts the post-condition that holds either way:
// after a failed Load, the store holds no live clients.
func TestLoad_PartialFailureTearsDown(t *testing.T) {
	root := t.TempDir()

	build := NewStore(root)
	locked, err := build.Build([]parse.Plugin{{Name: "example-plugin", Source: examplePluginDir(t)}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// A second entry whose stored bytes don't match its locked hash.
	badHash, err := build.storeBinary(mustWriteTemp(t, "original"), "bad")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(build.binPath("bad", badHash), []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}

	lock := map[string]LockedPlugin{
		"example-plugin": locked["example-plugin"],
		"bad":            {Hash: badHash},
	}

	// Run a handful of times so both map orderings (good-first, which is the
	// leak-prone path, and bad-first) are exercised.
	for range 10 {
		s := NewStore(root)
		if _, err := s.Load(lock); err == nil {
			s.Close()
			t.Fatal("Load succeeded over a tampered binary, want error")
		}
		if n := len(s.clients); n != 0 {
			s.Close()
			t.Fatalf("Load left %d subprocess(es) running after a partial failure", n)
		}
	}
}

func TestBuild_PerNameFile(t *testing.T) {
	// Two different plugin names built from the same source dir produce
	// byte-identical binaries — the lock records the same hash for both,
	// but each lands on its own `<name>-psyduck-<sha7>` file on disk so
	// their subprocesses show up separately in ps.
	store := NewStore(t.TempDir())

	locked, err := store.Build([]parse.Plugin{
		{Name: "a", Source: examplePluginDir(t)},
		{Name: "b", Source: examplePluginDir(t)},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if locked["a"].Hash == "" || locked["a"].Hash != locked["b"].Hash {
		t.Fatalf("want matching non-empty hashes for identical builds, got %#v", locked)
	}

	entries, err := os.ReadDir(store.pluginsDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("want 2 stored binaries (one per plugin name), got %d", len(entries))
	}
}

func TestLoad_MissingPlugin(t *testing.T) {
	store := NewStore(t.TempDir())

	_, err := store.Load(map[string]LockedPlugin{"missing-plugin": {Hash: "deadbeef"}})
	if err == nil {
		t.Fatal("Load succeeded, want error")
	}
	if !strings.Contains(err.Error(), "missing-plugin") {
		t.Errorf("error should mention plugin name, got: %v", err)
	}
}

func TestLoad_InvalidPluginFile(t *testing.T) {
	store := NewStore(t.TempDir())

	hash, err := store.storeBinary(mustWriteTemp(t, "not a real plugin"), "bad-plugin")
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Load(map[string]LockedPlugin{"bad-plugin": {Hash: hash}})
	if err == nil {
		t.Fatal("Load succeeded, want error")
	}
	if !strings.Contains(err.Error(), "bad-plugin") {
		t.Errorf("error should mention plugin name, got: %v", err)
	}
}

func TestLoad_HashMismatch(t *testing.T) {
	store := NewStore(t.TempDir())

	hash, err := store.storeBinary(mustWriteTemp(t, "original content"), "corrupted")
	if err != nil {
		t.Fatal(err)
	}

	// Corrupt the stored binary after the fact — Load must catch the drift
	// rather than silently opening whatever's on disk.
	if err := os.WriteFile(store.binPath("corrupted", hash), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = store.Load(map[string]LockedPlugin{"corrupted": {Hash: hash}})
	if err == nil {
		t.Fatal("Load succeeded over a tampered binary, want a hash-mismatch error")
	}
	if !strings.Contains(err.Error(), "corrupted") {
		t.Errorf("error should mention plugin name, got: %v", err)
	}
}
