package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
	"sync"

	"github.com/inconshreveable/log15"
	"github.com/ngrok/libngrok-go"
)

func unwrap[T any](out T, err error) T {
	if err != nil {
		log15.Error("unwrapped error", "error", err)
		os.Exit(1)
	}

	return out
}

func httpResp(w http.ResponseWriter, code int, msg string, args ...any) {
	w.WriteHeader(code)
	fmt.Fprintf(w, msg, args...)
}

func main() {
	log15.Root().SetHandler(log15.LvlFilterHandler(log15.LvlInfo, log15.StdoutHandler))

	restart := true

	ctx := context.Background()

	for restart {
		sess := unwrap(libngrok.Connect(ctx, libngrok.ConnectOptions().
			WithLog15(log15.Root()).
			WithRemoteCallbacks(libngrok.RemoteCallbacks{
				OnStop: func(ctx context.Context, sess libngrok.Session) error {
					restart = false
					log15.Info("exiting due to remote request")
					return nil
				},
				OnRestart: func(ctx context.Context, sess libngrok.Session) error {
					log15.Info("restarting due to remote request")
					return nil
				},
			}).
			WithAuthToken(os.Getenv("NGROK_TOKEN")),
		))

		tun := unwrap(sess.StartTunnel(ctx, libngrok.HTTPOptions().
			WithForwardsTo("tunnel management"),
		))

		log15.Info("managing tunnels", "url", tun.URL())

		log15.Info("management server shut down", "error", tun.AsHTTP().Serve(ctx, manageTunnels(ctx, sess)))
	}
}

func manageTunnels(ctx context.Context, sess libngrok.Session) http.Handler {
	tunnels := sync.Map{}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GET /create will start a new tunnel with the given provider and
		// allowed user. The new tunnel ID and url will be returned in the HTTP
		// response.
		if r.Method == http.MethodGet && r.URL.Path == "/create" {
			provider := r.FormValue("provider")
			allowed := r.FormValue("allow")
			if provider == "" || allowed == "" {
				httpResp(w, http.StatusBadRequest, "must supply 'provider' and 'allow' form values\n")
				return
			}

			tun, err := sess.StartTunnel(ctx, libngrok.HTTPOptions().
				WithForwardsTo("dump requests").
				WithOAuth(libngrok.OAuthProvider(provider).
					AllowEmail(allowed)))
			if err != nil {
				httpResp(w, http.StatusInternalServerError, "error starting tunnel: %v\n", err)
				return
			}

			tunnels.Store(tun.ID(), tun)

			go tun.AsHTTP().Serve(ctx, dumpRequest())

			httpResp(w, http.StatusOK, "%s: %s\n", tun.ID(), tun.URL())
			log.Printf("Started tunnel for %s on %s: %s\n", allowed, provider, tun.URL())
			return
		}

		// DELETE /<tunnel-id> will close the tunnel with the given ID
		if r.Method == http.MethodDelete {
			tun, ok := tunnels.LoadAndDelete(strings.TrimPrefix(r.URL.Path, "/"))
			if ok {
				tun := tun.(libngrok.Tunnel)
				log15.Info("closing tunnel", "url", tun.URL())
				tun.Close()
				httpResp(w, http.StatusOK, "tunnel closed\n")
			} else {
				httpResp(w, http.StatusNotFound, "no tunnel found with ID %s\n", r.URL.Path)
			}

			return
		}

		httpResp(w, http.StatusBadRequest, "%s not supported for path %s", r.Method, r.URL.Path)
	})
}

func dumpRequest() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dump, err := httputil.DumpRequest(r, true)
		if err != nil {
			httpResp(w, http.StatusInternalServerError, "failed to dump request body\n")
			return
		}

		i := 0
		for i < len(dump) {
			n, err := w.Write(dump[i:])
			i += n
			if err != nil {
				return
			}
		}
	})
}
