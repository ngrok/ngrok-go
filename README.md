# ngrok-go

[![Go Reference](https://pkg.go.dev/badge/golang.ngrok.com/ngrok.svg)](https://pkg.go.dev/golang.ngrok.com/ngrok)
[![Go](https://github.com/ngrok/ngrok-go/actions/workflows/buildandtest.yml/badge.svg)](https://github.com/ngrok/ngrok-go/actions/workflows/buildandtest.yml)
[![MIT licensed](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/ngrok/ngrok-rust/blob/main/LICENSE-MIT)


[ngrok](https://ngrok.com) is a simplified API-first ingress-as-a-service that adds connectivity, security, and observability to your apps.

ngrok-go is an open source and idiomatic library for embedding ngrok networking directly into Go applications. If you’ve used ngrok before, you can think of ngrok-go as the ngrok agent packaged as a Go library.

ngrok-go lets developers serve Go apps on the internet in a single line of code without setting up low-level network primitives like IPs, certificates, load balancers and even ports! Applications using ngrok-go listen on ngrok’s global ingress network but they receive the same interface any Go app would expect (net.Listener) as if it listened on a local port by calling net.Listen(). This makes it effortless to integrate ngrok-go into any application that uses Go's net or net/http packages.

See [`examples/http/main.go`](/examples/http/main.go) for example usage, or the tests in [`online_test.go`](/online_test.go).

For working with the [ngrok API](https://ngrok.com/docs/api/), check out the [ngrok Go API Client Library](https://github.com/ngrok/ngrok-api-go).

## Installation

The best way to install the ngrok agent SDK is through `go get`.

```sh
go get golang.ngrok.com/ngrok
```

## Documentation

A full API reference is included in the [ngrok go sdk documentation on pkg.go.dev](https://pkg.go.dev/golang.ngrok.com/ngrok). Check out the [ngrok Documentation](https://ngrok.com/docs) for more information about what you can do with ngrok.

For additional information, be sure to also check out the [ngrok-go launch announcement](https://ngrok.com/blog-post/ngrok-go)!

## Quickstart

For more examples of using ngrok-go, check out the [/examples](/examples) folder.

The following example uses ngrok to start an http endpoint with a random url that will route traffic to the handler. The ngrok URL provided when running this example is accessible by anyone with an internet connection.

The ngrok authtoken is pulled from the `NGROK_AUTHTOKEN` environment variable. You can find your authtoken by logging into the [ngrok dashboard](https://dashboard.ngrok.com/get-started/your-authtoken).

You can run this example with the following command:

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

	"golang.ngrok.com/ngrok"
	"golang.ngrok.com/ngrok/config"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	ln, err := ngrok.Listen(ctx,
		config.HTTPEndpoint(),
		ngrok.WithAuthtokenFromEnv(),
	)
	if err != nil {
		return err
	}

	log.Println("Ingress established at:", ln.URL())

	return http.Serve(ln, http.HandlerFunc(handler))
}

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Hello from ngrok-go!")
}
```

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
