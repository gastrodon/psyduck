package produce

import (
	"github.com/gastrodon/psyduck/model"
)

func produceZeroes(data chan interface{}, signal chan string) {
	for {
		data <- 0
	}
}

func Zeroes(config interface{}) model.Mover {
	return func(signal chan string) chan interface{} {
		data := make(chan interface{}, 32)
		go produceZeroes(data, signal)

		return data
	}
}
