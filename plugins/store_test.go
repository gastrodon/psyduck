package plugins

import (
	"os"
	"path/filepath"
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
		name      string
		got, want string
	}{
		{"pluginsDir", store.pluginsDir(), filepath.Join(root, "plugins")},
		{"hashPath", store.hashPath("deadbeef"), filepath.Join(root, "plugins", "deadbeef.so")},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestStoreBinary_ContentAddressed(t *testing.T) {
	store := NewStore(t.TempDir())

	src := filepath.Join(t.TempDir(), "plugin.so")
	if err := os.WriteFile(src, []byte("fake plugin bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	hash, err := store.storeBinary(src)
	if err != nil {
		t.Fatalf("storeBinary: %v", err)
	}
	if hash == "" {
		t.Fatal("storeBinary returned empty hash")
	}

	got, err := os.ReadFile(store.hashPath(hash))
	if err != nil {
		t.Fatalf("stored binary not found at hash path: %v", err)
	}
	if string(got) != "fake plugin bytes" {
		t.Errorf("stored content = %q, want %q", got, "fake plugin bytes")
	}
}

func TestStoreBinary_DedupesIdenticalContent(t *testing.T) {
	store := NewStore(t.TempDir())

	dir := t.TempDir()
	a := filepath.Join(dir, "a.so")
	b := filepath.Join(dir, "b.so")
	if err := os.WriteFile(a, []byte("same bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("same bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	hashA, err := store.storeBinary(a)
	if err != nil {
		t.Fatal(err)
	}
	hashB, err := store.storeBinary(b)
	if err != nil {
		t.Fatal(err)
	}
	if hashA != hashB {
		t.Errorf("identical content hashed differently: %q vs %q", hashA, hashB)
	}

	entries, err := os.ReadDir(store.pluginsDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("want exactly 1 stored binary after dedup, got %d", len(entries))
	}
}

func mustWriteTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plugin.so")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
