## 1.5.1

- Adds TLS Renegotiation to the backend `tls.Config`.

## 1.5.0

- Added new forwarding API. See `[Session].ListenAndForward` and `[Session].ListenAndServeHTTP`. 
- Deprecates `WithHTTPServer` and `WithHTTPHandler`. Use `[Session].ListenAndServeHTTP` instead.

## 1.4.0

- Switch to `connect.ngrok-agent.com:443` as the default server address
- Add nicer error types that expose the ngrok error code

## 1.0.0 (2023-01-10)

Enhancements:
* Initial release
