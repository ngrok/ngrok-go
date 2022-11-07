# ngrok-go

This is the ngrok agent in library form, suitable for integrating directly into
an application. See `example/http.go` for example usage, or the tests in
`online_test.go`.

## Installation

The best way to install the ngrok agent SDK is through `go get`.

```sh
go get github.com/ngrok/ngrok-go
```
## Documentation
A quickstart guide and a full API reference are included in the [ngrok go sdk documentation on pkg.go.dev](https://pkg.go.dev/github.com/ngrok/ngrok-go). Check out the [ngrok Documentation](https://ngrok.com/docs) for more information about what you can do with ngrok.
## Quickstart
For more examples of using ngrok-go, check out the [/examples](/examples) folder.

The following example uses ngrok to start an http endpoint with a random url that will route traffic to the handler. The ngrok URL provided when running this example is accessible by anyone with an internet connection. 

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/ngrok/ngrok-go"
	"github.com/ngrok/ngrok-go/config"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	tun, err := ngrok.StartTunnel(ctx,
		config.HTTPEndpoint(),
		ngrok.WithAuthtokenFromEnv(),
	)
	if err != nil {
		return err
	}

	log.Println("tunnel created:", tun.URL())

	return tun.AsHTTP().Serve(ctx, http.HandlerFunc(handler))
}

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Hello from ngrok-go!")
}
```


## Support
The best place to get support using ngrok-go is through the [ngrok Slack Community](https://ngrok.com/slack). If you find bugs or have feature requests, please follow the [contributing guide](/CONTRIBUTING.md) and open an issue.

## License

ngrok-go is licensed under the terms of the MIT license.

See [LICENSE](./LICENSE) for details.
