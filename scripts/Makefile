.PHONY: test
test:
	go test -coverprofile cover.out

.PHONY: coverage
coverage: test
	go tool cover -html cover.out

docs:
	godoc -index -http 127.0.0.1:6060 & \
		xdg-open http://127.0.0.1:6060
