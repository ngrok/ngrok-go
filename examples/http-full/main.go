package main

// This example demonstrates how to create a HTTP tunnel with all
// available configuration options illustrated.

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

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
		// tunnel configuration
		config.HTTPEndpoint(
			config.WithAllowCIDRString("0.0.0.0/0"),
			config.WithAllowUserAgent("Mozilla/5.0.*"),
			// config.WithBasicAuth("ngrok", "online1line"),
			config.WithCircuitBreaker(0.5),
			config.WithCompression(),
			config.WithDenyCIDRString("10.1.1.1/32"),
			config.WithDenyUserAgent("EvilCorp.*"),
			// config.WithDomain("<somedomain>.ngrok.io"),
			config.WithMetadata("example tunnel metadata from golang"),
			// config.WithMutualTLSCA(<cert>),
			// config.WithOAuth("google",
			// 	config.WithAllowOAuthEmail("<user>@<domain>"),
			// 	config.WithAllowOAuthDomain("<domain>"),
			// 	config.WithOAuthScope("<scope>"),
			// ),
			// config.WithOIDC("<url>", "<id>", "<secret>",
			// 	config.WithAllowOIDCEmail("<user>@<domain>"),
			// 	config.WithAllowOIDCDomain("<domain>"),
			// 	config.WithOIDCScope("<scope>"),
			// ),
			config.WithProxyProto(config.ProxyProtoNone),
			config.WithRemoveRequestHeader("X-Req-Nope"),
			config.WithRemoveResponseHeader("X-Res-Nope"),
			config.WithRequestHeader("X-Req-Yup", "true"),
			config.WithResponseHeader("X-Res-Yup", "true"),
			config.WithScheme(config.SchemeHTTPS),
			// config.WithWebsocketTCPConversion(),
			// config.WithWebhookVerification("twilio", "asdf"),
		),

		// session configuration
		// ngrok.WithAuthtoken("<auth_token>"),
		ngrok.WithAuthtokenFromEnv(),
		ngrok.WithClientInfo("go-example-full", "0.0.1"),
		ngrok.WithDisconnectHandler(func(ctx context.Context, sess ngrok.Session, err error) {
			log.Println("session disconnect:", sess, "error:", err)
		}),
		ngrok.WithHeartbeatHandler(func(ctx context.Context, sess ngrok.Session, latency time.Duration) {
			log.Println("session heartbeat:", sess, "latency:", latency)
		}),
		ngrok.WithMetadata("go-example-full"),
		ngrok.WithRestartHandler(func(ctx context.Context, sess ngrok.Session) error {
			log.Println("session restart:", sess)
			return nil
		}),
		ngrok.WithStopHandler(func(ctx context.Context, sess ngrok.Session) error {
			log.Println("session stop:", sess)
			return nil
		}),
		ngrok.WithUpdateHandler(func(ctx context.Context, sess ngrok.Session) error {
			log.Println("session update:", sess)
			return nil
		}),
	)
	if err != nil {
		return err
	}

	log.Println("tunnel created:", tun.URL())

	return http.Serve(tun, http.HandlerFunc(handler))
}

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Hello from ngrok-go!\n\nThe time is now: ", time.Now().String())
}
