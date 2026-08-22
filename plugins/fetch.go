package plugins

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gastrodon/psyduck/parse"
)

const (
	pluginUnknown = iota
	pluginLocal
	pluginRemote
)

var (
	pPluginGitSSH   = regexp.MustCompile(`git@.+:.*`)
	pPluginGitHTTPS = regexp.MustCompile(`https:\/\/.*`)
)

func pluginKind(spec parse.Plugin) int {
	if spec.Source == "" {
		return pluginUnknown
	}
	if pPluginGitSSH.MatchString(spec.Source) || pPluginGitHTTPS.MatchString(spec.Source) {
		return pluginRemote
	}
	return pluginLocal
}

// fetcher is an ephemeral helper created by Store.Build. It holds the
// temporary clone/build directory; every binary it produces is handed to
// the store to be content-addressed before fetch returns — the store
// itself is never told a plugin's name, only its bytes.
type fetcher struct {
	store  *Store
	tmpDir string
}

func (f *fetcher) cloneDir(spec parse.Plugin) string {
	return filepath.Join(f.tmpDir, spec.Name)
}

func (f *fetcher) cleanup() {
	os.RemoveAll(f.tmpDir)
}

// build compiles codePath into a temporary plugin executable, using the
// toolchain spec.BuildMode names. It never writes directly into the
// store — the store only knows binaries by content hash, which isn't
// known until after the build produces bytes to hash. Plugins run as
// subprocesses (see sdk/rpc) regardless of toolchain: no
// -buildmode=plugin, no toolchain/race parity with the host.
func (f *fetcher) build(codePath string, spec parse.Plugin) (string, error) {
	switch spec.BuildMode {
	case "", parse.BuildModeGo:
		return f.buildGo(codePath, spec)
	case parse.BuildModeBun:
		return f.buildBun(codePath)
	case parse.BuildModeBin:
		return resolveBin(codePath, spec.Name)
	default:
		return "", fmt.Errorf("plugin %s: unknown buildmode %q", spec.Name, spec.BuildMode)
	}
}

// buildGo runs `go build -C codePath -o <tmpOut>`, the toolchain's own
// build orchestration for a Go plugin's package layout.
func (f *fetcher) buildGo(codePath string, spec parse.Plugin) (string, error) {
	// The ".bin" suffix keeps the output distinct from cloneDir: a remote
	// plugin's clone already sits at <tmpDir>/<name>, and a build tool's
	// `-o`/`--outfile` pointed at an existing directory doesn't fail — it
	// silently writes the binary inside it, leaving nothing at the path we
	// hand back.
	tmpOut := filepath.Join(f.tmpDir, spec.Name+".bin")

	cmd := exec.Command("go", "build", "-C", codePath, "-o", tmpOut)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to build %s: %w\noutput: %s", codePath, err, out)
	}
	return tmpOut, nil
}

// buildBun runs a bun plugin's own build: `bun install` against its
// committed lockfile, then its `build-plugin` package.json script, which
// is expected to write the built executable to ./plugin (relative to
// codePath) — see docs/plugins.md. What that script actually runs
// (--compile, externals, entry point) is the plugin's own business, the
// same way fetch.go never looks inside a Go plugin's package layout. The
// build's side effects (node_modules, ./plugin) are left in codePath,
// same as any other build tooling writing into a checkout.
func (f *fetcher) buildBun(codePath string) (string, error) {
	install := exec.Command("bun", "install", "--frozen-lockfile")
	install.Dir = codePath
	if out, err := install.CombinedOutput(); err != nil {
		return "", fmt.Errorf("bun install failed: %w\noutput: %s", err, out)
	}

	build := exec.Command("bun", "run", "build-plugin")
	build.Dir = codePath
	if out, err := build.CombinedOutput(); err != nil {
		return "", fmt.Errorf("bun run build-plugin failed: %w\noutput: %s", err, out)
	}

	out := filepath.Join(codePath, "plugin")
	if _, err := os.Stat(out); err != nil {
		return "", fmt.Errorf("bun build-plugin did not produce %s: %w", out, err)
	}
	return out, nil
}

// resolveBin returns codePath as-is if it's a file, erroring if it's a
// directory (which a git-clone codePath always is).
func resolveBin(codePath, name string) (string, error) {
	stat, err := os.Stat(codePath)
	if err != nil {
		return "", err
	}
	if stat.IsDir() {
		return "", fmt.Errorf("plugin %s: buildmode \"bin\" requires source to be a file, not a directory", name)
	}
	return codePath, nil
}

func (f *fetcher) clone(spec parse.Plugin) (string, error) {
	cloneDir := f.cloneDir(spec)
	if out, err := exec.Command("git", "clone", spec.Source, cloneDir).CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to clone %s: %w\noutput: %s", spec.Source, err, out)
	}
	if spec.Tag != "" {
		if out, err := exec.Command("git", "-C", cloneDir, "checkout", spec.Tag).CombinedOutput(); err != nil {
			return "", fmt.Errorf("failed to checkout %s: %w\noutput: %s", spec.Tag, err, out)
		}
	}
	return cloneDir, nil
}

// resolveRef reports the most specific git reference HEAD actually
// resolves to in cloneDir, after cloning and any requested checkout:
// a branch (refs/heads/<name>) if HEAD is symbolic, else a tag
// (refs/tags/<name>) if HEAD exactly matches one, else the commit's full
// SHA. Called unconditionally for every remote plugin — even one with no
// `tag` attribute still lands on some real commit, and that's what gets
// recorded.
func resolveRef(cloneDir string) (string, error) {
	if out, err := exec.Command("git", "-C", cloneDir, "symbolic-ref", "-q", "HEAD").CombinedOutput(); err == nil {
		return strings.TrimSpace(string(out)), nil
	}

	if out, err := exec.Command("git", "-C", cloneDir, "describe", "--tags", "--exact-match", "HEAD").CombinedOutput(); err == nil {
		return "refs/tags/" + strings.TrimSpace(string(out)), nil
	}

	out, err := exec.Command("git", "-C", cloneDir, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to resolve HEAD in %s: %w\noutput: %s", cloneDir, err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

// fetch resolves spec (building it first if it's source code) and
// content-addresses the resulting binary into the store, returning its
// hash and, for remote sources, the actual git ref that got checked out.
// A local buildmode="bin" source is read and stored as-is (relative to
// the current working directory, same as any other file argument — the
// store no longer needs it to be absolute since it only reads it once,
// here, to copy its bytes in); it must point directly at the executable
// file, not a directory.
func (f *fetcher) fetch(spec parse.Plugin) (hash, resolve string, err error) {
	switch pluginKind(spec) {
	case pluginLocal:
		stat, err := os.Stat(spec.Source)
		if err != nil {
			return "", "", err
		}
		// A directory is always something to build; a bare file only
		// counts as an already-built binary when buildmode says so.
		if spec.BuildMode != parse.BuildModeBin && !stat.IsDir() {
			return "", "", fmt.Errorf("plugin %s: local source %s is not a directory; set buildmode = \"bin\" for a prebuilt binary", spec.Name, spec.Source)
		}
		built, err := f.build(spec.Source, spec)
		if err != nil {
			return "", "", err
		}
		hash, err := f.store.storeBinary(built, spec.Name)
		return hash, "", err
	case pluginRemote:
		cloneDir, err := f.clone(spec)
		if err != nil {
			return "", "", err
		}
		resolve, err := resolveRef(cloneDir)
		if err != nil {
			return "", "", err
		}
		built, err := f.build(cloneDir, spec)
		if err != nil {
			return "", "", err
		}
		hash, err := f.store.storeBinary(built, spec.Name)
		return hash, resolve, err
	default:
		return "", "", fmt.Errorf("unable to find a suitable way to fetch %s: %#v", spec.Name, spec)
	}
}
