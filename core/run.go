package core

import "fmt"

func makeDone(message string) func() {
	return func() {
		fmt.Println(message)
	}
}

func RunPipeline(pipeline *Pipeline, signal chan string) error {
	chanProducer, chanProducerError := pipeline.Producer(signal, makeDone("the producer is done!"))
	chanConsumer, chanConsumerError := pipeline.Consumer(signal, makeDone("the consumer is done!"))

	for {
		select {
		case data := <-chanProducer:
			transformed, err := pipeline.StackedTransformer(data)
			if err != nil {
				return err
			}

			chanConsumer <- transformed
		case err := <-chanProducerError:
			return err
		case err := <-chanConsumerError:
			return err
		}
	}
}
