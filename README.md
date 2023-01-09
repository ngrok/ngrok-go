[![Go Reference](https://pkg.go.dev/badge/golang.ngrok.com/ngrok.svg)](https://pkg.go.dev/golang.ngrok.com/ngrok)
[![Go](https://github.com/ngrok/ngrok-go/actions/workflows/buildandtest.yml/badge.svg)](https://github.com/ngrok/ngrok-go/actions/workflows/buildandtest.yml)
# ngrok-go

ngrok is a globally distributed reverse proxy commonly used for quickly getting a public URL to a service running inside a private network, such as on your local laptop. The ngrok agent is usually deployed inside a private network and is used to communicate with the ngrok cloud service.

This is the ngrok agent in library form, suitable for integrating directly into Go applications. This allows you to quickly build ngrok into your application with no separate process to manage. 

See [`examples/http/main.go`](/examples/http/main.go) for example usage, or the tests in [`online_test.go`](/online_test.go).

For working with the [ngrok API](https://ngrok.com/docs/api/), check out the [ngrok Go API Client Library](https://github.com/ngrok/ngrok-api-go).

## Installation

The best way to install the ngrok agent SDK is through `go get`.

```sh
go get golang.ngrok.com/ngrok
```
## Documentation
A full API reference is included in the [ngrok go sdk documentation on pkg.go.dev](https://pkg.go.dev/golang.ngrok.com/ngrok). Check out the [ngrok Documentation](https://ngrok.com/docs) for more information about what you can do with ngrok.
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
	tun, err := ngrok.Listen(ctx,
		config.HTTPEndpoint(),
		ngrok.WithAuthtokenFromEnv(),
	)
	if err != nil {
		return err
	}

	log.Println("tunnel created:", tun.URL())

	return http.Serve(tun, http.HandlerFunc(handler))
}

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Hello from ngrok-go!")
}
```

## Support
The best place to get support using ngrok-go is through the [ngrok Slack Community](https://ngrok.com/slack). If you find bugs or would like to contribute code, please follow the instructions in the [contributing guide](/CONTRIBUTING.md).

## License

ngrok-go is licensed under the terms of the MIT license.

See [LICENSE](./LICENSE.txt) for details.
