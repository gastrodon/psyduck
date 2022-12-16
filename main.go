package main

import (
	"fmt"
	"os"

	"github.com/gastrodon/psyduck/configure"
	"github.com/gastrodon/psyduck/core"
	"github.com/urfave/cli/v2"
)

var NAME = "psyduck"
var SUBCOMMANDS = [...]string{
	"run",
}

func run(ctx *cli.Context) error {
	descriptors, exprContext, err := configure.Directory(ctx.String("chdir"))
	if err != nil {
		return err
	}

	target := ctx.String("target")
	descriptor, ok := descriptors[target]
	if !ok {
		return fmt.Errorf("can't find target %s", target)
	}

	pipeline, err := core.BuildPipeline(descriptor, exprContext, core.NewLibrary())
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
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
