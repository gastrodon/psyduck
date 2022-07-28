package core

import "errors"

func makeDone(done chan bool) func() {
	return func() { done <- true }
}

func RunPipeline(pipeline *Pipeline, signal chan string) error {
	doneProducer := make(chan bool)
	doneConsumer := make(chan bool)

	chanProducer, chanProducerError := pipeline.Producer(signal, makeDone(doneProducer))
	chanConsumer, chanConsumerError := pipeline.Consumer(signal, makeDone(doneConsumer))

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
		case <-doneProducer:
			return nil
		case <-doneConsumer:
			return errors.New("the consumer stopped accepting data")
		}
	}
}
