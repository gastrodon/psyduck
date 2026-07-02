package parse

// ErrNoValue reports a lookup miss by key.
type ErrNoValue struct {
	Key string
}

func (e *ErrNoValue) Error() string {
	return "no value for key: " + e.Key
}

// Result provides access to fully-resolved pipeline descriptions.
type Result interface {
	Pipeline(name string) (Pipeline, error)
	Pipelines() map[string]Pipeline
}

type result struct {
	pipelines map[string]Pipeline
}

func (r *result) Pipeline(name string) (Pipeline, error) {
	p, ok := r.pipelines[name]
	if !ok {
		return Pipeline{}, &ErrNoValue{Key: name}
	}
	return p, nil
}

func (r *result) Pipelines() map[string]Pipeline {
	return r.pipelines
}

// NewResult wraps resolved pipelines in a Result. Intended for Format
// implementations.
func NewResult(pipelines map[string]Pipeline) Result {
	return &result{pipelines: pipelines}
}
