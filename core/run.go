package core



func RunPipeline(pipeline *Pipeline) error {
	next := pipeline.Producer()
	consume := pipeline.Consumer()
	d, t, done, err := []byte(nil), []byte(nil), false, error(nil)

	for !done {
		d, done, err = next()
		if err != nil {
			return err
		}

		t, err = pipeline.StackedTransformer(d)
		if err != nil {
			return err
		}

		err = consume(t)
		if err != nil {
			return err
		}
	}

	return nil
}
