package core

func RunPipeline(pipeline *Pipeline, signal chan string) error {
	chanProducer, err := pipeline.Producer(signal)
	if err != nil {
		return err
	}

	chanConsumer, err := pipeline.Consumer(signal)
	if err != nil {
		return err
	}

	for data := range chanProducer {
		transformed, err := pipeline.StackedTransformer(data)
		if err != nil {
			return err
		}

		chanConsumer <- transformed
	}

	return nil
}
