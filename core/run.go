package core

import (
	"github.com/gastrodon/psyduck/model"
)

func RunPipeline(producer, consumer model.Mover, transformers []model.Transformer) {
	signal := make(chan string)

	produceChannel := producer(signal)
	consumeChannel := consumer(signal)

	index := 0
	for data := range produceChannel {
		for _, transform := range transformers {
			data = transform(data)
		}

		consumeChannel <- data
		index++
	}
}
