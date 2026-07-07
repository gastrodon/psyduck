package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNewStore_ResolvesToAbsolute(t *testing.T) {
	store := NewStore(".psyduck")
	if !filepath.IsAbs(store.root) {
		t.Errorf("root = %q, want absolute", store.root)
	}

	const abs = "/tmp/psyduck-store"
	if s := NewStore(abs); s.root != abs {
		t.Errorf("root = %q, want %q", s.root, abs)
	}
}

func TestStorePaths(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)

	cases := []struct {
		name     string
		got, want string
	}{
		{"pluginsDir", store.pluginsDir(), filepath.Join(root, "plugins")},
		{"manifestPath", store.manifestPath(), filepath.Join(root, "plugin.json")},
		{"soPath", store.soPath("myplugin"), filepath.Join(root, "plugins", "myplugin.so")},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestReadManifest_MissingReturnsEmpty(t *testing.T) {
	store := NewStore(t.TempDir())
	m, err := store.readManifest()
	if err != nil {
		t.Fatalf("readManifest: %v", err)
	}
	if len(m) != 0 {
		t.Errorf("manifest = %v, want empty", m)
	}
}

func TestReadManifest_MalformedErrors(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := os.WriteFile(store.manifestPath(), []byte(`{invalid`), 0o644); err != nil {
		t.Fatalf("write malformed manifest: %v", err)
	}
	if _, err := store.readManifest(); err == nil {
		t.Error("readManifest on malformed JSON: want error, got nil")
	}
}

func TestManifest_RoundTrip(t *testing.T) {
	store := NewStore(t.TempDir())
	want := map[string]string{
		"plugin1": "/path/to/plugin1.so",
		"plugin2": "/path/to/plugin2.so",
	}
	if err := store.writeManifest(want); err != nil {
		t.Fatalf("writeManifest: %v", err)
	}
	got, err := store.readManifest()
	if err != nil {
		t.Fatalf("readManifest: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("manifest = %v, want %v", got, want)
	}

	// On-disk format is JSON — users may inspect plugin.json directly.
	data, err := os.ReadFile(store.manifestPath())
	if err != nil {
		t.Fatalf("read manifest file: %v", err)
	}
	var parsed map[string]string
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Errorf("manifest on disk is not valid JSON: %v", err)
	}
}
