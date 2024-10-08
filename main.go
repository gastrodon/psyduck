package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path"

	"github.com/gastrodon/psyduck/core"
	"github.com/gastrodon/psyduck/parse"
	"github.com/hashicorp/hcl/v2"
	"github.com/urfave/cli/v2"
)

func readFiles(paths ...string) (map[string][]byte, error) {
	read := make(map[string][]byte, len(paths))
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}

		read[path] = content
	}

	return read, nil
}

var NAME = "psyduck"
var SUBCOMMANDS = [...]string{
	"init",
	"run",
}

func fetchPluginsGroup(initPath string, files map[string][]byte) (map[string]string, hcl.Diagnostics) {
	composed, diags := make(map[string]string), make(hcl.Diagnostics, 0)
	for filename, literal := range files {
		frag, err := parse.FetchPlugins(initPath, filename, literal)
		if err != nil {
			return nil, diags.Append(&hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "failed to fetch plugins of group member",
				Detail:   "failed to fetch the plugins of literal group member at " + filename,
			})
		}

		for k, v := range frag {
			if _, ok := composed[k]; ok {
				diags = diags.Append(&hcl.Diagnostic{
					Severity: hcl.DiagWarning,
					Summary:  "duplicate plugin",
					Detail:   "duplicate plugin defined as " + k + " in " + filename,
				})
			}

			composed[k] = v
		}
	}

	return composed, diags
}

func cmdinit(ctx *cli.Context) error { // init is a different thing in go
	initPath := path.Join(ctx.String("chdir"), ".psyduck")
	dirEnts, err := os.ReadDir(ctx.String("chdir"))
	if err != nil {
		return fmt.Errorf("failed to read chdir entries: %s", err)
	}

	filenames := make([]string, len(dirEnts))
	for i, ent := range dirEnts {
		filenames[i] = ent.Name()
	}

	files, err := readFiles(filenames...)
	if err != nil {
		return fmt.Errorf("failed to read files in chdir: %s", err)
	}

	pluginPaths, diags := fetchPluginsGroup(initPath, files)
	if diags.HasErrors() {
		return diags
	}

	b, err := json.Marshal(pluginPaths)
	if err != nil {
		return err
	}

	return os.WriteFile(path.Join(initPath, "plugin.json"), b, 0o644)
}

func run(ctx *cli.Context) error {
	files, err := readFiles(ctx.Args().Slice()...)
	if err != nil {
		return fmt.Errorf("failed to read files: %s", err)
	}

	initPath := path.Join(ctx.String("chdir"), ".psyduck")
	plugins, err := parse.LoadPlugins(initPath, files)
	if err != nil {
		return err
	}

	library := core.NewLibrary(plugins)
	descriptor, diags := parse.NewFileGroup(files).Pipelines(library.Ctx())
	if diags.HasErrors() {
		return diags
	}

	pipeline, err := core.BuildPipeline(descriptor.Filter(ctx.StringSlice("group")).Monify(), library)
	if err != nil {
		return err
	}

	return core.RunPipeline(pipeline)
}

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		panic(fmt.Sprintf("failed getting $HOME: %s", err))
	}

	app := cli.App{
		Name:  "psyduck",
		Usage: "run and manage etl pipelines",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:      "plugin",
				Usage:     "directory to load plugins from",
				Value:     path.Join(home, ".psyduck.d/plugin"),
				TakesFile: true,
			},
			&cli.StringFlag{
				Name:      "chdir",
				Usage:     "directory to execute from",
				Value:     ".",
				TakesFile: true,
			},
		},
		Commands: []*cli.Command{
			{
				Name:      "run",
				Usage:     "run a pipeline job",
				Action:    run,
				ArgsUsage: "pipeline name",
				Flags: []cli.Flag{
					&cli.StringSliceFlag{
						Name:    "group",
						Usage:   "groups of movers to include",
						Aliases: []string{"g"},
					},
				},
			},
			{
				Name:   "init",
				Usage:  "init a pipeline workspace",
				Action: cmdinit,
				Flags:  []cli.Flag{},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
