package core

func RunPipeline(pipeline *Pipeline) error {
	dataProducer, errorProducer := pipeline.Producer()
	dataConsumer, errorConsumer, finishConsumer := pipeline.Consumer()
	consumerClosed := false

	for {
		select {
		case done := <-finishConsumer:
			if !done {
				panic("false sent on finish channel")
			}

			return nil
		case data := <-dataProducer:
			if data == nil {
				if !consumerClosed {
					consumerClosed = true
					close(dataConsumer)
				}

				break // TODO use a label to jump out to a block that doesn't consume dataProducer
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
