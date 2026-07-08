package plugins

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

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

// build compiles codePath into a temporary .so. It never writes directly
// into the store — the store only knows binaries by content hash, which
// isn't known until after the build produces bytes to hash.
func (f *fetcher) build(codePath string, spec parse.Plugin) (string, error) {
	tmpOut := filepath.Join(f.tmpDir, spec.Name+".so")
	cmd := exec.Command("go", "build", "-C", codePath, "-o", tmpOut, "-buildmode", "plugin")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to build %s: %w\noutput: %s", codePath, err, out)
	}
	return tmpOut, nil
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

// fetch resolves spec (building it first if it's source code) and
// content-addresses the resulting binary into the store, returning its
// hash. A local .so source is read and stored as-is, relative to the
// current working directory same as any other file argument — the store
// no longer needs it to be absolute since it only reads it once, here, to
// copy its bytes in.
func (f *fetcher) fetch(spec parse.Plugin) (string, error) {
	switch pluginKind(spec) {
	case pluginLocal:
		stat, err := os.Stat(spec.Source)
		if err != nil {
			return "", err
		}
		if stat.IsDir() {
			built, err := f.build(spec.Source, spec)
			if err != nil {
				return "", err
			}
			return f.store.storeBinary(built)
		}
		return f.store.storeBinary(spec.Source)
	case pluginRemote:
		cloneDir, err := f.clone(spec)
		if err != nil {
			return "", err
		}
		built, err := f.build(cloneDir, spec)
		if err != nil {
			return "", err
		}
		return f.store.storeBinary(built)
	default:
		return "", fmt.Errorf("unable to find a suitable way to fetch %s: %#v", spec.Name, spec)
	}
}
