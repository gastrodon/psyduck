package datasource

import (
	"testing"

	"github.com/psyduck-etl/sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mockConsumer() sdk.Consumer {
	return func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {}
}

func TestLiteralConsumerSetNextOnce(t *testing.T) {
	set := LiteralConsumerSet(mockConsumer())

	consumers, err := set(10)
	require.NoError(t, err)
	assert.Len(t, consumers, 1)

	consumers, err = set(10)
	require.NoError(t, err)
	assert.Nil(t, consumers)
}

func TestLiteralConsumerSetZeroMax(t *testing.T) {
	set := LiteralConsumerSet(mockConsumer())

	consumers, err := set(0)
	require.NoError(t, err)
	assert.Nil(t, consumers)
}

func TestJoinConsumerSets(t *testing.T) {
	set := JoinConsumerSets(
		LiteralConsumerSet(mockConsumer()),
		LiteralConsumerSet(mockConsumer()),
	)

	consumers, err := set(10)
	require.NoError(t, err)
	assert.Len(t, consumers, 1)

	consumers, err = set(10)
	require.NoError(t, err)
	assert.Len(t, consumers, 1)

	consumers, err = set(10)
	require.NoError(t, err)
	assert.Nil(t, consumers)
}
