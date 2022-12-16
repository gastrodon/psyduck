default: depends

clean:
	rm -rf vendor/*
	go mod edit -dropreplace github.com/zclconf/go-cty

depends:
	mkdir -p replace/
	git clone git@github.com:gastrodon/go-cty replace/go-cty || :
	go mod edit -replace github.com/zclconf/go-cty=./replace/go-cty