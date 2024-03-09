package configure

import (
	"fmt"
	"go/build"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"plugin"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/psyduck-std/sdk"
)

const (
	pluginUnknown = iota
	pluginLocal
	pluginRemote
)

// TODO this isn't very robust
func getPluginKind(descriptor pluginSource) int {
	if descriptor.Source == "" {
		return pluginUnknown
	}

	if build.IsLocalImport(descriptor.Source) {
		return pluginLocal
	}

	return pluginRemote
}

/*
Fetch plugins, cloning and building them if necessary
Returns an absolute filepath pointing to a loadable shared library
*/
func fetchPlugin(cachePath, basePath string, descriptor pluginSource) (string, error) {
	switch getPluginKind(descriptor) {
	case pluginLocal:
		soPath := descriptor.Source

		if !filepath.IsAbs(soPath) {
			soPath = filepath.Join(basePath, soPath)
		}

		return filepath.Abs(soPath)

	case pluginRemote:
		soPath, err := filepath.Abs(path.Join(basePath, path.Base(descriptor.Source)+".so"))
		if err != nil {
			return "", err
		}

		pkgCache := path.Join(cachePath, descriptor.Source)
		cmdClone := exec.Command("git", "clone", descriptor.Source, pkgCache)
		println(strings.Join([]string{"git", "clone", descriptor.Source, pkgCache}, " "))
		if err := cmdClone.Run(); err != nil {
			return "", fmt.Errorf("failed to clone %s: %s\nstdout: %v\nstderr: %v", descriptor.Source, err, cmdClone.Stdout, cmdClone.Stderr)
		}

		cmdBuild := exec.Command("go", "build", "-C", pkgCache, "-o", soPath, "-buildmode", "plugin")
		println(strings.Join([]string{"go", "build", "-C", pkgCache, "-o", soPath, "-buildmode", "plugin"}, " "))
		if err := cmdBuild.Run(); err != nil {
			return "", fmt.Errorf("failed to build %s: %s\nstdout: %v\nstderr: %v", pkgCache, err, cmdBuild.Stdout, cmdBuild.Stderr)
		}

		return soPath, nil

	default:
		return "", fmt.Errorf(
			"unable to find a suitable way to fetch %s! descriptor:\n%#v",
			descriptor.Name, descriptor,
		)
	}
}

func loadPlugin(cachePath, basePath string, descriptor pluginSource) (*sdk.Plugin, *hcl.Diagnostic) {
	pluginPath, err := fetchPlugin(cachePath, basePath, descriptor)
	if err != nil {
		return nil, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "cannot fetch plugin",
			Detail: fmt.Sprintf(
				"failed fetching the library providing %s ( %s ):\n%s",
				descriptor.Name, descriptor.Source, err,
			),
		}
	}

	plugin, err := plugin.Open(pluginPath)
	if err != nil {
		return nil, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "cannot load plugin",
			Detail: fmt.Sprintf(
				"failed loading the library providing %s ( %s @ %s ):\n%s",
				descriptor.Name, descriptor.Source, pluginPath, err,
			),
		}
	}

	makePluginSym, err := plugin.Lookup("Plugin")
	if err != nil {
		return nil, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "cannot load provider func",
			Detail: fmt.Sprintf(
				"failed loading the provider func for %s:\n%s",
				descriptor.Name, err,
			),
		}
	}

	if makePluginSym == nil {
		return nil, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "cannot load provider func",
			Detail: fmt.Sprintf(
				"failed loading the provider func for %s:\nLookup \"Plugin\" is nil",
				descriptor.Name,
			),
		}
	}

	makePlugin, ok := makePluginSym.(func() *sdk.Plugin)
	if !ok {
		return nil, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "cannot load provider func",
			Detail: fmt.Sprintf(
				"failed loading the provider func for %s:\nnot OK: %+v",
				descriptor.Name, makePluginSym,
			),
		}
	}

	return makePlugin(), nil
}

func loadPlugins(cachePath, basePath string, descriptors *Plugins) (map[string]*sdk.Plugin, hcl.Diagnostics) {
	plugins := make(map[string]*sdk.Plugin, len(descriptors.Blocks))
	diags := make(hcl.Diagnostics, len(descriptors.Blocks))
	diagIndex := 0
	for _, descriptor := range descriptors.Blocks {
		if plugin, diag := loadPlugin(cachePath, basePath, descriptor); diag != nil {
			diags[diagIndex] = diag
			diagIndex++
		} else {
			plugins[plugin.Name] = plugin
		}
	}

	return plugins, diags[:diagIndex]
}

func readPluginBlocks(filename string, literal []byte, context *hcl.EvalContext) (*Plugins, hcl.Diagnostics) {
	target := new(Plugins)
	if file, diags := hclparse.NewParser().ParseHCL(literal, filename); diags != nil {
		return nil, diags
	} else {
		gohcl.DecodeBody(file.Body, context, target)
		return target, make(hcl.Diagnostics, 0)
	}
}

func LoadPlugins(basePath, filename string, literal []byte, context *hcl.EvalContext) (map[string]*sdk.Plugin, hcl.Diagnostics) {
	basePathAbs, err := filepath.Abs(basePath)
	if err != nil {
		return nil, []*hcl.Diagnostic{{
			Severity: hcl.DiagError,
			Summary:  "failed to resolve plugin output dir",
			Detail: fmt.Sprintf(
				"failed to resolve plugin output dir %s:\n%s",
				basePath, err,
			),
		}}
	}

	if descriptors, diags := readPluginBlocks(filename, literal, context); diags.HasErrors() {
		return nil, diags
	} else {
		cachePath, err := os.MkdirTemp("", "psyduck-plugin-*")
		if err != nil {
			return nil, []*hcl.Diagnostic{{
				Severity: hcl.DiagError,
				Summary:  "failed to create build cache dir",
				Detail:   fmt.Sprintf("failed to create build cache dir:\n%s", err),
			}}
		}

		loaded, diags := loadPlugins(cachePath, basePathAbs, descriptors)
		if err := os.RemoveAll(cachePath); err != nil {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagWarning,
				Summary:  "failed to cleanup build cache dir",
				Detail: fmt.Sprintf(
					"failed to cleanup build cache dir at %s:\n%s",
					cachePath, err,
				),
			})
		}

		return loaded, diags
	}
}
