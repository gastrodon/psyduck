package core

import (
	"github.com/gastrodon/psyduck/model"
)

func RunPipeline(pipeline model.Pipeline) {
	signal := make(chan string)
	chanProducer := pipeline.Producer(signal)
	chanConsumer := pipeline.Consumer(signal)

	for data := range chanProducer {
		chanConsumer <- pipeline.StackedTransformer(data)
	}
}
