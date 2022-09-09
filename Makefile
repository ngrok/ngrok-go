.PHONY: build
build:
	go build

.PHONY: examples
examples: example/http

.PHONY: run-example-%
run-example-%:
	cd example && go run $*.go

.PHONY: example/%
example/%: example/%.go
	cd example && go build -o $* $*.go

.PHONY: test
test:
	go test -coverprofile cover.out

.PHONY: coverage
coverage: test
	go tool cover -html cover.out