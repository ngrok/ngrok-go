package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"

	"golang.ngrok.com/ngrok/v2"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	// Create a single ngrok agent
	agent, err := ngrok.NewAgent()
	if err != nil {
		return err
	}
	defer agent.Disconnect()

	// Start HTTPS endpoint
	httpsListener, err := agent.Listen(ctx, ngrok.WithURL("https://"))
	if err != nil {
		return fmt.Errorf("HTTPS endpoint error: %v", err)
	}
	log.Println("HTTPS endpoint online:", httpsListener.URL())

	// Start TLS endpoint
	tlsListener, err := agent.Listen(ctx, ngrok.WithURL("tls://"))
	if err != nil {
		return fmt.Errorf("TLS endpoint error: %v", err)
	}
	log.Println("TLS endpoint online:", tlsListener.URL())

	// Start TCP endpoint
	tcpListener, err := agent.Listen(ctx, ngrok.WithURL("tcp://"))
	if err != nil {
		return fmt.Errorf("TCP endpoint error: %v", err)
	}
	log.Println("TCP endpoint online:", tcpListener.URL())

	// Start HTTP server in a goroutine
	go serveHTTP(httpsListener)

	// Start TLS server in a goroutine
	go serveTLS(tlsListener)

	// Start TCP server in a goroutine
	go serveTCP(ctx, tcpListener)

	// Display summary of all endpoints
	log.Println("All endpoints are now online:")
	for _, endpoint := range agent.Endpoints() {
		log.Printf("- %s", endpoint.URL())
	}

	// Block forever
	select {}
}

func serveHTTP(listener net.Listener) {
	log.Println("HTTP server exited:", http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Hello from ngrok-go HTTPS endpoint!")
	})))
}

func serveTLS(listener net.Listener) {
	config := &tls.Config{
		GetCertificate: func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
			// This would normally return a real certificate
			// For this example, we'll let the ngrok edge handle TLS termination
			return &tls.Certificate{}, nil
		},
	}

	// Wrap listener with TLS
	tlsListener := tls.NewListener(listener, config)
	log.Println("TLS server started")

	// Accept connections
	for {
		conn, err := tlsListener.Accept()
		if err != nil {
			log.Println("TLS accept error:", err)
			break
		}

		log.Println("accepted TLS connection from", conn.RemoteAddr())

		go func(c net.Conn) {
			defer c.Close()
			io.WriteString(c, "Hello from ngrok-go TLS endpoint!\n")
			buf := make([]byte, 1024)
			io.ReadFull(c, buf)
		}(conn)
	}
}

func serveTCP(ctx context.Context, listener net.Listener) {
	log.Println("TCP server started")
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("TCP accept error:", err)
			break
		}

		log.Println("accepted TCP connection from", conn.RemoteAddr())

		go func(c net.Conn) {
			defer c.Close()

			// Echo back to the client
			_, err := fmt.Fprintln(c, "Hello from ngrok-go TCP endpoint!")
			if err != nil {
				log.Println("TCP write error:", err)
				return
			}

			// Copy data back to the client (echo server)
			_, err = io.Copy(c, c)
			if err != nil {
				log.Println("TCP copy error:", err)
			}
		}(conn)
	}
}
