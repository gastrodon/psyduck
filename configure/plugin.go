package configure

import (
	"fmt"
	"path/filepath"
	"plugin"

	"github.com/gastrodon/psyduck/sdk"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
)

func loadPlugin(basePath string, descriptor pluginSource) (*sdk.Plugin, *hcl.Diagnostic) {
	switch {
	case descriptor.Source != "":
		pluginPath := descriptor.Source
		if !filepath.IsAbs(pluginPath) {
			pluginPath = filepath.Join(basePath, pluginPath)
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
	default:
		return nil, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "no plugin loader found",
			Detail: fmt.Sprintf(
				"unable to find a suitable way to load the plugin %s with the descriptor\n%#v",
				descriptor.Name, descriptor,
			),
		}
	}
}

func loadPluginsLookup(basePath string, descriptors *Plugins) (map[string]*sdk.Plugin, hcl.Diagnostics) {
	plugins := make(map[string]*sdk.Plugin, len(descriptors.Blocks))
	diags := make(hcl.Diagnostics, len(descriptors.Blocks))
	diagIndex := 0
	for _, descriptor := range descriptors.Blocks {
		if plugin, diag := loadPlugin(basePath, descriptor); diag != nil {
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

func LoadPluginsLookup(basePath, filename string, literal []byte, context *hcl.EvalContext) (map[string]*sdk.Plugin, hcl.Diagnostics) {
	if descriptors, diags := readPluginBlocks(filename, literal, context); diags.HasErrors() {
		return nil, diags
	} else {
		return loadPluginsLookup(basePath, descriptors)
	}
}
