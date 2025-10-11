package core

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/psyduck-etl/sdk"
	"github.com/sirupsen/logrus"
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
		if testcase.Delay && testing.Short() {
			continue
		}

		if err := testPipeline(testcase); err != nil {
			test.Fatalf("run-pipeline[%d]: failed running pipeline: %s, err!", i, err)
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
		test.Fatalf("failed running pipeline: %s, err!", err)
	}

	if received != limit/fac {
		test.Fatalf("failed running pipeline: expected %d, got %d!", limit/fac, received)
	}
}

type testHook struct {
	fn func()
}

func (testHook) Levels() []logrus.Level {
	return []logrus.Level{logrus.ErrorLevel}
}

func (hook testHook) Fire(*logrus.Entry) error {
	hook.fn()
	return nil
}

func pipelineTestLogger(fn func()) *logrus.Logger {
	log := pipelineLogger()
	log.Hooks.Add(testHook{fn})
	return log
}

func Test_RunPipeline_error(test *testing.T) {
	errText := "error made"
	produceErr := func(n int) sdk.Producer {
		return func(send chan<- []byte, errs chan<- error) {
			for i := 0; i < n; i++ {
				send <- []byte{0}
			}

			errs <- fmt.Errorf(errText)
		}
	}

	consumeErr := func(n int) sdk.Consumer {
		return func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
			for i := 0; i < n; i++ {
				<-recv
			}

			errs <- fmt.Errorf(errText)
		}
	}

	transformeErr := func(n int) sdk.Transformer {
		i := 0
		return func(in []byte) ([]byte, error) {
			if i >= n {
				return nil, fmt.Errorf(errText)
			}
			i++
			return in, nil
		}
	}

	testcases := [...]struct {
		pipeline *Pipeline
		want     error
	}{
		{&Pipeline{
			Producer:    produceErr(50),
			Consumer:    consumeErr(math.MaxInt32),
			Transformer: transformeErr(math.MaxInt32),
			ExitOnError: true,
		}, fmt.Errorf("producer supplied error: %s", errText)},
		{&Pipeline{
			Producer:    produceErr(math.MaxInt32),
			Consumer:    consumeErr(50),
			Transformer: transformeErr(math.MaxInt32),
			ExitOnError: true,
		}, fmt.Errorf("consumer supplied error: %s", errText)},
		{&Pipeline{
			Producer:    produceErr(math.MaxInt32),
			Consumer:    consumeErr(math.MaxInt32),
			Transformer: transformeErr(50),
			ExitOnError: true,
		}, fmt.Errorf("transformer supplied error: %s", errText)},
		{&Pipeline{
			Producer:    joinProducers([]sdk.Producer{produceErr(50), produceErr(math.MaxInt32)}, logrus.StandardLogger()),
			Consumer:    consumeErr(math.MaxInt32),
			Transformer: transformeErr(math.MaxInt32),
			ExitOnError: true,
		}, fmt.Errorf("producer supplied error: %s", errText)},
		{&Pipeline{
			Producer:    produceErr(math.MaxInt32),
			Consumer:    joinConsumers([]sdk.Consumer{consumeErr(math.MaxInt32), consumeErr(50)}, logrus.StandardLogger()),
			Transformer: transformeErr(math.MaxInt32),
			ExitOnError: true,
		}, fmt.Errorf("consumer supplied error: %s", errText)},
	}

	for i, testcase := range testcases {
		didFire := false
		testcase.pipeline.logger = pipelineTestLogger(func() {
			didFire = true
		})
		if err := RunPipeline(testcase.pipeline); err == nil {
			test.Fatalf("run-pipeline-error[%d]: failed running pipeline: expected error, got none!", i)
		} else if err.Error() != testcase.want.Error() {
			test.Fatalf("run-pipeline-error[%d]: failed running pipeline: expected error %s, got %s!", i, testcase.want, err)
		} else if !didFire {
			test.Fatalf("run-pipeline-error[%d]: failed running pipeline: expected logger hook to fire, got not fired!", i)
		}
	}
}
