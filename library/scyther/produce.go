package scyther

import (
	"github.com/gastrodon/psyduck/model"
)

func ProduceQueue(configRaw interface{}) model.Mover {
	config := configRaw.(QueueConfig)

	return func(signal chan string) chan interface{} {
		data := make(chan interface{}, 32)

		go func() {
			for {
				data <- getQueueHead(config)
			}
		}()

		return data
	}
}
