package scyther

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

func queueURL(config QueueConfig) string {
	return fmt.Sprintf("%s/queues/%s", config.URL, config.Queue)
}

func getQueueHead(config QueueConfig) string {
	response, err := http.Get(queueURL(config) + "/head")
	if err != nil {
		panic(err)
	}

	defer response.Body.Close()
	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}

	return string(bodyBytes)
}

func putQueueHead(config QueueConfig, each []byte) {
	body := bytes.NewReader(each)
	request, err := http.NewRequest("PUT", queueURL(config), body)
	if err != nil {
		panic(err)
	}

	client := &http.Client{}
	if _, err := client.Do(request); err != nil {
		panic(err)
	}
}
