package main

import (
	"context"
	"fmt"
	"log"

	"github.com/valyala/fasthttp"

	"golang.ngrok.com/ngrok/v2"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatalf("main: %v", err)
	}
}

func run(ctx context.Context) error {
	ln, err := ngrok.Listen(ctx)
	if err != nil {
		return err
	}
	log.Println("endpoint online", ln.URL())

	var serv fasthttp.Server

	serv.Handler = func(ctx *fasthttp.RequestCtx) {
		fmt.Fprintf(ctx, "Hello! You're requesting %q", ctx.RequestURI())
	}

	err = serv.Serve(ln)
	if err != nil {
		return err
	}

	return nil
}
