# Contributing to ngrok-go

Thank you for deciding to contribute to ngrok-go!

## Reporting a bug

To report a bug, please [open a new issue](https://github.com/ngrok/ngrok-go/issues/new) with clear reproduction steps. We will triage and investigate these issues at a regular interval.

## Contributing code

Bugfixes and small improvements are always appreciated!

For any larger changes or features, please [open a new issue](https://github.com/ngrok/ngrok-go/issues/new) first to discuss whether the change makes sense. When in doubt, it's always okay to open an issue first.

## Building and running tests

The library can be compiled with `go build`.

To run tests, `go test`.

Tests are split into a number of categories that can be enabled as desired via environment variables. By default, only offline tests run which validate tunnel protocol RPC messages generated from the `config` APIs. The other tests are gated behind the following environment variables:

* `NGROK_TEST_ONLINE`: All online tests require this variable to be set
* `NGROK_TEST_AUTHED`: Enables tests that require an ngrok account and that the authtoken is set in `NGROK_AUTHTOKEN`.
* `NGROK_TEST_PAID`: Enables online, authenticated tests that require access to paid features. If your subscription doesn't support a feature being tested, you should see error messages to that effect.
* `NGROK_TEST_LONG`: Enables online tests that may take longer than most. May also require the `AUTHED` and/or `PAID` groups enabled.
* `NGROK_TEST_FLAKEY`: Enable online tests that may be unreliable. Their success or failure may depend on network conditions, timing, solar flares, ghosts in the machine, etc.

This list may be incomplete and drift slightly as we add more tests and granularity. See the tests in `online_test.go` for the most accurate list.