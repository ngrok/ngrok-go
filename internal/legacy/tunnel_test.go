package legacy

import (
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	tunnel_client "golang.ngrok.com/ngrok/v2/internal/tunnel/client"
)

type failingAcceptTunnel struct {
	tunnel_client.Tunnel
}

func (failingAcceptTunnel) Accept() (*tunnel_client.ProxyConn, error) {
	return nil, errors.New("session closed")
}

func (failingAcceptTunnel) ID() string { return "tun_1" }

func TestAcceptFailureBeforeConnect(t *testing.T) {
	sess := new(sessionImpl)
	sess.setInner(&sessionInner{})
	tun := &tunnelImpl{Sess: sess, Tunnel: failingAcceptTunnel{}}

	var conn net.Conn
	var err error
	require.NotPanics(t, func() { conn, err = tun.Accept() })
	require.Nil(t, conn)
	var acceptErr errAcceptFailed
	require.ErrorAs(t, err, &acceptErr)
}
