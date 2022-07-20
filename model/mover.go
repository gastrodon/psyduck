package model

type Mover func(chan string) chan interface{}
type MoverProducer func(interface{}) Mover
