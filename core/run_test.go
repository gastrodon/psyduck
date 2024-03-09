package core

import (
	"testing"
	"time"

	"github.com/psyduck-std/sdk"
)

type Values func() (int, int)

const (
	COUNT_DELAY     = 10
	COUNT_IMMEDIATE = 10_000
	COUNT_BUFFERED  = 10_000
)

type runConfig struct {
	DataCount     int
	Buffer        int
	Delay         bool
	ProducerCount int
	ConsumerCount int
}

func makeTestProducer(config runConfig, test *testing.T) (sdk.Producer, func() []int) {
	counts := make([]int, config.ProducerCount)
	producers := make([]sdk.Producer, config.ProducerCount)

	for index := 0; index < config.ProducerCount; index++ {
		producers[index] = func(slot int) sdk.Producer {

			return func() (chan []byte, chan error) {
				data := make(chan []byte, config.Buffer)

				go func(slot int) {
					for dataEach := 0; dataEach < config.DataCount; dataEach++ {
						data <- []byte{byte(dataEach)}
						counts[slot]++
					}

					close(data)
				}(slot)

				return data, nil
			}

		}(index)
	}

	report := func() []int { return counts }
	if config.ProducerCount == 1 {
		return producers[0], report
	}

	return joinProducers(producers), report
}

func makeTestConsumer(config runConfig, test *testing.T) (sdk.Consumer, func() []int) {
	counts := make([]int, config.ConsumerCount)
	consumers := make([]sdk.Consumer, config.ConsumerCount)

	for index := 0; index < config.ConsumerCount; index++ {
		consumers[index] = func(slot int) sdk.Consumer {

			return func() (chan []byte, chan error, chan bool) {
				data := make(chan []byte)
				done := make(chan bool)

				go func(slot int) {

					for range data {
						counts[slot]++
					}

					done <- true
				}(slot)

				return data, nil, done
			}

		}(index)

	}

	report := func() []int { return counts }
	if config.ConsumerCount == 1 {
		return consumers[0], report
	}

	return joinConsumers(consumers), report
}

func testPipeline(config runConfig, test *testing.T) {
	producer, reportProducer := makeTestProducer(config, test)
	consumer, reportConsumer := makeTestConsumer(config, test)

	pipeline := &Pipeline{
		Producer: producer,
		Consumer: consumer,
		StackedTransformer: func(data []byte) ([]byte, error) {
			if config.Delay {
				time.Sleep(50 * time.Millisecond)
			}

			return data, nil
		},
	}

	if err := RunPipeline(pipeline); err != nil {
		test.Fatal(err)
	}

	producerCount := reportProducer()
	for index := range producerCount {
		if producerCount[index] != config.DataCount {
			test.Fatalf("produce count mismatch at %d! %d / %d", index, producerCount[index], config.DataCount)
		}

	}

	consumerCount := reportConsumer()
	for index := range consumerCount {
		if consumerCount[index] != config.DataCount*config.ProducerCount {
			test.Fatalf("consume count mismatch at %d! %d / %d * %d", index, consumerCount[index], config.DataCount, config.ProducerCount)
		}
	}
}

func Test_RunPipeline(test *testing.T) {
	cases := []struct {
		DataCount int
		Buffer    int
		Delay     bool
	}{
		{COUNT_DELAY, 0, true},
		{COUNT_IMMEDIATE, 0, false},
		{COUNT_BUFFERED, COUNT_BUFFERED + 1, false},
	}

	for _, testcase := range cases {
		for _, producerCount := range []int{1, 10} {
			for _, consumerCount := range []int{1, 10} {
				testPipeline(runConfig{
					testcase.DataCount,
					testcase.Buffer,
					testcase.Delay,
					producerCount,
					consumerCount,
				}, test)
			}
		}
	}
}
