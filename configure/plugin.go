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

	"github.com/gastrodon/psyduck/sdk"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
)

/*
Fetch and build a plugin, writing the binary to root, returning the filename of the compiled library
cache is where we will pull pkg to before building it
root is where we want to build the package out to
pkg is the name of the package - should mostly be stuff at github.com/psyduck-std/...
*/
func fetchPlugin(cache, root, pkg string) (string, error) {
	pkgCache := path.Join(cache, pkg)
	cmdClone := exec.Command("git", "clone", pkg, pkgCache)
	println(strings.Join([]string{"git", "clone", pkg, pkgCache}, " "))
	if err := cmdClone.Run(); err != nil {
		return "", fmt.Errorf("failed to clone %s: %sstdout: \n%s\nstderr: %s", pkg, err, cmdClone.Stdout, cmdClone.Stderr)
	}

	fileName := path.Base(pkg)
	cmdBuild := exec.Command("go", "build", "-C", pkgCache, "-o", path.Join(root, fileName), "-buildmode", "plugin")
	println(strings.Join([]string{"go", "build", "-C", pkgCache, "-o", path.Join(root, fileName), "-buildmode", "plugin"}, " "))
	if err := cmdBuild.Run(); err != nil {
		return "", fmt.Errorf("failed to build %s: %sstdout: \n%s\nstderr: %s", pkgCache, err, cmdBuild.Stdout, cmdBuild.Stderr)
	}

	return fileName, nil
}

func loadPlugin(_ *build.Context, cachePath, basePath string, descriptor pluginSource) (*sdk.Plugin, *hcl.Diagnostic) {
	if descriptor.Source == "" {
		return nil, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "no plugin loader found",
			Detail: fmt.Sprintf(
				"unable to find a suitable way to load the plugin %s with the descriptor\n%#v",
				descriptor.Name, descriptor,
			),
		}
	}

	var pluginPath string
	if !build.IsLocalImport(descriptor.Source) {
		println("WANT REMOTE")
		filename, err := fetchPlugin(cachePath, basePath, descriptor.Source)
		if err != nil {
			return nil, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "failed to import package",
				Detail: fmt.Sprintf(
					"unable to import %s/%s ( sourced by plugin %s):\n%s",
					basePath, descriptor.Source, descriptor.Name, err,
				),
			}
		}

		pluginPath = path.Join(basePath, filename)
	} else {
		println("WANT LOCAL")
		relPath := descriptor.Source
		if !filepath.IsAbs(relPath) {
			relPath = filepath.Join(basePath, relPath)
		}

		var err error
		pluginPath, err = filepath.Abs(relPath)
		if err != nil {
			return nil, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "failed to import package",
				Detail: fmt.Sprintf(
					"unable to import %s/%s ( sourced by plugin %s):\n%s",
					basePath, relPath, descriptor.Name, err,
				),
			}
		}
	}

	plugin, err := plugin.Open(pluginPath)
	if err != nil {
		return nil, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "cannot load plugin",
			Detail: fmt.Sprintf(
				"failed loading the library providing %s ( loading file %s ):\n%s",
				descriptor.Name, descriptor.Source, err,
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

	makePlugin, ok := makePluginSym.(func() *sdk.Plugin)
	if !ok {
		return nil, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "cannot load provider func",
			Detail: fmt.Sprintf(
				"failed loading the provider func for %s:\n%s",
				descriptor.Name, err,
			),
		}
	}

	return makePlugin(), nil
}

// TODO pull the build cache path making code out of here
func loadPluginsLookup(basePath string, descriptors *Plugins) (map[string]*sdk.Plugin, hcl.Diagnostics) {
	cachePathRoot := path.Join(os.TempDir(), "psyduck-build")
	err := os.MkdirAll(cachePathRoot, os.ModePerm)
	if err != nil {
		return nil, []*hcl.Diagnostic{{
			Severity: hcl.DiagError,
			Summary:  "failed to create build cache root",
			Detail: fmt.Sprintf(
				"failed to create build cache root at %s:\n%s",
				cachePathRoot, err,
			),
		}}
	}

	cachePath, err := os.MkdirTemp(cachePathRoot, "*")
	if err != nil {
		return nil, []*hcl.Diagnostic{{
			Severity: hcl.DiagError,
			Summary:  "failed to create build cache dir",
			Detail: fmt.Sprintf(
				"failed to create build cache dir at %s:\n%s",
				cachePathRoot, err,
			),
		}}
	}

	plugins := make(map[string]*sdk.Plugin, len(descriptors.Blocks))
	diags := make(hcl.Diagnostics, len(descriptors.Blocks)+1)
	diagIndex := 0
	for _, descriptor := range descriptors.Blocks {
		if plugin, diag := loadPlugin(&build.Default, cachePath, basePath, descriptor); diag != nil {
			diags[diagIndex] = diag
			diagIndex++
		} else {
			plugins[plugin.Name] = plugin
		}
	}

	if err := os.RemoveAll(cachePath); err != nil {
		diags[diagIndex] = &hcl.Diagnostic{
			Severity: hcl.DiagWarning,
			Summary:  "failed to cleanup build cache dir",
			Detail: fmt.Sprintf(
				"failed to cleanup build cache dir at %s:\n%s",
				cachePath, err,
			),
		}

		diagIndex++
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

func LoadPluginsLookup(basePath, filename string, literal []byte, context *hcl.EvalContext) (map[string]*sdk.Plugin, hcl.Diagnostics) {
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
		return loadPluginsLookup(basePathAbs, descriptors)
	}
}
