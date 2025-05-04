# Contributing to ngrok-go

Thank you for deciding to contribute to ngrok-go!

## Reporting a bug

To report a bug, please [open a new issue](https://github.com/ngrok/ngrok-go/issues/new) with clear reproduction steps. We will triage and investigate these issues at a regular interval.

## Contributing code

Bugfixes and small improvements are always appreciated!

For any larger changes or features, please [open a new issue](https://github.com/ngrok/ngrok-go/issues/new) first to discuss whether the change makes sense. When in doubt, it's always okay to open an issue first.

## Building and running tests

The library can be compiled with `go build`.

To run tests, use `go test ./...`.

Tests are split into a number of categories that can be enabled as desired via environment variables. By default, only offline tests run which validate tunnel protocol RPC messages generated from the `config` APIs. The other tests are gated behind the following environment variables:

* `NGROK_TEST_ONLINE`: All online tests require this variable to be set and an authtoken in `NGROK_AUTHTOKEN`. Tests that require paid features will fail with appropriate error messages if your subscription doesn't support them.

This list may be incomplete and drift slightly as we add more tests and granularity. See the tests in `internal/legacy/online_test.go` and `internal/integration_tests/` for the most accurate implementations.