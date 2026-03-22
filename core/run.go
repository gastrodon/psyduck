package core

import (
	"fmt"
	"sync"
)

func RunPipeline(pipeline *Pipeline) error {
	dataProducer, errorProducer := make(chan []byte), make(chan error)
	dataConsumer, errorConsumer, finishConsumer := make(chan []byte), make(chan error), make(chan struct{})
	errs := make(chan error)

	go pipeline.Producer(dataProducer, errorProducer)
	go pipeline.Consumer(dataConsumer, errorConsumer, finishConsumer)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for err := range errorProducer {
			if err != nil {
				errs <- fmt.Errorf("producer supplied error: %s", err)
			}
		}
	}()

	go func() {
		defer wg.Done()
		for err := range errorConsumer {
			if err != nil {
				errs <- fmt.Errorf("consumer supplied error: %s", err)
			}
		}
	}()

	go func() {
		for msg := range dataProducer {
			transformed, err := pipeline.Transformer(msg)
			if err != nil {
				errs <- fmt.Errorf("transformer supplied error: %s", err)
			}

			if transformed == nil {
				continue
			}

			dataConsumer <- transformed
		}

		close(dataConsumer)
		<-finishConsumer
		wg.Wait()
		close(errs)
	}()

	for err := range errs {
		pipeline.logger.Error(err)
		if pipeline.ExitOnError {
			return err
		}
	}

	return nil
}
