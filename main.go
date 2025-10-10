package main

import (
	"fmt"
	"os"
	"path"

	"github.com/gastrodon/psyduck/configure"
	"github.com/gastrodon/psyduck/core"
	"github.com/psyduck-etl/sdk"
	"github.com/urfave/cli/v2"
)

var NAME = "psyduck"
var SUBCOMMANDS = [...]string{
	"init",
	"run",
}

func run(ctx *cli.Context) error {
	if !ctx.Args().Present() {
		return fmt.Errorf("target required")
	}

	cfg, err := configure.ParseDir(ctx.String("chdir"))
	if err != nil {
		return err
	}

	target := ctx.Args().First()
	var descriptor *configure.PipelineYAML
	for _, pipeline := range cfg.Pipelines {
		if pipeline.Name == target {
			descriptor = &pipeline
			break
		}
	}

	if descriptor == nil {
		return fmt.Errorf("can't find target %s", target)
	}

	plugins := make([]*sdk.Plugin, len(cfg.Plugins))
	for i, p := range cfg.Plugins {
		plugins[i], err = p.Load()
		if err != nil {
			return fmt.Errorf("failed loading plugin %s: %s", p.Name, err)
		}
	}

	pipeline, err := core.BuildPipeline(descriptor, core.NewLibrary(plugins))
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
				Args:      true,
				ArgsUsage: "pipeline name",
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
