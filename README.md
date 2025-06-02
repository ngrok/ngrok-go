# ngrok-go

[![Go Reference](https://pkg.go.dev/badge/golang.ngrok.com/ngrok/v2.svg)](https://pkg.go.dev/golang.ngrok.com/ngrok/v2)
[![Go](https://github.com/ngrok/ngrok-go/actions/workflows/buildandtest.yml/badge.svg)](https://github.com/ngrok/ngrok-go/actions/workflows/buildandtest.yml)
[![MIT licensed](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/ngrok/ngrok-go/blob/main/LICENSE.txt)


[ngrok](https://ngrok.com) is an API gateway cloud service that forwards to
applications running anywhere.

ngrok-go is an open source and idiomatic Go package for embedding ngrok
networking directly into your Go applications. If you've used ngrok before, you
can think of ngrok-go as the ngrok agent packaged as a Go library.

ngrok-go enables you to serve Go apps on the internet in a single line of code
without setting up low-level network primitives like IPs, certificates, load
balancers and even ports! Applications using ngrok-go listen on ngrok's global
cloud service but, they receive connections using the same interface
(net.Listener) that any Go app would expect if it listened on a local port.

For working with the [ngrok API](https://ngrok.com/docs/api/), check out the [ngrok Go API Client Library](https://github.com/ngrok/ngrok-api-go).

## Installation

Install ngrok-go with `go get`.

```sh
go get golang.ngrok.com/ngrok/v2
```

## Documentation

- [ngrok-go API Reference](https://pkg.go.dev/golang.ngrok.com/ngrok/v2) on pkg.go.dev.
- [ngrok Documentation](https://ngrok.com/docs) for what you can do with ngrok.
- [Examples](./examples) are another great way to get started.
- [ngrok-go launch announcement](https://ngrok.com/blog-post/ngrok-go) for more context on why we built it. The examples in the blog post may be out of date for the new API.

## Quickstart

The following example starts a Go web server that receives traffic from an
endpoint on ngrok's cloud service with a randomly-assigned URL. The ngrok URL
provided when running this example is accessible by anyone with an internet
connection.

You need an ngrok authtoken to run the following example, which you can get from
the [ngrok dashboard](https://dashboard.ngrok.com/get-started/your-authtoken).

Run this example with the following command:

```sh
NGROK_AUTHTOKEN=xxxx_xxxx go run examples/http/main.go
```

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"golang.ngrok.com/ngrok/v2"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	// ngrok.Listen uses ngrok.DefaultAgent which uses the NGROK_AUTHTOKEN
	// environment variable for auth
	ln, err := ngrok.Listen(ctx)
	if err != nil {
		return err
	}

	log.Println("Endpoint online", ln.URL())

	// Serve HTTP traffic on the ngrok endpoint
	return http.Serve(ln, http.HandlerFunc(handler))
}

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Hello from ngrok-go!")
}
```

## Traffic Policy

You can use ngrok's [Traffic Policy](https://ngrok.com/docs/traffic-policy/)
engine to apply API Gateway behaviors at ngrok's cloud service to auth, route,
block and rate-limit the traffic. For example:

```go
tp := `
on_http_request:
  - name: "rate limit by ip address"
    actions:
    - type: rate-limit
      config:
        name: client-ip-rate-limit
        algorithm: sliding_window
        capacity: 30
        rate: 60s
        bucket_key:
          - conn.client_ip
  - name: "federate to google for auth"
    actions:
    - type: oauth
      config:
        provider: google
  - name: "block users without an 'example.com' domain"
    expressions:
      - "!actions.ngrok.oauth.identity.email.endsWith('@example.com')"
    actions:
      - type: custom-response
        config:
          status_code: 403
          content: "${actions.ngrok.oauth.identity.name} is not allowed"
`

ln, err := ngrok.Listen(ctx, ngrok.WithTrafficPolicy(tp))
if err != nil {
	return err
}
```

## Examples

There are many more great examples you can reference to get started:

- [Creating a TCP endpoint](./examples/tcp/) and handling TCP connections directly.
- [Forwarding to another URL](./examples/forward/) instead of handling connections yourself.
- [Adding Traffic Policy](./examples/traffic-policy/) in front of your app for authentication, rate limiting, etc.

## Support

The best place to get support using ngrok-go is through the [ngrok Slack Community](https://ngrok.com/slack). If you find bugs or would like to contribute code, please follow the instructions in the [contributing guide](/CONTRIBUTING.md).

## Changelog

Changes to `ngrok-go` are tracked under [CHANGELOG.md](https://github.com/ngrok/ngrok-go/blob/main/CHANGELOG.md).

## Join the ngrok Community

- Check out [our official docs](https://docs.ngrok.com)
- Read about updates on [our blog](https://ngrok.com/blog)
- Open an [issue](https://github.com/ngrok/ngrok-go/issues) or [pull request](https://github.com/ngrok/ngrok-go/pulls)
- Join our [Slack community](https://ngrok.com/slack)
- Follow us on [X / Twitter (@ngrokHQ)](https://twitter.com/ngrokhq)
- Subscribe to our [Youtube channel (@ngrokHQ)](https://www.youtube.com/@ngrokhq)

## License

ngrok-go is licensed under the terms of the MIT license.

See [LICENSE](./LICENSE.txt) for details.
