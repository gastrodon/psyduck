package produce

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"

	"github.com/psyduck-etl/sdk"
)

type cmdProduceConfig struct {
	Command    string   `psy:"command"`
	Args       []string `psy:"args"`
	SplitLines bool     `psy:"split-lines"`
}

func Cmd(parse sdk.Parser) (sdk.Producer, error) {
	config := new(cmdProduceConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)

		cmd := exec.Command(config.Command, config.Args...)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			errs <- fmt.Errorf("cmd stdout pipe: %w", err)
			return
		}

		if err := cmd.Start(); err != nil {
			errs <- fmt.Errorf("cmd start: %w", err)
			return
		}

		if config.SplitLines {
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				line := scanner.Bytes()
				cp := make([]byte, len(line))
				copy(cp, line)
				send <- cp
			}
			if scanErr := scanner.Err(); scanErr != nil {
				errs <- scanErr
			}
		} else {
			data, err := io.ReadAll(stdout)
			if err != nil {
				errs <- fmt.Errorf("cmd read: %w", err)
			} else if len(data) > 0 {
				send <- data
			}
		}

		if err := cmd.Wait(); err != nil {
			errs <- fmt.Errorf("cmd wait: %w", err)
		}
	}, nil
}
