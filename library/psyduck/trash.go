package psyduck

import "github.com/gastrodon/psyduck/model"

func consumeTrash(configRaw interface{}) model.Consumer {
	return func(signal chan string) chan interface{} {
		data := make(chan interface{}, 32)

		go func() {
			for _ = range data {
			}
		}()

		return data
	}
}
