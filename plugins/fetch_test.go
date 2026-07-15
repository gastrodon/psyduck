package plugins

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gastrodon/psyduck/parse"
)

func TestPluginKind(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   int
	}{
		{"empty", "", pluginUnknown},
		{"git ssh", "git@github.com:foo/bar.git", pluginRemote},
		{"https", "https://github.com/foo/bar.git", pluginRemote},
		{"https no .git suffix", "https://github.com/user/repo", pluginRemote},
		{"relative dot-slash", "./relative/path", pluginLocal},
		{"relative bare", "relative/path", pluginLocal},
		{"absolute", "/abs/path/to/plugin.so", pluginLocal},
		{"tilde-home", "~/plugins/myplugin", pluginLocal},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := pluginKind(parse.Plugin{Source: c.source}); got != c.want {
				t.Errorf("pluginKind(%q) = %d, want %d", c.source, got, c.want)
			}
		})
	}
}

// mustGit runs a git command in dir, failing the test on error and
// returning trimmed stdout+stderr.
func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestResolveRef exercises the three cases resolveRef distinguishes,
// against a real local repo: HEAD on a branch, HEAD exactly at a tag, and
// detached at a bare commit (what checking out a raw SHA leaves you at).
func TestResolveRef(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: shells out to git commit, which invokes ambient pre-commit hook")
	}
	dir := t.TempDir()
	mustGit(t, dir, "init", "-q", "-b", "main")
	mustGit(t, dir, "config", "user.email", "test@test.com")
	mustGit(t, dir, "config", "user.name", "test")
	mustGit(t, dir, "config", "core.hooksPath", "/dev/null")

	write := func(content string) {
		if err := os.WriteFile(filepath.Join(dir, "f"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("a")
	mustGit(t, dir, "add", "f")
	mustGit(t, dir, "commit", "-q", "-m", "first")
	mustGit(t, dir, "tag", "v1.0.0")

	write("b")
	mustGit(t, dir, "commit", "-q", "-am", "second")
	secondSHA := mustGit(t, dir, "rev-parse", "HEAD") // untagged, so a real fallback case
	mustGit(t, dir, "checkout", "-q", "-b", "feature")

	t.Run("branch", func(t *testing.T) {
		mustGit(t, dir, "checkout", "-q", "feature")
		ref, err := resolveRef(dir)
		if err != nil {
			t.Fatal(err)
		}
		if ref != "refs/heads/feature" {
			t.Errorf("ref = %q, want refs/heads/feature", ref)
		}
	})

	t.Run("tag", func(t *testing.T) {
		mustGit(t, dir, "checkout", "-q", "v1.0.0")
		ref, err := resolveRef(dir)
		if err != nil {
			t.Fatal(err)
		}
		if ref != "refs/tags/v1.0.0" {
			t.Errorf("ref = %q, want refs/tags/v1.0.0", ref)
		}
	})

	t.Run("detached sha", func(t *testing.T) {
		mustGit(t, dir, "checkout", "-q", secondSHA)
		ref, err := resolveRef(dir)
		if err != nil {
			t.Fatal(err)
		}
		if ref != secondSHA {
			t.Errorf("ref = %q, want %q", ref, secondSHA)
		}
	})
}

// TestBuild_RemoteSource covers the whole remote plugin path: clone,
// resolveRef, go build, storeBinary. The repo holds a dep-free main package —
// Build never dials the binary, so it doesn't need to be a real psyduck
// plugin, and a module with no requirements builds with no network or module
// cache.
//
// pluginKind only treats git@/https:// sources as remote, so the test clones
// a fake https URL that a git insteadOf rewrite (scoped to a throwaway
// GIT_CONFIG_GLOBAL) redirects to the local repo.
func TestBuild_RemoteSource(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: shells out to git commit, which invokes ambient pre-commit hook")
	}

	repo := t.TempDir()
	mustGit(t, repo, "init", "-q", "-b", "main")
	mustGit(t, repo, "config", "user.email", "test@test.com")
	mustGit(t, repo, "config", "user.name", "test")
	mustGit(t, repo, "config", "core.hooksPath", "/dev/null")

	writeFile := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("go.mod", "module dummyplugin\n\ngo 1.21\n")
	writeFile("main.go", "package main\n\nfunc main() {}\n")
	mustGit(t, repo, "add", ".")
	mustGit(t, repo, "commit", "-q", "-m", "dummy plugin")

	const source = "https://psyduck.invalid/dummy.git"
	gitConfig := filepath.Join(t.TempDir(), "gitconfig")
	rewrite := fmt.Sprintf("[url %q]\n\tinsteadOf = %s\n", "file://"+repo, source)
	if err := os.WriteFile(gitConfig, []byte(rewrite), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", gitConfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	store := NewStore(t.TempDir())
	locked, err := store.Build([]parse.Plugin{{Name: "dummy", Source: source}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	entry, ok := locked["dummy"]
	if !ok {
		t.Fatalf("no lock entry for dummy: %#v", locked)
	}
	if entry.Ref != "refs/heads/main" {
		t.Errorf("resolved ref = %q, want refs/heads/main", entry.Ref)
	}
	stat, err := os.Stat(store.binPath("dummy", entry.Hash))
	if err != nil {
		t.Fatalf("stored binary: %v", err)
	}
	if !stat.Mode().IsRegular() {
		t.Errorf("stored binary mode = %v, want a regular file", stat.Mode())
	}
}
