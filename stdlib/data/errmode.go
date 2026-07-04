package data

import "fmt"

// ErrMode governs what happens when an operation fails on a message. It is
// iota-based so new modes can be appended without breaking existing callers —
// the model deliberately starts small (err + drop) and leaves room to grow.
type ErrMode int

const (
	// ErrPropagate ("err") sends the error to the pipeline's error channel,
	// the default behaviour.
	ErrPropagate ErrMode = iota
	// ErrDrop ("drop") swallows the error and drops the message.
	ErrDrop
)

// String renders an ErrMode as its config name.
func (m ErrMode) String() string {
	switch m {
	case ErrPropagate:
		return "err"
	case ErrDrop:
		return "drop"
	default:
		return fmt.Sprintf("errmode(%d)", int(m))
	}
}

// ParseErrMode parses the config spelling of an error mode. An empty string
// defaults to ErrPropagate.
func ParseErrMode(s string) (ErrMode, error) {
	switch s {
	case "", "err":
		return ErrPropagate, nil
	case "drop":
		return ErrDrop, nil
	default:
		return ErrPropagate, fmt.Errorf("unknown error mode %q (want \"err\" or \"drop\")", s)
	}
}
