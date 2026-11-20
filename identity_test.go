package ngrok

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	muxado "golang.ngrok.com/muxado/v2"

	"golang.ngrok.com/ngrok/v2/internal/tlstest"
	"golang.ngrok.com/ngrok/v2/internal/tunnel/proto"
)

// identityServer stands in for the tunnel server: it answers the Auth request
// of the i-th connection with extras[i] as the response's Extra object, or the
// last of extras for later ones, and hands each accepted connection to conns
// so a test can drop it. Each extra is the JSON object the server sends, keyed
// as the server keys it, so a client that renames a field fails the test.
func identityServer(t *testing.T, extras ...map[string]any) (addr string, conns <-chan net.Conn) {
	t.Helper()
	cert, err := tlstest.CreateCertificate()
	require.NoError(t, err)
	l, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{*cert}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })

	accepted := make(chan net.Conn, len(extras)+1)
	go func() {
		for i := 0; ; i++ {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = conn.Close() })
			accepted <- conn
			extra := extras[min(i, len(extras)-1)]
			go func() {
				// The heartbeat wrapper answers the client's heartbeats and
				// keeps them out of AcceptTypedStream.
				srv := muxado.NewHeartbeat(
					muxado.NewTypedStreamSession(muxado.Server(conn, nil)),
					func(time.Duration, bool) {},
					nil,
				)
				stream, err := srv.AcceptTypedStream()
				if err != nil {
					return
				}
				var req proto.Auth
				if err := json.NewDecoder(stream).Decode(&req); err != nil {
					return
				}
				_ = json.NewEncoder(stream).Encode(map[string]any{"ClientId": "test-client", "Extra": extra})
				// Hold the session open until the test drops the connection.
				_, _ = srv.AcceptTypedStream()
			}()
		}
	}()
	return l.Addr().String(), accepted
}

type identity struct{ principal, account string }

func identityOf(t *testing.T, sess AgentSession) identity {
	t.Helper()
	id, ok := sess.(AgentSessionIdentity)
	require.True(t, ok, "AgentSession must implement AgentSessionIdentity")
	return identity{id.PrincipalID(), id.AccountID()}
}

func connectIdentityAgent(t *testing.T, addr string, seen chan<- identity) Agent {
	t.Helper()
	a, err := NewAgent(
		WithAgentConnectURL(addr),
		WithTLSConfig(func(c *tls.Config) { c.InsecureSkipVerify = true }),
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		WithEventHandler(func(evt Event) {
			if e, ok := evt.(*EventAgentConnectSucceeded); ok {
				seen <- identityOf(t, e.Session)
			}
		}),
	)
	require.NoError(t, err)
	// The session ends with the context passed to Connect, so it lives as
	// long as the test.
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	require.NoError(t, a.Connect(ctx))
	t.Cleanup(func() { _ = a.Disconnect() })
	return a
}

func currentSession(t *testing.T, a Agent) AgentSession {
	t.Helper()
	sess, err := a.Session()
	require.NoError(t, err)
	return sess
}

func receive(t *testing.T, seen <-chan identity) identity {
	t.Helper()
	select {
	case id := <-seen:
		return id
	case <-time.After(10 * time.Second):
		t.Fatal("no EventAgentConnectSucceeded")
		return identity{}
	}
}

func TestAgentSessionIdentityFromHandshake(t *testing.T) {
	addr, conns := identityServer(t,
		map[string]any{"PrincipalID": "usr_first", "AccountID": "ac_first"},
		map[string]any{"PrincipalID": "bot_second", "AccountID": "ac_second"},
	)
	seen := make(chan identity, 2)
	a := connectIdentityAgent(t, addr, seen)

	first := identity{"usr_first", "ac_first"}
	require.Equal(t, first, receive(t, seen), "inside EventAgentConnectSucceeded")
	require.Equal(t, first, identityOf(t, currentSession(t, a)), "after Connect returns")

	// Drop the connection: the SDK reconnects on its own and the server
	// answers the second Auth with another owner.
	require.NoError(t, (<-conns).Close())
	second := identity{"bot_second", "ac_second"}
	require.Equal(t, second, receive(t, seen), "inside EventAgentConnectSucceeded after a reconnect")
	require.Equal(t, second, identityOf(t, currentSession(t, a)), "after a reconnect")
}

func TestAgentSessionIdentityEmptyWhenNotSent(t *testing.T) {
	// A server that predates the fields sends neither key, but still sends
	// the handshake's other values.
	addr, _ := identityServer(t, map[string]any{"AccountName": "Some Account"})
	seen := make(chan identity, 1)
	a := connectIdentityAgent(t, addr, seen)

	require.Equal(t, identity{}, receive(t, seen))
	require.Equal(t, identity{}, identityOf(t, currentSession(t, a)))
	details, ok := currentSession(t, a).(AgentSessionDetails)
	require.True(t, ok)
	require.Equal(t, "Some Account", details.AccountName(), "the stand-in's handshake reached the session")
}
