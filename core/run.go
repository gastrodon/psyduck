package core

func RunPipeline(pipeline *Pipeline, signal chan string) {
	chanProducer := pipeline.Producer(signal)
	chanConsumer := pipeline.Consumer(signal)

	for data := range chanProducer {
		chanConsumer <- pipeline.StackedTransformer(data)
	}
}
