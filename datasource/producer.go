package datasource

import "github.com/psyduck-etl/sdk"

func LiteralProducerSet(p sdk.Producer) ProducerSet {
	consumed := false
	return func(max int) ([]sdk.Producer, error) {
		if max < 1 || consumed {
			return nil, nil
		}
		consumed = true
		return []sdk.Producer{p}, nil
	}
}

func RemoteProducerSet(meta sdk.Producer, factory func([]byte) ([]sdk.Producer, error)) ProducerSet {
	var send chan []byte
	var errs chan error
	done := false
	return func(max int) ([]sdk.Producer, error) {
		if max < 1 || done {
			return nil, nil
		}

		if send == nil {
			send = make(chan []byte)
			errs = make(chan error, 1)
			go meta(send, errs)
		}

		var producers []sdk.Producer
		for len(producers) < max {
			select {
			case err := <-errs:
				return nil, err
			case msg, ok := <-send:
				if !ok {
					done = true
					if len(producers) == 0 {
						return nil, nil
					}
					return producers, nil
				}
				batch, err := factory(msg)
				if err != nil {
					return nil, err
				}
				producers = append(producers, batch...)
			}
		}
		return producers, nil
	}
}

func JoinProducerSets(sets ...ProducerSet) ProducerSet {
	idx := 0
	return func(max int) ([]sdk.Producer, error) {
		if max < 1 {
			return nil, nil
		}

		for idx < len(sets) {
			ps, err := sets[idx](max)
			if err != nil {
				return nil, err
			}
			if ps != nil {
				return ps, nil
			}
			idx++
		}
		return nil, nil
	}
}
