package datasource

import (
	"fmt"
	"testing"

	"github.com/psyduck-etl/sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mockProducer(label string) sdk.Producer {
	return func(send chan<- []byte, errs chan<- error) {
		send <- []byte(label)
		close(send)
	}
}

func TestLiteralProducerSetNextOnce(t *testing.T) {
	p := mockProducer("x")
	set := LiteralProducerSet(p)

	ps, err := set(10)
	require.NoError(t, err)
	require.Len(t, ps, 1)

	ps, err = set(10)
	require.NoError(t, err)
	assert.Nil(t, ps)
}

func TestLiteralProducerSetZeroMax(t *testing.T) {
	set := LiteralProducerSet(mockProducer("x"))

	ps, err := set(0)
	require.NoError(t, err)
	assert.Nil(t, ps)
}

func TestRemoteProducerSetBasic(t *testing.T) {
	meta := func(send chan<- []byte, errs chan<- error) {
		send <- []byte("m1")
		send <- []byte("m2")
		send <- []byte("m3")
		close(send)
	}

	factory := func(msg []byte) ([]sdk.Producer, error) {
		label := string(msg)
		return []sdk.Producer{mockProducer(label)}, nil
	}

	set := RemoteProducerSet(meta, factory)

	ps, err := set(10)
	require.NoError(t, err)
	assert.Len(t, ps, 3)

	ps, err = set(10)
	require.NoError(t, err)
	assert.Nil(t, ps)
}

func TestRemoteProducerSetFactoryError(t *testing.T) {
	meta := func(send chan<- []byte, errs chan<- error) {
		send <- []byte("ok")
		send <- []byte("bad")
		close(send)
	}

	factoryErr := fmt.Errorf("factory failed")
	factory := func(msg []byte) ([]sdk.Producer, error) {
		if string(msg) == "bad" {
			return nil, factoryErr
		}
		return []sdk.Producer{mockProducer(string(msg))}, nil
	}

	set := RemoteProducerSet(meta, factory)

	_, err := set(10)
	require.Error(t, err)
	assert.Equal(t, factoryErr, err)
}

func TestJoinProducerSets(t *testing.T) {
	a := LiteralProducerSet(mockProducer("a"))
	b := LiteralProducerSet(mockProducer("b"))
	joined := JoinProducerSets(a, b)

	ps, err := joined(10)
	require.NoError(t, err)
	require.Len(t, ps, 1)

	ps, err = joined(10)
	require.NoError(t, err)
	require.Len(t, ps, 1)

	ps, err = joined(10)
	require.NoError(t, err)
	assert.Nil(t, ps)
}
