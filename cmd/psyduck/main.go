package main

import (
	"flag"
	"fmt"

	std "github.com/gastrodon/psyduck-std"
	"github.com/gastrodon/psyduck/config"
	"github.com/gastrodon/psyduck/core"
)

func main() {
	file := flag.String("file", "psyduck.yml", "File to interpret")
	flag.Parse()

	fmt.Println(*file)

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
		pipelines[name] = core.BuildPipeline(pipelineRaw, library)
	}

	signal := make(chan string)
	core.RunPipeline(pipelines["test"], signal)

	select {
	case <-make(chan bool):
		break
	}
}
