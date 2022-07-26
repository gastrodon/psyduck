package core

func RunPipeline(pipeline *Pipeline, signal chan string) error {
	chanProducer, chanProducerError := pipeline.Producer(signal)
	chanConsumer, chanConsumerError := pipeline.Consumer(signal)

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
