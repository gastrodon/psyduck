package core

import (
	"fmt"
	"testing"
	"time"

	"github.com/psyduck-etl/sdk"
)

type Values func() (int, int)

type testPipelineCase struct {
	DataCount     int
	Delay         bool
	ProducerCount int
	ConsumerCount int
}

func makeTestProducer(testcase testPipelineCase) (sdk.Producer, func() []int) {
	counts := make([]int, testcase.ProducerCount)
	producers := make([]sdk.Producer, testcase.ProducerCount)

	for index := 0; index < testcase.ProducerCount; index++ {
		producers[index] = func(slot int) sdk.Producer {
			return func(send chan<- []byte, errs chan<- error) {
				go func(slot int) {
					for dataEach := 0; dataEach < testcase.DataCount; dataEach++ {
						send <- []byte{byte(dataEach)}
						counts[slot]++
					}

					close(send)
					close(errs)
				}(slot)
			}

		}(index)
	}

	return joinProducers(producers, pipelineLogger()), func() []int { return counts }
}

func makeTestConsumer(testcase testPipelineCase) (sdk.Consumer, func() []int) {
	counts := make([]int, testcase.ConsumerCount)
	consumers := make([]sdk.Consumer, testcase.ConsumerCount)

	for index := 0; index < testcase.ConsumerCount; index++ {
		consumers[index] = func(slot int) sdk.Consumer {
			return func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
				go func(i int) {
					for range recv {
						counts[i]++
					}

					close(done)
				}(slot)
			}

		}(index)

	}

	return joinConsumers(consumers, pipelineLogger()), func() []int { return counts }
}

func testPipeline(testcase testPipelineCase) error {
	producer, reportProducer := makeTestProducer(testcase)
	consumer, reportConsumer := makeTestConsumer(testcase)
	transformer := func(d []byte) ([]byte, error) { return d, nil }

	if testcase.Delay {
		transformer = func(d []byte) ([]byte, error) {
			time.Sleep(50 * time.Millisecond)
			return d, nil
		}
	}

	pipeline := &Pipeline{
		Producer:    producer,
		Consumer:    consumer,
		Transformer: transformer,
	}

	if err := RunPipeline(pipeline); err != nil {
		return err
	}

	producerCount := reportProducer()
	for index := range producerCount {
		if producerCount[index] != testcase.DataCount {
			return fmt.Errorf("produce count mismatch at %d! %d / %d", index, producerCount[index], testcase.DataCount)
		}

	}

	consumerCount := reportConsumer()
	for index := range consumerCount {
		if consumerCount[index] != testcase.DataCount*testcase.ProducerCount {
			return fmt.Errorf("consume count mismatch at %d! %d / %d * %d", index, consumerCount[index], testcase.DataCount, testcase.ProducerCount)
		}
	}

	return nil
}

func Test_RunPipeline(test *testing.T) {
	const (
		COUNT_DELAY     = 100
		COUNT_IMMEDIATE = 10_000
		COUNT_BUFFERED  = 10_000
	)

	cases := []testPipelineCase{
		{COUNT_DELAY, true, 1, 1},
		{COUNT_DELAY, true, 10, 10},
		{COUNT_IMMEDIATE, false, 1, 1},
		{COUNT_IMMEDIATE, false, 1, 10},
		{COUNT_IMMEDIATE, false, 10, 1},
		{COUNT_IMMEDIATE, false, 10, 10},
	}

	for i, testcase := range cases {
		if err := testPipeline(testcase); err != nil {
			test.Fatalf("case %d failed: %s", i, err)
		}
	}
}

func Test_RunPipeline_filtering(test *testing.T) {
	received, limit, fac := byte(0), byte(100), byte(2)
	testcase := &Pipeline{
		Producer: func(send chan<- []byte, errs chan<- error) {
			for i := byte(0); i < limit; i++ {
				send <- []byte{i}
			}
			close(send)
		},
		Consumer: func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
			for range recv {
				received++
			}

			close(done)
		},
		Transformer: func(in []byte) ([]byte, error) {
			if in[0]%fac != fac-1 {
				return nil, nil

			}
			return in, nil
		},
	}

	if err := RunPipeline(testcase); err != nil {
		test.Fatal(err)
	}

	if received != limit/fac {
		test.Fatalf("recieved %d != %d/%d!", received, limit, fac)
	}
}

func Test_RunPipeline_producerError(test *testing.T) {
	recieved, errText := byte(0), "limit reached"
	testcase := &Pipeline{
		Producer: func(send chan<- []byte, errs chan<- error) {
			for i := byte(0); i < 100; i++ {
				send <- []byte{i}
			}

			errs <- fmt.Errorf(errText)
		},
		Consumer: func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
			for range recv {
				recieved++
			}
			close(done)
		},
		Transformer: func(in []byte) ([]byte, error) { return in, nil },
	}

	errWant := fmt.Errorf("producer supplied error: %s", errText)
	if err := RunPipeline(testcase); err == nil {
		test.Fatal("no error returned")
	} else if err.Error() != errWant.Error() {
		test.Fatalf("other error: %s != %s!", err, errWant)
	}
}

func Test_RunPipeline_consumerError(test *testing.T) {
	recieved, limit, errText := byte(0), byte(50), "limit reached"
	testcase := &Pipeline{
		Producer: func(send chan<- []byte, errs chan<- error) {
			for i := byte(0); i < 100; i++ {
				send <- []byte{i}
			}

		},
		Consumer: func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
			for range recv {
				recieved++

				if recieved == limit {
					errs <- fmt.Errorf(errText)
					return
				}
			}
			close(done)
		},
		Transformer: func(in []byte) ([]byte, error) { return in, nil },
	}

	errWant := fmt.Errorf("consumer supplied error: %s", errText)
	if err := RunPipeline(testcase); err == nil {
		test.Fatal("no error returned")
	} else if err.Error() != errWant.Error() {
		test.Fatalf("other error: %s != %s!", err, errWant)
	}

	if recieved > limit {
		test.Fatalf("recieved too many: %d > %d!", recieved, limit)
	}
}

func Test_RunPipeline_transformerError(test *testing.T) {
	recieved, limit, errText := byte(0), byte(50), "limit reached"
	testcase := &Pipeline{
		Producer: func(send chan<- []byte, errs chan<- error) {
			for i := byte(0); i < 100; i++ {
				send <- []byte{i}
			}

		},
		Consumer: func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
			for range recv {
				recieved++
			}
			close(done)
		},
		Transformer: func(in []byte) ([]byte, error) {
			if recieved == limit {
				return nil, fmt.Errorf(errText)
			}
			return in, nil
		},
	}

	errWant := fmt.Errorf("transformer supplied error: %s", errText)
	if err := RunPipeline(testcase); err == nil {
		test.Fatal("no error returned")
	} else if err.Error() != errWant.Error() {
		test.Fatalf("other error: %s != %s!", err, errWant)
	}

	if recieved > limit {
		test.Fatalf("recieved too many: %d > %d!", recieved, limit)
	}
}
