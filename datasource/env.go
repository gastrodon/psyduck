package datasource

import "os"

type envDatasource struct{}

func Env() Datasource[string] {
	return envDatasource{}
}

func (envDatasource) Exists(key string) (bool, error) {
	_, ok := os.LookupEnv(key)
	return ok, nil
}

func (envDatasource) Get(key string) (string, error) {
	val, ok := os.LookupEnv(key)
	if !ok {
		return "", &ErrNoValue{Key: key}
	}
	return val, nil
}
