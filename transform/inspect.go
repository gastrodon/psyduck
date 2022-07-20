package transform

import (
	"fmt"
)

func Inspect(data interface{}) interface{} {
	fmt.Printf("%#v\n", data)

	return data
}
