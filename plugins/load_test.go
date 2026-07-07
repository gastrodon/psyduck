//go:build linux || darwin

package plugins

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/psyduck-etl/sdk"
)

// jsonBlock is a ConfigBlock backed by a JSON blob, mirroring the SDK's own
// test pattern. The example plugin's config struct carries json: tags
// alongside psy: so this decodes without pulling in the host's parser.
type jsonBlock struct{ data []byte }

func (b jsonBlock) Origin() sdk.SourceRange { return sdk.SourceRange{SourceName: "test"} }
func (b jsonBlock) Decode(dst any) error {
	if len(b.data) == 0 {
		return nil
	}
	return json.Unmarshal(b.data, dst)
}

// buildExamplePlugin compiles cmd/example-plugin to a .so at outPath. Tests
// run from the package directory, so the module root is one level up.
func buildExamplePlugin(t *testing.T, outPath string) {
	t.Helper()
	src, err := filepath.Abs("../cmd/example-plugin")
	if err != nil {
		t.Fatalf("abs example plugin path: %v", err)
	}
	cmd := exec.Command("go", "build", "-C", src, "-o", outPath, "-buildmode", "plugin")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build example plugin: %v\n%s", err, out)
	}
}

func TestLoad_Integration(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := os.MkdirAll(store.pluginsDir(), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}

	soPath := store.soPath("example-plugin")
	buildExamplePlugin(t, soPath)

	if err := store.writeManifest(map[string]string{"example-plugin": soPath}); err != nil {
		t.Fatalf("writeManifest: %v", err)
	}

	plugins, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
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
	inst, err := p.Bind(sdk.PRODUCER, "constant", block)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	send := make(chan []byte, 4)
	errs := make(chan error, 1)
	go inst.Produce(send, errs)

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

func TestLoad_MissingPlugin(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.writeManifest(map[string]string{"missing-plugin": "/nonexistent/plugin.so"}); err != nil {
		t.Fatalf("writeManifest: %v", err)
	}

	_, err := store.Load()
	if err == nil {
		t.Fatal("Load succeeded, want error")
	}
	if !strings.Contains(err.Error(), "missing-plugin") {
		t.Errorf("error should mention plugin name, got: %v", err)
	}
}

func TestLoad_InvalidPluginFile(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := os.MkdirAll(store.pluginsDir(), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}

	badPath := filepath.Join(store.pluginsDir(), "bad.so")
	if err := os.WriteFile(badPath, nil, 0o644); err != nil {
		t.Fatalf("write bad plugin: %v", err)
	}
	if err := store.writeManifest(map[string]string{"bad-plugin": badPath}); err != nil {
		t.Fatalf("writeManifest: %v", err)
	}

	_, err := store.Load()
	if err == nil {
		t.Fatal("Load succeeded, want error")
	}
	if !strings.Contains(err.Error(), "bad-plugin") {
		t.Errorf("error should mention plugin name, got: %v", err)
	}
}
