package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/gastrodon/psyduck/core"
	"github.com/gastrodon/psyduck/parse"
	"github.com/hashicorp/hcl/v2"
	"github.com/urfave/cli/v2"
)

func readFiles(ctx *cli.Context) (map[string][]byte, error) {
	chdir := ctx.String("chdir")
	if chdir == "" {
		chdir = "."
	}

	dirEnts, err := os.ReadDir(chdir)
	if err != nil {
		return nil, fmt.Errorf("failed to read chdir entries: %s", err)
	}

	filepaths := make([]string, len(dirEnts))
	i := 0
	for _, ent := range dirEnts {
		if !ent.IsDir() && strings.HasSuffix(ent.Name(), ".psy") {
			filepaths[i] = ent.Name()
			i++
		}
	}

	if i == 0 {
		return nil, fmt.Errorf("no psyduck files in %s", chdir)
	}

	filepaths = filepaths[:i]

	read := make(map[string][]byte, len(filepaths))
	for _, p := range filepaths {
		fp := path.Join(chdir, p)
		content, err := os.ReadFile(fp)
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s: %s", fp, err)
		}

		read[path.Base(fp)] = content
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
				Detail:   "failed to fetch the plugins of literal group member at " + filename + ": " + err.Error(),
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
	files, err := readFiles(ctx)
	if err != nil {
		return fmt.Errorf("failed to read psyduck files: %s", err)
	}

	initPath := path.Join(ctx.String("chdir"), ".psyduck")
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
	files, err := readFiles(ctx)
	if err != nil {
		return fmt.Errorf("failed to read psyduck files: %s", err)
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

	groups := ctx.StringSlice("group")
	if !ctx.Bool("no-root") {
		groups = append(groups, "")
	}

	pipeline, err := core.BuildPipeline(descriptor.Filter(groups).Monify(), library)
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
					&cli.BoolFlag{
						Name:  "no-root",
						Usage: "exclude root level movers",
					},
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
