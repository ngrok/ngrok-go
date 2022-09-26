package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/davecgh/go-spew/spew"
	"github.com/gorilla/mux"
	"github.com/inconshreveable/log15"

	"github.com/ngrok/ngrok-go"
	"github.com/ngrok/ngrok-go/examples/common"
	"github.com/ngrok/ngrok-go/log/log15adapter"
)

func main() {
	log15.Root().SetHandler(log15.LvlFilterHandler(log15.LvlInfo, log15.StdoutHandler))

	ctx := context.Background()

	logger := log15.New()
	logger.SetHandler(log15.LvlFilterHandler(log15.LvlInfo, log15.StderrHandler))

	r := mux.NewRouter()
	r.HandleFunc("/", helloHandler)
	r.HandleFunc("/dump", dumpHandler)

	opts := ngrok.ConnectOptions().
		WithAuthtoken(os.Getenv("NGROK_TOKEN")).
		WithLogger(log15adapter.NewLogger(logger))

	sess := common.Unwrap(ngrok.Connect(ctx, opts))

	tun := common.Unwrap(sess.StartTunnel(ctx, ngrok.HTTPOptions()))

	l := tun.AsHTTP()
	log15.Info("started tunnel", "url", l.URL())

	err := l.Serve(ctx, r)
	if err != nil {
		log15.Info("http server exited", "error", err)
	}
}

func helloHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Hello, world!")
}

func dumpHandler(w http.ResponseWriter, r *http.Request) {
	spew.Fdump(w, r)
}
