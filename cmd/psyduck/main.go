package main

import (
	"flag"

	std "github.com/gastrodon/psyduck-std"
	"github.com/gastrodon/psyduck/config"
	"github.com/gastrodon/psyduck/core"
)

func main() {
	file := flag.String("file", "psyduck.yml", "File to interpret")
	pipeline := flag.String("pipeline", "", "Pipelines to run")
	flag.Parse()

	if *pipeline == "" {
		panic("a value for -pipeline wasn't supplied")
	}

	pipelinesRaw, err := config.LoadFile(*file)
	if err != nil {
		panic(err)
	}

	library := core.NewLibrary()
	library.Load(std.IFunny())
	library.Load(std.Psyduck())
	library.Load(std.Scyther())

	pipelines := make(core.Pipelines, len(pipelinesRaw))
	for name, pipelineRaw := range pipelinesRaw {
		pipeline, err := core.BuildPipeline(pipelineRaw, library)
		if err != nil {
			panic(err)
		}

		pipelines[name] = pipeline
	}

	signal := make(chan string)
	if err := core.RunPipeline(pipelines[*pipeline], signal); err != nil {
		panic(err)
	}
}
