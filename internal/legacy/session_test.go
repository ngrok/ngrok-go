package legacy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	tunnel_client "golang.ngrok.com/ngrok/v2/internal/tunnel/client"
	"golang.ngrok.com/ngrok/v2/internal/tunnel/proto"
)

func TestUserAgent(t *testing.T) {
	s := (&clientInfo{Type: "library/official/go", Version: "1.2.3"}).ToUserAgent()
	require.Equal(t, "library-official-go/1.2.3", s)

	s = (&clientInfo{Type: "some@funky☺user agent", Version: "№1.2.3"}).ToUserAgent()
	require.Equal(t, "some#funky#user#agent/#1.2.3", s)

	s = (&clientInfo{
		Type:     "agent/official/go",
		Version:  "3.2.1",
		Comments: []string{"{\"ProxyType\": \"socks5\", \"ConfigVersion\": \"2\"}"},
	}).ToUserAgent()
	require.Equal(t, "agent-official-go/3.2.1 ({\"ProxyType\": \"socks5\", \"ConfigVersion\": \"2\"})", s)
}

// metricsRaw is a RawSession that answers every metrics batch.
type metricsRaw struct {
	tunnel_client.RawSession
	sent int
}

func (r *metricsRaw) SendAgentMetrics(context.Context, []byte) ([]byte, error) {
	r.sent++
	return []byte("result"), nil
}

func TestSendAgentMetricsRequiresSettings(t *testing.T) {
	ctx := context.Background()
	raw := &metricsRaw{}
	sess := new(sessionImpl)

	sess.setInner(&sessionInner{raw: raw})
	_, err := sess.SendAgentMetrics(ctx, []byte{})
	require.ErrorIs(t, err, ErrAgentMetricsUnsupported)
	require.Zero(t, raw.sent, "a connection without settings must not get a metrics stream")

	sess.setInner(&sessionInner{raw: raw, AgentMetrics: &proto.AgentMetricsSettings{IntervalMs: 30000}})
	res, err := sess.SendAgentMetrics(ctx, []byte{})
	require.NoError(t, err)
	require.Equal(t, []byte("result"), res)
	require.Equal(t, 1, raw.sent)
}
