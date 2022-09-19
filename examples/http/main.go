package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"

	"github.com/davecgh/go-spew/spew"
	"github.com/inconshreveable/log15"

	ngrok "github.com/ngrok/ngrok-go"
	"github.com/ngrok/ngrok-go/examples/common"
	"github.com/ngrok/ngrok-go/log/log15adapter"
)

func exitErr(err error) {
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func main() {
	ctx := context.Background()

	stopRequested := false
	hostname := ""

	logger := log15.New()
	logger.SetHandler(log15.LvlFilterHandler(log15.LvlInfo, log15.StderrHandler))

	for {
		opts := ngrok.ConnectOptions().
			WithAuthtoken(os.Getenv("NGROK_TOKEN")).
			WithServer(os.Getenv("NGROK_SERVER")).
			WithRegion(os.Getenv("NGROK_REGION")).
			WithLogger(log15adapter.NewLogger(logger)).
			WithMetadata("Hello, world!").
			WithRemoteCallbacks(ngrok.RemoteCallbacks{
				OnStop: func(_ context.Context, sess ngrok.Session) error {
					fmt.Println("got remote stop")
					stopRequested = true
					return nil
				},
				OnRestart: func(_ context.Context, sess ngrok.Session) error {
					fmt.Println("got remote restart")
					return nil
				},
			})

		sess := common.Unwrap(ngrok.Connect(ctx, opts))

		tun := common.Unwrap(sess.StartTunnel(ctx, ngrok.
			HTTPOptions().
			WithDomain(hostname).
			WithMetadata(`{"foo":"bar"}`).
			WithForwardsTo("foobarbaz"),
		))

		l := tun.AsHTTP()
		fmt.Println("url: ", l.URL())

		u, err := url.Parse(l.URL())
		exitErr(err)
		hostname = u.Hostname()

		err = l.Serve(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			spew.Fdump(w, r)
		}))
		if err != nil {
			fmt.Println("http server exited:", err)
		}

		if stopRequested {
			fmt.Println("exiting")
			os.Exit(0)
		}
	}
}
