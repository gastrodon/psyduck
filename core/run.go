package core

import (
	"fmt"

	"github.com/psyduck-etl/sdk"
	"github.com/sirupsen/logrus"
)

func nextProducerGroup(spawn <-chan result[sdk.Producer], count uint, logger *logrus.Logger) (sdk.Producer, error) {
	group := make([]sdk.Producer, count)
	if count == 0 {
		for next := range spawn {
			if next.err != nil {
				return nil, next.err
			}

			group = append(group, next.v)
		}
	} else {
		for i := uint(0); i < count; i++ {
			next := <-spawn
			if next.err != nil {
				return nil, next.err
			} else if next.v == nil {
				return joinProducers(group[:i], logger), nil
			}
			group[i] = next.v
		}
	}

	return joinProducers(group, logger), nil
}

func RunPipeline(pipeline *Pipeline) error {
	spawnProducer := pipeline.Producer()

	// consumer stuff, called once per runtime
	dataConsumer, errorConsumer, finishConsumer := make(chan []byte), make(chan error), make(chan struct{})
	go pipeline.Consumer(dataConsumer, errorConsumer, finishConsumer)

	// producer stuff, called in a loop per runtime
	for {
		group, err := nextProducerGroup(spawnProducer, pipeline.ParallelProducers, pipeline.logger)
		if err != nil {
			// This doesn't respect exit-on-error because
			// an error getting the producers halts the pipeline regardless
			return fmt.Errorf("failed collecting producer group: %s", err)
		} else if group == nil {
			close(dataConsumer)
			<-finishConsumer
			return nil
		}

		dataProducer, errorProducer := make(chan []byte), make(chan error)
		errs := make(chan error)

		go group(dataProducer, errorProducer)

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
			close(errs)
		}()

		for err := range errs {
			pipeline.logger.Error(err)
			if pipeline.ExitOnError {
				return err
			}
		}
	}
}
