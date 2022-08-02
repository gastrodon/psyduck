package main

import (
	"flag"
	"fmt"

	std "github.com/gastrodon/psyduck-std"
	"github.com/gastrodon/psyduck/config"
	"github.com/gastrodon/psyduck/core"
)

func library() *core.Library {
	made := core.NewLibrary()
	made.Load(std.IFunny())
	made.Load(std.Psyduck())
	made.Load(std.Scyther())

	return made
}

func main() {
	directory := flag.String("where", ".", "Directory with psyduck configs")
	pipelineTarget := flag.String("pipeline", "", "Pipelines to run")
	flag.Parse()

	pipelines, context, err := config.LoadDirectory(*directory)
	if err != nil {
		panic(err)
	}

	pipelineConfig, ok := pipelines.Pipelines[*pipelineTarget]
	if !ok {
		panic(fmt.Errorf("no such pipeline %s", *pipelineTarget))
	}

	pipelineBuilt, err := core.BuildPipeline(pipelineConfig, library())
	if err != nil {
		panic(err)
	}

	signal := make(chan string)
	if err := core.RunPipeline(pipelineBuilt, signal); err != nil {
		panic(err)
	}
}
