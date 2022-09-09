package main

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"

	"github.com/davecgh/go-spew/spew"
	"github.com/inconshreveable/log15"
	libngrok "github.com/ngrok/libngrok-go"
	"github.com/ngrok/libngrok-go/log/log15adapter"
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
	reconnectCookie := ""
	hostname := ""

	logger := log15.New()
	logger.SetHandler(log15.LvlFilterHandler(log15.LvlInfo, log15.StderrHandler))

	for {
		opts := libngrok.ConnectOptions().
			WithAuthToken(os.Getenv("NGROK_TOKEN")).
			WithReconnectCookie(reconnectCookie).
			WithServer(os.Getenv("NGROK_SERVER")).
			WithRegion(os.Getenv("NGROK_REGION")).
			WithLogger(log15adapter.NewLogger(logger)).
			WithMetadata("Hello, world!").
			WithRemoteCallbacks(libngrok.RemoteCallbacks{
				OnStop: func(_ context.Context, sess libngrok.Session) error {
					fmt.Println("got remote stop")
					stopRequested = true
					return nil
				},
				OnRestart: func(_ context.Context, sess libngrok.Session) error {
					fmt.Println("got remote restart")
					return nil
				},
			})

		if caPath := os.Getenv("NGROK_CA"); caPath != "" {
			caBytes, err := ioutil.ReadFile(caPath)
			exitErr(err)
			pool := x509.NewCertPool()
			ok := pool.AppendCertsFromPEM(caBytes)
			if !ok {
				exitErr(errors.New("failed to add CA Certificates"))
			}
			opts.WithCA(pool)
		}

		sess, err := libngrok.Connect(ctx, opts)
		exitErr(err)

		reconnectCookie = sess.AuthResp().Extra.Cookie

		info, err := sess.SrvInfo()
		exitErr(err)

		fmt.Println("info: ", info)

		tun, err := sess.StartTunnel(ctx, libngrok.
			HTTPOptions().
			WithDomain(hostname).
			WithMetadata(`{"foo":"bar"}`).
			WithForwardsTo("foobarbaz"),
		)
		exitErr(err)

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
