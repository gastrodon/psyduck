package psyduck

import (
	"fmt"

	"github.com/gastrodon/psyduck/model"
)

func inspect(configRaw interface{}) model.Transformer {
	return func(data interface{}) interface{} {
		fmt.Println(data)
		return data
	}
}
