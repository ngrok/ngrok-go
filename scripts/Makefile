.PHONY: fmt
fmt:
	go install golang.org/x/tools/cmd/goimports@v0.3.0
	goimports -format-only -w -local golang.ngrok.com *.go config examples

.PHONY: lint
lint:
	golangci-lint run --enable=bodyclose .

.PHONY: test
test:
	go test -coverprofile cover.out ./...

.PHONY: coverage
coverage: test
	go tool cover -html cover.out

docs:
	pkill pkgsite || true
	pkgsite >/dev/null 2>&1 & \
		xdg-open http://127.0.0.1:8080/golang.ngrok.com/ngrok

.PHONY: gen-proto
gen-proto:
	protoc --go_out=. --go_opt=module=golang.ngrok.com/ngrok ./proto/middleware.proto
	protoc --go_out=. --go_opt=module=golang.ngrok.com/ngrok ./proto/conn_header.proto
