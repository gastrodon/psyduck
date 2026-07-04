package consume

import (
	"fmt"
	"os/exec"

	"github.com/psyduck-etl/sdk"
)

type cmdConsumeConfig struct {
	Command   string   `psy:"command"`
	Args      []string `psy:"args"`
	Delimiter string   `psy:"delimiter"`
}

// Cmd starts a subprocess for each message and pipes the message to its stdin.
func Cmd(parse sdk.Parser) (sdk.Consumer, error) {
	config := new(cmdConsumeConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	if config.Delimiter == "" {
		config.Delimiter = "\n"
	}

	delim := []byte(config.Delimiter)

	return func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		defer close(done)
		defer close(errs)

		for msg := range recv {
			cmd := exec.Command(config.Command, config.Args...)
			stdin, err := cmd.StdinPipe()
			if err != nil {
				errs <- fmt.Errorf("cmd consumer stdin pipe: %w", err)
				return
			}

			if err := cmd.Start(); err != nil {
				errs <- fmt.Errorf("cmd consumer start: %w", err)
				return
			}

			if _, err := stdin.Write(msg); err != nil {
				errs <- fmt.Errorf("cmd consumer write: %w", err)
			}
			if len(delim) > 0 {
				stdin.Write(delim) //nolint:errcheck
			}
			stdin.Close()

			if err := cmd.Wait(); err != nil {
				errs <- fmt.Errorf("cmd consumer wait: %w", err)
			}
		}
	}, nil
}
