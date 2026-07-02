package main

import (
	"fmt"
	"os"
	"path"

	"github.com/psyduck-etl/sdk"
	"github.com/urfave/cli/v2"

	"github.com/gastrodon/psyduck/configure"
	"github.com/gastrodon/psyduck/configure/hcl"
	"github.com/gastrodon/psyduck/core"
	"github.com/gastrodon/psyduck/plugins"
	"github.com/gastrodon/psyduck/stdlib"
)

func cmdinit(ctx *cli.Context) error { // init is a different thing in go
	sources, err := configure.ReadFiles(ctx.String("chdir"))
	if err != nil {
		return err
	}

	specs, err := hcl.NewHCL().Plugins(sources)
	if err != nil {
		return err
	}

	initPath := path.Join(ctx.String("chdir"), ".psyduck")
	if err := os.MkdirAll(initPath, os.ModeDir|os.ModePerm); err != nil {
		return err
	}

	return plugins.Fetch(initPath, specs)
}

func run(ctx *cli.Context) error {
	if !ctx.Args().Present() {
		return fmt.Errorf("target required")
	}

	sources, err := configure.ReadFiles(ctx.String("chdir"))
	if err != nil {
		return err
	}

	initPath := path.Join(ctx.String("chdir"), ".psyduck")
	loaded, err := plugins.NewGoPluginLoader(initPath).LoadAll()
	if err != nil {
		return err
	}
	loaded = append(loaded, stdlib.Plugin())

	format := hcl.NewHCL()

	result, err := format.Parse(sources, loaded)
	if err != nil {
		return err
	}

	target := ctx.Args().First()
	pipe, err := result.Pipeline(target)
	if err != nil {
		return err
	}

	pluginIx := make(map[string]sdk.Plugin, len(loaded))
	for _, p := range loaded {
		pluginIx[p.Name()] = p
	}

	pipeline, err := core.BuildPipeline(pipe, pluginIx)
	if err != nil {
		return err
	}

	return core.RunPipeline(pipeline)
}

func main() {
	app := cli.App{
		Name:  "psyduck",
		Usage: "run and manage etl pipelines",
		Flags: []cli.Flag{
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
