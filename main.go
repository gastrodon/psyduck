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

// entryPath resolves the pipeline file argument (relative to --chdir) that
// run/init/list/show all take as their first positional argument.
func entryPath(ctx *cli.Context) (string, error) {
	if !ctx.Args().Present() {
		return "", fmt.Errorf("pipeline file required")
	}
	return path.Join(ctx.String("chdir"), ctx.Args().First()), nil
}

func cmdinit(ctx *cli.Context) error { // init is a different thing in go
	entry, err := entryPath(ctx)
	if err != nil {
		return err
	}

	specs, err := hcl.NewParserHCL().Plugins(entry, parse.OSLoader)
	if err != nil {
		return err
	}

	initPath := path.Join(ctx.String("chdir"), ".psyduck")
	if err := os.MkdirAll(initPath, os.ModeDir|os.ModePerm); err != nil {
		return err
	}

	return plugins.NewStore(initPath).Build(specs)
}

// loadPipelines resolves entry and its transitive imports against the
// loaded plugins + stdlib, and returns every pipeline{} declared directly
// in entry (imported pipelines are data for entry to reuse, not part of
// what runs).
func loadPipelines(ctx *cli.Context) (map[string]parse.Pipeline, []sdk.Plugin, error) {
	entry, err := entryPath(ctx)
	if err != nil {
		return nil, nil, err
	}

	initPath := path.Join(ctx.String("chdir"), ".psyduck")
	loaded, err := plugins.NewStore(initPath).Load()
	if err != nil {
		return nil, nil, err
	}
	loaded = append(loaded, stdlib.Plugin())

	pipelines, err := hcl.NewParserHCL().Parse(entry, parse.OSLoader, loaded)
	if err != nil {
		return nil, nil, err
	}
	return pipelines, loaded, nil
}

// run builds every pipeline{} declared directly in the target file and
// runs them. Zero pipelines is an error. One runs directly. More than one
// run concurrently, one goroutine each; run returns the first error seen
// (if any) once all of them have finished.
func run(ctx *cli.Context) error {
	pipelines, loaded, err := loadPipelines(ctx)
	if err != nil {
		return err
	}

	if len(pipelines) == 0 {
		return fmt.Errorf("%s: declares no pipeline", ctx.Args().First())
	}

	built := make([]*core.Pipeline, 0, len(pipelines))
	for _, pipe := range pipelines {
		b, err := core.BuildPipeline(pipe, loaded)
		if err != nil {
			return err
		}
		built = append(built, b)
	}

	if len(built) == 1 {
		return core.RunPipeline(built[0])
	}

	errs := make(chan error, len(built))
	for _, b := range built {
		go func(b *core.Pipeline) { errs <- core.RunPipeline(b) }(b)
	}

	var failed error
	for range built {
		if err := <-errs; err != nil && failed == nil {
			failed = err
		}
	}
	return failed
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

	names := ctx.Args().Slice()[1:] // args[0] is the entry file
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
				Usage:     "run every pipeline declared in a file",
				Action:    run,
				Args:      true,
				ArgsUsage: "pipeline file",
			},
			{
				Name:      "list",
				Usage:     "list pipelines declared in a file",
				Action:    list,
				Args:      true,
				ArgsUsage: "pipeline file",
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
				ArgsUsage: "pipeline file [pipeline name ...]",
			},
			{
				Name:      "init",
				Usage:     "init a pipeline workspace for a file",
				Action:    cmdinit,
				Args:      true,
				ArgsUsage: "pipeline file",
				Flags:     []cli.Flag{},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
