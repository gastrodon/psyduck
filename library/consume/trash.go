package consume

func receiveTrash(trash chan interface{}) {
	for {
		_ <- trash
	}
}

func Trash() chan interface{} {
  trash := make(chan interface, 32)
  go receiveTrash(trash)

  return trash
}
