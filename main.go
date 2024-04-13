package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path"

	"github.com/gastrodon/psyduck/configure"
	"github.com/gastrodon/psyduck/core"
	"github.com/urfave/cli/v2"
)

var NAME = "psyduck"
var SUBCOMMANDS = [...]string{
	"init",
	"run",
}

func cmdinit(ctx *cli.Context) error { // init is a different thing in go
	literal, err := configure.ReadDirectory(ctx.String("chdir"))
	if err != nil {
		return err
	}

	filename := path.Base(ctx.String("chdir"))
	_, evalCtx, err := configure.Literal(filename, literal)
	if err != nil {
		return err
	}

	initPath := path.Join(ctx.String("chdir"), ".psyduck")
	err = os.MkdirAll(initPath, os.ModeDir|os.ModePerm)
	if err != nil {
		return err
	}

	pluginPaths, err := configure.CollectPlugins(initPath, filename, literal, evalCtx)
	if err != nil {
		return err
	}

	b, err := json.Marshal(pluginPaths)
	if err != nil {
		return err
	}

	return os.WriteFile(path.Join(initPath, "plugin.json"), b, 0o644)
}

func run(ctx *cli.Context) error {
	literal, err := configure.ReadDirectory(ctx.String("chdir"))
	if err != nil {
		return err
	}

	filename := path.Base(ctx.String("chdir"))
	descriptors, evalCtx, err := configure.Literal(filename, literal)
	if err != nil {
		return err
	}

	initPath := path.Join(ctx.String("chdir"), ".psyduck")
	plugins, err := configure.LoadPlugins(initPath, filename, literal, evalCtx)
	if err != nil {
		return err
	}

	target := ctx.String("target")
	descriptor, ok := descriptors[target]
	if !ok {
		return fmt.Errorf("can't find target %s", target)
	}

	pipeline, err := core.BuildPipeline(descriptor, evalCtx, core.NewLibrary(plugins))
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
				Name:   "run",
				Usage:  "run a pipeline job",
				Action: run,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "target",
						Usage:    "pipeline that we want to run",
						Required: true,
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
