package client

import (
	"encoding/json"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.ngrok.com/muxado/v2"
	"golang.ngrok.com/ngrok/v2/internal/tunnel/proto"
)

// lockedBuffer collects log output written from the session's goroutines.
type lockedBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestRPCDoesNotLogCredentials covers the leak where it happened: rpc logs the
// request and the response at debug level, and a JSON handler encodes them by
// reflection. proto's own tests can't see a regression here, because slog only
// resolves a LogValuer at the top level of an attribute value.
func TestRPCDoesNotLogCredentials(t *testing.T) {
	const (
		credential = "fake-credential-in-rpc"
		reqCookie  = "fake-request-cookie"
		respCookie = "fake-response-cookie"
	)

	clientConn, serverConn := net.Pipe()
	// The heartbeat wrapper answers the client's heartbeats and keeps them out
	// of AcceptTypedStream, which would otherwise hand us a heartbeat stream
	// instead of the Auth request.
	srv := muxado.NewHeartbeat(
		muxado.NewTypedStreamSession(muxado.Server(serverConn, nil)),
		func(time.Duration, bool) {},
		nil,
	)
	defer srv.Close()

	// Stand in for the tunnel server: read the Auth request, answer it.
	go func() {
		stream, err := srv.AcceptTypedStream()
		if err != nil {
			return
		}
		defer stream.Close()

		var req proto.Auth
		if err := json.NewDecoder(stream).Decode(&req); err != nil {
			return
		}
		_ = json.NewEncoder(stream).Encode(proto.AuthResp{
			ClientID: "fake-client-id",
			Extra:    proto.AuthRespExtra{Cookie: respCookie},
		})
	}()

	logs := &lockedBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	sess := NewRawSession(logger, muxado.Client(clientConn, nil), nil, nil)
	defer sess.Close()

	resp, err := sess.Auth("", proto.AuthExtra{
		Authtoken: proto.ObfuscatedString(credential),
		Cookie:    reqCookie,
	})
	require.NoError(t, err)
	require.Equal(t, "fake-client-id", resp.ClientID)
	// Redaction must not reach the value the caller gets back.
	require.Equal(t, respCookie, resp.Extra.Cookie)

	out := logs.String()
	require.Contains(t, out, "encode request")
	require.Contains(t, out, "decoded response")
	require.NotContains(t, out, credential)
	require.NotContains(t, out, reqCookie)
	require.NotContains(t, out, respCookie)
}
