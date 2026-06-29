package datasource

import "github.com/psyduck-etl/sdk"

func LiteralConsumerSet(c sdk.Consumer) ConsumerSet {
	consumed := false
	return func(max int) ([]sdk.Consumer, error) {
		if max < 1 || consumed {
			return nil, nil
		}
		consumed = true
		return []sdk.Consumer{c}, nil
	}
}

func JoinConsumerSets(sets ...ConsumerSet) ConsumerSet {
	idx := 0
	return func(max int) ([]sdk.Consumer, error) {
		if max < 1 {
			return nil, nil
		}

		for idx < len(sets) {
			result, err := sets[idx](max)
			if err != nil {
				return nil, err
			}
			if result != nil {
				return result, nil
			}
			idx++
		}
		return nil, nil
	}
}
