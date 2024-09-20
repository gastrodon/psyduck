package configure

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"plugin"
	"regexp"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/psyduck-etl/sdk"
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

// TODO this isn't very robust
func getPluginKind(descriptor PluginDesc) int {
	if descriptor.Source == "" {
		return pluginUnknown
	}

	if pPluginGitSSH.Match([]byte(descriptor.Source)) || pPluginGitHTTPS.Match([]byte(descriptor.Source)) {
		return pluginRemote
	}

	return pluginLocal
}

func buildPlugin(codePath, binPath string, descriptor PluginDesc) (string, error) {
	soPath, err := filepath.Abs(path.Join(binPath, path.Base(descriptor.Source)+".so"))
	if err != nil {
		return "", fmt.Errorf("failed to get abspath for .so: %s", err)
	}

	cmdBuild := exec.Command("go", "build", "-C", codePath, "-o", soPath, "-buildmode", "plugin")
	println(strings.Join([]string{"go", "build", "-C", codePath, "-o", soPath, "-buildmode", "plugin"}, " "))
	if err := cmdBuild.Run(); err != nil {
		return "", fmt.Errorf("failed to build %s: %s\nstdout: %v\nstderr: %v", codePath, err, cmdBuild.Stdout, cmdBuild.Stderr)
	}

	return soPath, nil
}

/*
cachePath is a tmpdir to work in while building
binPath is where built .so files will live
*/

func fetchPlugin(cachePath, binPath string, descriptor PluginDesc) (string, error) {
	switch getPluginKind(descriptor) {
	case pluginLocal:
		stat, err := os.Stat(descriptor.Source)
		if err != nil {
			return "", err
		}

		if stat.IsDir() {
			soPath, err := buildPlugin(descriptor.Source, binPath, descriptor)
			if err != nil {
				return "", fmt.Errorf("failed to build local plugin: %s", err)
			}

			return soPath, nil
		}

		soPath := descriptor.Source
		if !filepath.IsAbs(soPath) {
			soPath = filepath.Join(binPath, soPath)
		}

		return filepath.Abs(soPath)
	case pluginRemote:
		pkgCache := path.Join(cachePath, descriptor.Name)
		cmdClone := exec.Command("git", "clone", descriptor.Source, pkgCache)
		println(strings.Join([]string{"git", "clone", descriptor.Source, pkgCache}, " "))
		if err := cmdClone.Run(); err != nil {
			return "", fmt.Errorf("failed to clone %s: %s\nstdout: %v\nstderr: %v", descriptor.Source, err, cmdClone.Stdout, cmdClone.Stderr)
		}

		if descriptor.Tag != "" {
			println(strings.Join([]string{"git", "-C", pkgCache, "checkout", descriptor.Tag}, " "))
			if err := exec.Command("git", "-C", pkgCache, "checkout", descriptor.Tag).Run(); err != nil {
				return "", fmt.Errorf("failed to checkout %s: %s", descriptor.Tag, err)
			}
		}

		return buildPlugin(pkgCache, binPath, descriptor)
	default:
		return "", fmt.Errorf(
			"unable to find a suitable way to fetch %s! descriptor:\n%#v",
			descriptor.Name, descriptor,
		)
	}
}

func fetchPlugins(cachePath, binPath string, descriptors []PluginDesc) (map[string]string, error) {
	collected := make(map[string]string, len(descriptors))
	for _, desc := range descriptors {
		loc, err := fetchPlugin(cachePath, binPath, desc)
		if err != nil {
			return nil, fmt.Errorf("unable to fetch %s: %s", desc.Name, err)
		}

		collected[desc.Name] = loc
	}

	return collected, nil
}

/*
Fetch plugins, cloning and building them if necessary
Returns an absolute filepath pointing to a loadable shared library
*/
func FetchPlugins(initPath, filename string, literal []byte, _ *hcl.EvalContext) (map[string]string, error) {
	descriptors, diags := ParsePluginsDesc(filename, literal)
	if diags.HasErrors() {
		return nil, diags
	}

	cachePath, err := os.MkdirTemp("", "psyduck-plugin-*")
	if err != nil {
		return nil, fmt.Errorf("failed to cache dir: %s", err)
	}

	binPath := path.Join(initPath, "plugins")
	if err := os.MkdirAll(binPath, os.ModeDir|os.ModePerm); err != nil {
		return nil, fmt.Errorf("failed to create binpath: %s", err)
	}

	collected, err := fetchPlugins(cachePath, binPath, descriptors)
	if err != nil {
		return nil, fmt.Errorf("failed to collect: %s", err)
	}

	if err := os.RemoveAll(cachePath); err != nil {
		return nil, fmt.Errorf("failed to rm cachepath: %s", err)
	}

	return collected, nil
}

/*
Load a plugin by opening with go-plugin and calling its Plugin func
*/
func loadPlugin(pluginPath string, descriptor PluginDesc) (*sdk.Plugin, error) {
	plugin, err := plugin.Open(pluginPath)
	if err != nil {
		return nil, fmt.Errorf("failed loading the library providing %s ( %s @ %s ):\n%s",
			descriptor.Name, descriptor.Source, pluginPath, err)
	}

	makePluginSym, err := plugin.Lookup("Plugin")
	if err != nil {
		return nil, fmt.Errorf("failed loading the provider func for %s:\n%s", descriptor.Name, err)
	}

	if makePluginSym == nil {
		return nil, fmt.Errorf("failed loading the provider func for %s:\nLookup \"Plugin\" is nil", descriptor.Name)
	}

	makePlugin, ok := makePluginSym.(func() *sdk.Plugin)
	if !ok {
		return nil, fmt.Errorf("failed loading the provider func for %s:\nnot OK: %+v", descriptor.Name, makePluginSym)
	}

	return makePlugin(), nil
}

func loadPlugins(binPaths map[string]string, descriptors []PluginDesc) ([]*sdk.Plugin, error) {
	plugins := make([]*sdk.Plugin, len(descriptors))
	for i, descriptor := range descriptors {
		binPath, ok := binPaths[descriptor.Name]
		if !ok {
			return nil, fmt.Errorf("binary not found for plugin %s", descriptor.Name)
		}

		plugin, err := loadPlugin(binPath, descriptor)
		if err != nil {
			return nil, fmt.Errorf("unable to load plugin %s: %s", descriptor.Name, err)
		}

		plugins[i] = plugin
	}

	return plugins, nil
}

/*
Load plugins that've been fetched and are pointed to in <initPath>/plugin.json
*/
func LoadPlugins(initPath, filename string, literal []byte, evalCtx *hcl.EvalContext) ([]*sdk.Plugin, error) {
	descriptors, diags := ParsePluginsDesc(filename, literal)
	if diags.HasErrors() {
		return nil, diags
	}

	b, err := os.ReadFile(path.Join(initPath, "plugin.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to read plugin.json: %s", err)
	}

	binPaths := make(map[string]string, len(descriptors))
	if err := json.Unmarshal(b, &binPaths); err != nil {
		return nil, fmt.Errorf("failed to decode binPaths: %s", err)
	}

	loaded, err := loadPlugins(binPaths, descriptors)
	if err != nil {
		return nil, fmt.Errorf("failed to load plugins from json: %s", err)
	}

	return loaded, nil
}

type PluginDesc struct {
	Name   string `hcl:"name,label"`
	Source string `hcl:"source"`
	Tag    string `hcl:"tag,optional"`
}

/*
For parsing plugin descriptor bocks
```

	plugin "name" {
		source = string
		tag 	 = string
	}

```
*/
func ParsePluginsDesc(filename string, literal []byte) ([]PluginDesc, hcl.Diagnostics) {
	file, diags := hclparse.NewParser().ParseHCL(literal, filename)
	if diags.HasErrors() {
		return nil, diags
	}

	target := new(struct {
		hcl.Body `hcl:",remain"`
		Blocks   []PluginDesc `hcl:"plugin,block"`
	})
	if diags := gohcl.DecodeBody(file.Body, &hcl.EvalContext{}, target); diags.HasErrors() {
		return nil, diags
	}

	return target.Blocks, make(hcl.Diagnostics, 0)
}
