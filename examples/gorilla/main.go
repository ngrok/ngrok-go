package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/gorilla/mux"

	"github.com/davecgh/go-spew/spew"
	"github.com/inconshreveable/log15"
	libngrok "github.com/ngrok/libngrok-go"
	"github.com/ngrok/libngrok-go/log/log15adapter"
	"github.com/ngrok/ngrok-go/examples/common"
)

func main() {
	log15.Root().SetHandler(log15.LvlFilterHandler(log15.LvlInfo, log15.StdoutHandler))

	ctx := context.Background()

	logger := log15.New()
	logger.SetHandler(log15.LvlFilterHandler(log15.LvlInfo, log15.StderrHandler))

	r := mux.NewRouter()
	r.HandleFunc("/", helloHandler)
	r.HandleFunc("/dump", dumpHandler)

	opts := libngrok.ConnectOptions().
		WithAuthToken(os.Getenv("NGROK_TOKEN")).
		WithLogger(log15adapter.NewLogger(logger))

	sess := common.Unwrap(libngrok.Connect(ctx, opts))

	tun := common.Unwrap(sess.StartTunnel(ctx, libngrok.HTTPOptions()))

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
