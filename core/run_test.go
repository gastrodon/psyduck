package core

import (
	"testing"
	"time"

	"github.com/gastrodon/psyduck/configure"
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
	producers := make([]sdk.Producer, config.ProducerCount)
	counts := make([]int, config.ProducerCount)

	for slot := 0; slot < config.ProducerCount; slot++ {
		producers[slot] = func(i int) sdk.Producer {

			// each producer owns its own part of counts and its own termination counter
			return func() sdk.Producefunc {
				count := 0
				return func() ([]byte, bool, error) {
					counts[i]++
					count++
					return []byte{byte(i & 0xff)}, count >= config.DataCount, nil
				}
			}
		}(slot)
	}

	report := func() []int { return counts }
	if config.ProducerCount == 1 {
		return producers[0], report
	}

	return joinProducers(producers, make([]*configure.Resource, len(producers))), report
}

func makeTestConsumer(config runConfig, test *testing.T) (sdk.Consumer, func() []int) {
	consumers := make([]sdk.Consumer, config.ConsumerCount)
	counts := make([]int, config.ConsumerCount)

	for slot := 0; slot < config.ConsumerCount; slot++ {
		consumers[slot] = func(i int) sdk.Consumer {
			return func() sdk.Consumefunc {
				return func([]byte) error {
					counts[i]++
					return nil
				}
			}
		}(slot)

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
