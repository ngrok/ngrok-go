package main

import (
	"context"
	"net/http"
	"net/url"
	"os"

	"github.com/davecgh/go-spew/spew"
	"github.com/inconshreveable/log15"

	ngrok "github.com/ngrok/ngrok-go"
	"github.com/ngrok/ngrok-go/examples/common"
	"github.com/ngrok/ngrok-go/log/log15adapter"
)

func main() {
	log15.Root().SetHandler(log15.LvlFilterHandler(log15.LvlInfo, log15.StdoutHandler))

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
					log15.Info("got remote stop")
					stopRequested = true
					return nil
				},
				OnRestart: func(_ context.Context, sess ngrok.Session) error {
					log15.Info("got remote restart")
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
		log15.Info("started tunnel", "url", l.URL())

		u, err := url.Parse(l.URL())
		common.ExitErr(err)
		hostname = u.Hostname()

		err = l.Serve(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			spew.Fdump(w, r)
		}))
		if err != nil {
			log15.Info("http server exited", "error", err)
		}

		if stopRequested {
			log15.Info("exiting")
			os.Exit(0)
		}
	}
}
