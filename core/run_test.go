package core

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/gastrodon/psyduck/parse"
	"github.com/hashicorp/hcl/v2"
	"github.com/psyduck-etl/sdk"
	"github.com/sirupsen/logrus"
)

type Values func() (int, int)

type testPipelineCase struct {
	DataCount        int
	Delay            bool
	ProducerCount    int
	ConsumerCount    int
	ProducerParallel uint
}

func makeTestProducer(testcase testPipelineCase) (func() <-chan result[sdk.Producer], func() []int) {
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

	return func() <-chan result[sdk.Producer] {
		c := make(chan result[sdk.Producer], len(producers))
		for _, p := range producers {
			c <- ok(p)
		}
		close(c)
		return c
	}, func() []int { return counts }
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
		Producer:          producer,
		Consumer:          consumer,
		Transformer:       transformer,
		logger:            pipelineLogger(),
		ParallelProducers: testcase.ProducerParallel,
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
		{COUNT_DELAY, true, 1, 1, 0},
		{COUNT_DELAY, true, 10, 10, 0},
		{COUNT_IMMEDIATE, false, 1, 1, 0},
		{COUNT_IMMEDIATE, false, 1, 10, 0},
		{COUNT_IMMEDIATE, false, 10, 1, 0},
		{COUNT_IMMEDIATE, false, 10, 10, 0},
		{COUNT_IMMEDIATE, false, 10, 1, 4},
		{COUNT_IMMEDIATE, false, 10, 10, 4},
		{COUNT_IMMEDIATE, false, 10, 1, 20},
		{COUNT_IMMEDIATE, false, 10, 10, 20},
	}

	for i, testcase := range cases {
		if testcase.Delay && testing.Short() {
			continue
		}

		if err := testPipeline(testcase); err != nil {
			test.Fatalf("case %d failed: %s", i, err)
		}
	}
}

func Test_RunPipeline_filtering(test *testing.T) {
	received, limit, fac := byte(0), byte(100), byte(2)
	testcase := &Pipeline{
		Producer: func() <-chan result[sdk.Producer] {
			c := make(chan result[sdk.Producer], 1)
			c <- ok[sdk.Producer](func(send chan<- []byte, errs chan<- error) {
				for i := byte(0); i < limit; i++ {
					send <- []byte{i}
				}
				close(send)
			})
			close(c)
			return c
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

func drawDiags(d hcl.Diagnostics) string {
	buf := make([]string, len(d))
	for i, diag := range d {
		buf[i] = diag.Error()
	}

	return strings.Join(buf, "\n")
}

func Test_RunPipeline_remote(t *testing.T) {
	pin := &sdk.Plugin{
		Name: "test",
		Resources: []*sdk.Resource{
			{
				Kinds: sdk.PRODUCER,
				Name:  "mono",
				Spec:  []*sdk.Spec{},
				ProvideProducer: func(parse sdk.Parser) (sdk.Producer, error) {
					cfg := new(struct {
						V byte `cty:"v"`
					})

					if err := parse(cfg); err != nil {
						return nil, err
					}

					return func(send chan<- []byte, errs chan<- error) {
						send <- []byte{cfg.V}
						close(send)
					}, nil
				},
			},
			{
				Kinds: sdk.PRODUCER,
				Name:  "meta",
				Spec:  []*sdk.Spec{},
				ProvideProducer: func(parse sdk.Parser) (sdk.Producer, error) {
					cfg := new(struct {
						V byte `cty:"v"`
						C int  `cty:"c"`
					})

					if err := parse(cfg); err != nil {
						return nil, err
					}

					return func(send chan<- []byte, errs chan<- error) {
						for i := 0; i < cfg.C; i++ {
							send <- []byte(fmt.Sprintf(`
							produce "mono" {
								v = %d
							}`, cfg.V))
						}
						close(send)
					}, nil
				},
			},
		},
	}

	lib := NewLibrary([]*sdk.Plugin{pin})

	group, diags := parse.NewFile("test-remote-producers", []byte(`
	produce-from "meta" {
		v = 100
		c = 5
	}

	produce-from "meta" {
		v = 50
		c = 10
	}`)).Pipelines(lib.Ctx())
	if diags.HasErrors() {
		t.Fatalf("failed to parse: %s", drawDiags(diags))
	}

	pline, err := BuildPipeline(group.Monify(), lib)
	if err != nil {
		t.Fatalf("failed to build: %s", err)
	}

	tabs := make(map[byte]int, 2)
	alt := pline.Transformer
	pline.Transformer = func(in []byte) ([]byte, error) {
		if v, ok := tabs[in[0]]; ok {
			tabs[in[0]] = v + 1
		} else {
			tabs[in[0]] = 1
		}

		return alt(in)
	}

	if err := RunPipeline(pline); err != nil {
		t.Fatalf("failed to run: %s", err)
	}

	if tabs[100] != 5 {
		t.Fatalf("tabs[100] mismatch: 5 != %d", tabs[100])
	}

	if tabs[50] != 10 {
		t.Fatalf("tabs[50] mismatch: 10 != %d", tabs[50])

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
	produceErr := func(n int) func() <-chan result[sdk.Producer] {
		return func() <-chan result[sdk.Producer] {
			c := make(chan result[sdk.Producer], 1)
			c <- ok[sdk.Producer](func(send chan<- []byte, errs chan<- error) {
				for i := 0; i < n; i++ {
					send <- []byte{0}
				}

				errs <- errors.New(errText)
			})
			close(c)
			return c
		}
	}

	consumeErr := func(n int) sdk.Consumer {
		return func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
			for i := 0; i < n; i++ {
				<-recv
			}

			errs <- errors.New(errText)
		}
	}

	transformeErr := func(n int) sdk.Transformer {
		i := 0
		return func(in []byte) ([]byte, error) {
			if i >= n {
				return nil, errors.New(errText)
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
			Producer: func() <-chan result[sdk.Producer] {
				c := make(chan result[sdk.Producer], 2)
				c <- <-produceErr(50)()
				c <- <-produceErr(math.MaxInt32)() // this is silly code!
				close(c)
				return c
			},
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

	for _, testcase := range testcases {
		didFire := false
		testcase.pipeline.logger = pipelineTestLogger(func() {
			didFire = true
		})
		if err := RunPipeline(testcase.pipeline); err == nil {
			test.Fatal("no error returned")
		} else if err.Error() != testcase.want.Error() {
			test.Fatalf("other error: %s != %s!", err, testcase.want)
		} else if !didFire {
			test.Fatal("logger hook did not fire")
		}
	}
}
