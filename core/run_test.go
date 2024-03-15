package core

import (
	"testing"
	"time"

	"github.com/psyduck-std/sdk"
)

type Values func() (int, int)

type testPipelineCase struct {
	DataCount     int
	Buffer        int
	Delay         bool
	ProducerCount int
	ConsumerCount int
}

func makeTestProducer(testcase testPipelineCase) (sdk.Producer, func() []int) {
	counts := make([]int, testcase.ProducerCount)
	producers := make([]sdk.Producer, testcase.ProducerCount)

	for index := 0; index < testcase.ProducerCount; index++ {
		producers[index] = func(slot int) sdk.Producer {

			return func() (chan []byte, chan error) {
				data := make(chan []byte, testcase.Buffer)

				go func(slot int) {
					for dataEach := 0; dataEach < testcase.DataCount; dataEach++ {
						data <- []byte{byte(dataEach)}
						counts[slot]++
					}

					close(data)
				}(slot)

				return data, nil
			}

		}(index)
	}

	return joinProducers(producers), func() []int { return counts }
}

func makeTestConsumer(testcase testPipelineCase) (sdk.Consumer, func() []int) {
	counts := make([]int, testcase.ConsumerCount)
	consumers := make([]sdk.Consumer, testcase.ConsumerCount)

	for index := 0; index < testcase.ConsumerCount; index++ {
		consumers[index] = func(slot int) sdk.Consumer {

			return func() (chan []byte, chan error, chan bool) {
				data := make(chan []byte)
				done := make(chan bool)

				go func(i int) {
					for range data {
						counts[i]++
					}

					done <- true
				}(slot)

				return data, nil, done
			}

		}(index)

	}

	return joinConsumers(consumers), func() []int { return counts }
}

func testPipeline(testcase testPipelineCase, test *testing.T) {
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
		Producer:           producer,
		Consumer:           consumer,
		StackedTransformer: transformer,
	}

	if err := RunPipeline(pipeline); err != nil {
		test.Fatal(err)
	}

	producerCount := reportProducer()
	for index := range producerCount {
		if producerCount[index] != testcase.DataCount {
			test.Fatalf("produce count mismatch at %d! %d / %d", index, producerCount[index], testcase.DataCount)
		}

	}

	consumerCount := reportConsumer()
	for index := range consumerCount {
		if consumerCount[index] != testcase.DataCount*testcase.ProducerCount {
			test.Fatalf("consume count mismatch at %d! %d / %d * %d", index, consumerCount[index], testcase.DataCount, testcase.ProducerCount)
		}
	}
}

func Test_RunPipeline(test *testing.T) {
	const (
		COUNT_DELAY     = 100
		COUNT_IMMEDIATE = 10_000
		COUNT_BUFFERED  = 10_000
	)

	cases := []testPipelineCase{
		{COUNT_DELAY, 0, true, 1, 1},
		{COUNT_DELAY, 0, true, 10, 10},
		{COUNT_IMMEDIATE, 0, false, 1, 1},
		{COUNT_IMMEDIATE, 0, false, 1, 10},
		{COUNT_IMMEDIATE, 0, false, 10, 1},
		{COUNT_IMMEDIATE, 0, false, 100, 10},
		{COUNT_BUFFERED, COUNT_BUFFERED / 2, false, 10, 1},
	}

	for _, testcase := range cases {
		testPipeline(testcase, test)
	}
}
