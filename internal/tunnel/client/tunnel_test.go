package client

import (
	"encoding/json"
	"net"
	"testing"

	"github.com/inconshreveable/log15/v3"
	"github.com/stretchr/testify/require"

	"golang.ngrok.com/muxado/v2"
	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

// bindTunnel binds an https tunnel on a session whose server answers the
// bind with bindResp, a JSON BindResp as the server writes it.
func bindTunnel(t *testing.T, bindResp string) Tunnel {
	clientConn, serverConn := net.Pipe()
	sess := NewSession(log15.New(), muxado.Client(clientConn, nil), nil, nil)
	t.Cleanup(func() { sess.Close() })

	srv := muxado.NewTypedStreamSession(muxado.Server(serverConn, nil))
	t.Cleanup(func() { srv.Close() })
	go func() {
		for {
			stream, err := srv.AcceptTypedStream()
			if err != nil {
				return
			}
			go func() {
				defer stream.Close()
				if stream.StreamType() != muxado.StreamType(proto.BindReq) {
					return
				}
				var bind proto.Bind
				if err := json.NewDecoder(stream).Decode(&bind); err != nil {
					return
				}
				_, _ = stream.Write([]byte(bindResp))
			}()
		}
	}()

	tun, err := sess.Listen("https", &proto.HTTPEndpoint{}, proto.BindExtra{}, "", "")
	require.NoError(t, err)
	return tun
}

func TestBindTunnelID(t *testing.T) {
	t.Run("the bind response sets the tunnel ID", func(t *testing.T) {
		tun := bindTunnel(t, `{"Id":"bind1","URL":"https://a.ngrok.app","Proto":"https","Extra":{"Token":"tok","TunnelID":"tn_123"}}`)
		require.Equal(t, "tn_123", tun.TunnelID())
		require.Equal(t, "bind1", tun.ID())
	})

	t.Run("a server that sends no tunnel ID gives an empty ID", func(t *testing.T) {
		tun := bindTunnel(t, `{"Id":"bind1","URL":"https://a.ngrok.app","Proto":"https","Extra":{"Token":"tok"}}`)
		require.Equal(t, "", tun.TunnelID())
	})
}
