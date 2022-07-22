package model

type Mover func(chan string) chan interface{}
type MoverProvider func(interface{}) Mover

type Producer Mover
type ProducerProvider func(interface{}) Producer

type Consumer Mover
type ConsumerProvider func(interface{}) Consumer
