package core

func RunPipeline(pipeline *Pipeline) error {
	dataProducer, errorProducer := pipeline.Producer()
	dataConsumer, errorConsumer := pipeline.Consumer()

	for {
		select {
		case data := <-dataProducer:
			if data == nil {
				return nil
			}

			transformed, err := pipeline.StackedTransformer(data)
			if err != nil {
				return err
			}

			dataConsumer <- transformed
		case err := <-errorProducer:
			if err == nil {
				continue
			}

			return err
		case err := <-errorConsumer:
			if err == nil {
				continue
			}

			return err
		}
	}
}
