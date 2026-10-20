package legacy

import (
	"context"
	"io"
	"log/slog"
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

func TestAgentMetricsSettings(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	valid := proto.AgentMetricsSettings{IntervalMs: 30000, MaxBatchBytes: 65536, MaxSamples: 1024}
	with := func(change func(*proto.AgentMetricsSettings)) *proto.AgentMetricsSettings {
		s := valid
		change(&s)
		return &s
	}

	require.Nil(t, agentMetricsSettings(nil, logger))
	require.Equal(t, &valid, agentMetricsSettings(&valid, logger))
	for name, settings := range map[string]*proto.AgentMetricsSettings{
		"zero interval":        with(func(s *proto.AgentMetricsSettings) { s.IntervalMs = 0 }),
		"negative batch bytes": with(func(s *proto.AgentMetricsSettings) { s.MaxBatchBytes = -1 }),
		"zero samples":         with(func(s *proto.AgentMetricsSettings) { s.MaxSamples = 0 }),
	} {
		require.Nil(t, agentMetricsSettings(settings, logger), name)
	}

	large := with(func(s *proto.AgentMetricsSettings) { s.MaxBatchBytes = 1 << 20 })
	got := agentMetricsSettings(large, logger)
	require.Equal(t, with(func(s *proto.AgentMetricsSettings) { s.MaxBatchBytes = tunnel_client.MaxAgentMetricsBatchBytes }), got)
	require.EqualValues(t, 1<<20, large.MaxBatchBytes, "the received settings must not change")
}
