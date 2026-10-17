package client

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/v2/internal/tunnel/proto"
)

// rebindRaw answers a bind with a new server tunnel ID, as the server does on
// every bind.
type rebindRaw struct {
	RawSession
	tunnelID string
}

func (r *rebindRaw) Listen(string, any, proto.BindExtra, string, string, string) (proto.BindResp, error) {
	return proto.BindResp{Extra: proto.BindRespExtra{TunnelID: r.tunnelID}}, nil
}

func TestReconnectTunnelToSessionTunnelID(t *testing.T) {
	cases := []struct {
		name string
		leg  uint32
		want string
	}{
		// PatchTunnelState goes to leg 0, so its ID must follow the rebind.
		{name: "leg 0 takes the new ID", leg: 0, want: "tn_new"},
		// Another leg's ID is not valid on leg 0.
		{name: "other leg keeps the leg 0 ID", leg: 1, want: "tn_old"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tun := newTunnel(proto.BindResp{ClientID: "client-id", Extra: proto.BindRespExtra{TunnelID: "tn_old"}}, proto.BindExtra{}, &session{}, "", "")
			sess := &session{raw: &rebindRaw{tunnelID: "tn_new"}, legNumber: tc.leg}

			err := (&reconnectingSession{}).reconnectTunnelToSession(sess, tun, map[string]*tunnel{}, tun.ID())

			require.NoError(t, err)
			require.Equal(t, tc.want, tun.TunnelID())
		})
	}
}
