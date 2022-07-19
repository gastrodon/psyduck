package transform

func MakeZeroes() chan int {
	channel := make(chan int, 8)

	for {
		channel <- 0
	}
}
