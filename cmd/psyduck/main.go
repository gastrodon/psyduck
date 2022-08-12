package main

import (
	"errors"
	"flag"
	"fmt"

	std "github.com/gastrodon/psyduck-std"
	"github.com/gastrodon/psyduck/configure"
	"github.com/gastrodon/psyduck/core"
)

func done() (func(), chan bool) {
	chanDone := make(chan bool)

	return func() {
		chanDone <- true
	}, chanDone
}

func do(pipeline *core.Pipeline) error {
	signal := make(chan string)
	doneProduce, doneProduceChannel := done()
	doneConsume, doneConsumeChannel := done()

	chanProduce, chanProduceError := pipeline.Producer(signal, doneProduce)
	chanConsume, chanConsumeError := pipeline.Consumer(signal, doneConsume)

	for {
		select {
		case <-doneProduceChannel:
			return errors.New("the producer is done (good ending)")
		case <-doneConsumeChannel:
			return errors.New("the consumer is done (bad ending)")
		case err := <-chanProduceError:
			return err
		case err := <-chanConsumeError:
			return err
		case data := <-chanProduce:
			if transformed, err := pipeline.StackedTransformer(data); err != nil {
				return err
			} else {
				chanConsume <- transformed
			}
		}
	}
}

func library() *core.Library {
	made := core.NewLibrary()
	made.Load(std.IFunny())
	made.Load(std.Psyduck())
	made.Load(std.Scyther())

	return made
}

func main() {
	directory := flag.String("where", ".", "Directory with psyduck configs")
	pipelineName := flag.String("pipeline", "", "Pipelines to run")
	flag.Parse()

	if *pipelineName == "" {
		panic(errors.New("flag pipeline requires a value"))
	}

	if descriptors, context, err := configure.Directory(*directory); err != nil {
		panic(err)
	} else if descriptor, ok := descriptors[*pipelineName]; !ok {
		panic(fmt.Errorf("can't find pipeline %s", *pipelineName))
	} else {
		if pipeline, err := core.BuildPipeline(descriptor, context, library()); err != nil {
			panic(err)
		} else {
			if err := do(pipeline); err != nil {
				panic(err)
			}
		}
	}
}
