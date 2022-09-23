.PHONY: test
test:
	go test -coverprofile cover.out

.PHONY: coverage
coverage: test
	go tool cover -html cover.out

docs:
	pkgsite >/dev/null 2>&1 & \
		xdg-open http://127.0.0.1:8080/github.com/ngrok/libngrok-go
