package consume

import (
	"github.com/gastrodon/psyduck/model"
)

func receiveTrash(trash chan interface{}, signal chan string) {
	for {
		_ = <-trash
	}
}

func Trash(config interface{}) model.Mover {
	return func(signal chan string) chan interface{} {
		data := make(chan interface{}, 32)
		go receiveTrash(data, signal)

		return data
	}
}
