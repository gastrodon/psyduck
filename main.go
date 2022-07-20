package main

import (
	"github.com/gastrodon/psyduck/core"
	"github.com/gastrodon/psyduck/library/scyther"
	"github.com/gastrodon/psyduck/model"
	"github.com/gastrodon/psyduck/transform"
)

const SCYTHER_URL = "https://webhook.site/96cca981-6550-4772-a72d-5c1498105d4a"

var queueSource = scyther.QueueConfig{
	URL:   SCYTHER_URL,
	Queue: "foobar",
}

var queueDest = scyther.QueueConfig{
	URL:   SCYTHER_URL,
	Queue: "dest",
}

func main() {
	produce := scyther.ProduceQueue(interface{}(queueSource))
	consume := scyther.ConsumeQueue(interface{}(queueDest))

	core.RunPipeline(produce, consume, []model.Transformer{transform.Inspect})
}
