package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
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

func pluginKind(spec parse.PluginSpec) int {
	if spec.Source == "" {
		return pluginUnknown
	}

	if pPluginGitSSH.MatchString(spec.Source) || pPluginGitHTTPS.MatchString(spec.Source) {
		return pluginRemote
	}

	return pluginLocal
}

func buildPlugin(codePath, binPath string, spec parse.PluginSpec) (string, error) {
	soPath, err := filepath.Abs(path.Join(binPath, path.Base(spec.Source)+".so"))
	if err != nil {
		return "", fmt.Errorf("failed to get abspath for .so: %w", err)
	}

	cmd := exec.Command("go", "build", "-C", codePath, "-o", soPath, "-buildmode", "plugin")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to build %s: %w\noutput: %s", codePath, err, out)
	}

	return soPath, nil
}

func fetchPlugin(cachePath, binPath string, spec parse.PluginSpec) (string, error) {
	switch pluginKind(spec) {
	case pluginLocal:
		stat, err := os.Stat(spec.Source)
		if err != nil {
			return "", err
		}

		if stat.IsDir() {
			return buildPlugin(spec.Source, binPath, spec)
		}

		soPath := spec.Source
		if !filepath.IsAbs(soPath) {
			soPath = filepath.Join(binPath, soPath)
		}

		return filepath.Abs(soPath)
	case pluginRemote:
		pkgCache := path.Join(cachePath, spec.Name)
		cmdClone := exec.Command("git", "clone", spec.Source, pkgCache)
		if out, err := cmdClone.CombinedOutput(); err != nil {
			return "", fmt.Errorf("failed to clone %s: %w\noutput: %s", spec.Source, err, out)
		}

		if spec.Tag != "" {
			if out, err := exec.Command("git", "-C", pkgCache, "checkout", spec.Tag).CombinedOutput(); err != nil {
				return "", fmt.Errorf("failed to checkout %s: %w\noutput: %s", spec.Tag, err, out)
			}
		}

		return buildPlugin(pkgCache, binPath, spec)
	default:
		return "", fmt.Errorf("unable to find a suitable way to fetch %s: %#v", spec.Name, spec)
	}
}

// Fetch clones and builds the declared plugins, writing the name → .so path
// manifest to <initPath>/plugin.json. Used by the init command.
func Fetch(initPath string, specs []parse.PluginSpec) error {
	cachePath, err := os.MkdirTemp("", "psyduck-plugin-*")
	if err != nil {
		return fmt.Errorf("failed to make cache dir: %w", err)
	}
	defer os.RemoveAll(cachePath)

	binPath := path.Join(initPath, "plugins")
	if err := os.MkdirAll(binPath, os.ModeDir|os.ModePerm); err != nil {
		return fmt.Errorf("failed to create binpath: %w", err)
	}

	collected := make(map[string]string, len(specs))
	for _, spec := range specs {
		loc, err := fetchPlugin(cachePath, binPath, spec)
		if err != nil {
			return fmt.Errorf("unable to fetch %s: %w", spec.Name, err)
		}
		collected[spec.Name] = loc
	}

	b, err := json.Marshal(collected)
	if err != nil {
		return err
	}

	return os.WriteFile(path.Join(initPath, "plugin.json"), b, 0o644)
}
