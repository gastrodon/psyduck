package core

func RunPipeline(pipeline *Pipeline) error {
	dataProducer, errorProducer := make(chan []byte), make(chan error)
	dataConsumer, errorConsumer, finishConsumer := make(chan []byte), make(chan error), make(chan struct{})
	go pipeline.Producer(dataProducer, errorProducer)
	go pipeline.Consumer(dataConsumer, errorConsumer, finishConsumer)

	for {
		select {
		case msg := <-dataProducer:
			if msg == nil {
				close(dataConsumer)
				<-finishConsumer
				return nil
			}

			transformed, err := pipeline.StackedTransformer(msg)
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
