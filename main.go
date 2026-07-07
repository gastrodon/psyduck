package main

import (
	"fmt"
	"os"
	"path"
	"sort"

	"github.com/psyduck-etl/sdk"
	"github.com/urfave/cli/v2"

	"github.com/gastrodon/psyduck/core"
	"github.com/gastrodon/psyduck/parse"
	"github.com/gastrodon/psyduck/parse/hcl"
	"github.com/gastrodon/psyduck/plugins"
	"github.com/gastrodon/psyduck/stdlib"
)

func cmdinit(ctx *cli.Context) error { // init is a different thing in go
	sources, err := parse.SourceFromDir(ctx.String("chdir"))
	if err != nil {
		return err
	}

	specs, err := hcl.NewParserHCL().Plugins(sources)
	if err != nil {
		return err
	}

	initPath := path.Join(ctx.String("chdir"), ".psyduck")
	if err := os.MkdirAll(initPath, os.ModeDir|os.ModePerm); err != nil {
		return err
	}

	return plugins.NewStore(initPath).Build(specs)
}

// loadPipelines parses every .psy source in the workspace against the
// loaded plugins + stdlib.
func loadPipelines(ctx *cli.Context) (map[string]parse.Pipeline, []sdk.Plugin, error) {
	sources, err := parse.SourceFromDir(ctx.String("chdir"))
	if err != nil {
		return nil, nil, err
	}

	initPath := path.Join(ctx.String("chdir"), ".psyduck")
	loaded, err := plugins.NewStore(initPath).Load()
	if err != nil {
		return nil, nil, err
	}
	loaded = append(loaded, stdlib.Plugin())

	pipelines, err := hcl.NewParserHCL().Parse(sources, loaded)
	if err != nil {
		return nil, nil, err
	}
	return pipelines, loaded, nil
}

func run(ctx *cli.Context) error {
	if !ctx.Args().Present() {
		return fmt.Errorf("target required")
	}

	pipelines, loaded, err := loadPipelines(ctx)
	if err != nil {
		return err
	}

	target := ctx.Args().First()
	pipe, ok := pipelines[target]
	if !ok {
		return fmt.Errorf("no pipeline %q", target)
	}

	pipeline, err := core.BuildPipeline(pipe, loaded)
	if err != nil {
		return err
	}

	return core.RunPipeline(pipeline)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedNames(pipelines map[string]parse.Pipeline) []string {
	return sortedKeys(pipelines)
}

func list(ctx *cli.Context) error {
	pipelines, _, err := loadPipelines(ctx)
	if err != nil {
		return err
	}

	for _, name := range sortedNames(pipelines) {
		if !ctx.Bool("stat") {
			fmt.Println(name)
			continue
		}

		spec := pipelines[name].Spec
		remote := " "
		if spec.RemoteSeed != nil {
			remote = "r"
		}
		fmt.Printf("%s %s r%d x%d c%d\n", name, remote,
			len(spec.Producers), len(spec.Transformers), len(spec.Consumers))
	}
	return nil
}

// printResource prints one resource and its config, indented under its
// pipeline block.
func printResource(r parse.Resource) {
	fmt.Printf("  %s\n", r.Ref)
	values, ok := r.Block.(parse.ConfigValues)
	if !ok {
		return
	}
	m := values.Values()
	for _, k := range sortedKeys[string](m) {
		fmt.Printf("    %s = %s\n", k, m[k])
	}
}

func show(ctx *cli.Context) error {
	pipelines, _, err := loadPipelines(ctx)
	if err != nil {
		return err
	}

	names := ctx.Args().Slice()
	if len(names) == 0 {
		names = sortedNames(pipelines)
	}

	for i, name := range names {
		pipe, ok := pipelines[name]
		if !ok {
			return fmt.Errorf("no pipeline %q", name)
		}

		if i > 0 {
			fmt.Println()
		}
		fmt.Println(pipe.Name)

		if pipe.Spec.RemoteSeed != nil {
			printResource(*pipe.Spec.RemoteSeed)
			fmt.Println("  *")
		}
		for _, r := range pipe.Spec.Producers {
			printResource(r)
		}
		fmt.Println("  ->")
		for _, r := range pipe.Spec.Transformers {
			printResource(r)
		}
		fmt.Println("  ->")
		for _, r := range pipe.Spec.Consumers {
			printResource(r)
		}
	}
	return nil
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
				Name:   "list",
				Usage:  "list pipelines by name",
				Action: list,
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "stat",
						Usage: "include resource counts: <name> <r|space> r<producers> x<transformers> c<consumers>",
					},
				},
			},
			{
				Name:      "show",
				Usage:     "show pipeline resources and config",
				Action:    show,
				Args:      true,
				ArgsUsage: "[pipeline name ...]",
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
