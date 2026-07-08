package core

import "fmt"

func RunPipeline(pipeline *Pipeline) error {
	dataProducer, errorProducer := make(chan []byte), make(chan error)
	dataConsumer, errorConsumer, finishConsumer := make(chan []byte), make(chan error), make(chan struct{})
	errs := make(chan error)

	go pipeline.Producer(dataProducer, errorProducer)
	go pipeline.Consumer(dataConsumer, errorConsumer, finishConsumer)

	go func() {
		for err := range errorProducer {
			if err != nil {
				errs <- fmt.Errorf("producer supplied error: %s", err)
			}
		}
	}()

	go func() {
		for err := range errorConsumer {
			if err != nil {
				errs <- fmt.Errorf("consumer supplied error: %s", err)
			}
		}
	}()

	dataDone := make(chan struct{})
	go func() {
		defer close(dataDone)
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
		close(errs)
	}()

	var retErr error
	for err := range errs {
		pipeline.logger.Error(err)
		if pipeline.ExitOnError {
			retErr = err
			break
		}
	}

	if retErr != nil {
		// The data goroutine (still transforming/consuming) and the
		// error-forwarding goroutines above may still be running and
		// blocked sending on errs. Drain it in the background so those
		// sends unblock and the goroutines can exit instead of leaking.
		go func() {
			for range errs {
			}
		}()
		return retErr
	}

	<-dataDone
	return nil
}
