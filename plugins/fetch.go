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
// temporary clone directory alongside the store so it can delegate all
// persistent-path questions back to the store.
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

func (f *fetcher) build(codePath string, spec parse.Plugin) (string, error) {
	soPath := f.store.soPath(spec.Name)
	cmd := exec.Command("go", "build", "-C", codePath, "-o", soPath, "-buildmode", "plugin")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to build %s: %w\noutput: %s", codePath, err, out)
	}
	return soPath, nil
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

func (f *fetcher) fetch(spec parse.Plugin) (string, error) {
	switch pluginKind(spec) {
	case pluginLocal:
		stat, err := os.Stat(spec.Source)
		if err != nil {
			return "", err
		}
		if stat.IsDir() {
			return f.build(spec.Source, spec)
		}
		soPath := spec.Source
		if !filepath.IsAbs(soPath) {
			soPath = filepath.Join(f.store.pluginsDir(), soPath)
		}
		return filepath.Abs(soPath)
	case pluginRemote:
		cloneDir, err := f.clone(spec)
		if err != nil {
			return "", err
		}
		return f.build(cloneDir, spec)
	default:
		return "", fmt.Errorf("unable to find a suitable way to fetch %s: %#v", spec.Name, spec)
	}
}
