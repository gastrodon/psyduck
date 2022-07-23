package ifunny

import (
	"github.com/gastrodon/psyduck/model"
)

func produceFeatures(parse func(interface{}) error) model.Producer {
	config := *new(IFunnyConfig)
	if err := parse(&config); err != nil {
		panic(err)
	}

	return func(signal chan string) chan interface{} {
		data := make(chan interface{}, 32)
		nextPage := ""

		go func() {
			for {
				page := getFeaturesPage(config, nextPage)
				nextPage = page.Paging.Cursors.Next

				for _, content := range page.Items {
					data <- content
				}
			}
		}()

		return data
	}
}
