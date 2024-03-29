package core

import "fmt"

func RunPipeline(pipeline *Pipeline) error {
	dataProducer, errorProducer := make(chan []byte), make(chan error)
	dataConsumer, errorConsumer, finishConsumer := make(chan []byte), make(chan error), make(chan struct{})
	go pipeline.Producer(dataProducer, errorProducer)
	go pipeline.Consumer(dataConsumer, errorConsumer, finishConsumer)

	for {
		select {
		case msg := <-dataProducer:
			if msg == nil {
				fmt.Println("RunPipeline: finishProducer was closed, closing dataConsumer")
				close(dataConsumer)
				<-finishConsumer
				return nil
			}

			transformed, err := pipeline.Transformer(msg)
			if err != nil {
				return fmt.Errorf("error generated by transformer: %s", err)
			}

			dataConsumer <- transformed
		case err := <-errorProducer:
			if err == nil {
				fmt.Println("RunPipeline: errorProducer was closed")
				continue
			}
			return fmt.Errorf("error generated by producer: %s", err)
		case err := <-errorConsumer:
			if err == nil {
				fmt.Println("RunPipeline: errorConsumer was closed")
				continue
			}
			return fmt.Errorf("error generated by consumer: %s", err)
		}
	}
}
