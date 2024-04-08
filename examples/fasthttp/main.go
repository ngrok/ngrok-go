package main

import (
	"context"
	"fmt"
	"log"

	"github.com/valyala/fasthttp"

	"golang.ngrok.com/ngrok"
	"golang.ngrok.com/ngrok/config"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatalf("main: %v", err)
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

	var serv fasthttp.Server

	serv.Handler = func(ctx *fasthttp.RequestCtx) {
		fmt.Fprintf(ctx, "Hello! You're requesting %q", ctx.RequestURI())
	}

	err = serv.Serve(tun)
	if err != nil {
		return err
	}

	return nil
}
