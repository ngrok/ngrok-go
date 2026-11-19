package ngrok

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/inconshreveable/log15/v3"
	"github.com/stretchr/testify/require"

	tunnel_client "golang.ngrok.com/ngrok/internal/tunnel/client"
	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

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

	var metrics AgentMetricsSession = sess

	sess.setInner(&sessionInner{raw: raw})
	_, ok := metrics.AgentMetricsSettings()
	require.False(t, ok)
	_, err := metrics.SendAgentMetrics(ctx, []byte{})
	require.ErrorIs(t, err, ErrAgentMetricsUnsupported)
	require.Zero(t, raw.sent, "a connection without settings must not get a metrics stream")

	sess.setInner(&sessionInner{raw: raw, AgentMetrics: &proto.AgentMetricsSettings{IntervalMs: 30000, MaxBatchBytes: 65536, MaxSamples: 1024}})
	settings, ok := metrics.AgentMetricsSettings()
	require.True(t, ok)
	require.Equal(t, AgentMetricsSettings{Interval: 30 * time.Second, MaxBatchBytes: 65536, MaxSamples: 1024}, settings)
	res, err := metrics.SendAgentMetrics(ctx, []byte{})
	require.NoError(t, err)
	require.Equal(t, []byte("result"), res)
	require.Equal(t, 1, raw.sent)
}

func TestSendAgentMetricsBeforeConnect(t *testing.T) {
	// Connect stores an inner session with no raw connection before the
	// first connect succeeds.
	sess := new(sessionImpl)
	sess.setInner(&sessionInner{})

	_, ok := sess.AgentMetricsSettings()
	require.False(t, ok)
	_, err := sess.SendAgentMetrics(context.Background(), []byte{})
	require.ErrorIs(t, err, ErrAgentMetricsUnsupported)
}

func TestAgentMetricsSettings(t *testing.T) {
	logger := log15.New()
	logger.SetHandler(log15.DiscardHandler())
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
		"overflowing interval": with(func(s *proto.AgentMetricsSettings) { s.IntervalMs = math.MaxInt64 }),
		"overflowing samples":  with(func(s *proto.AgentMetricsSettings) { s.MaxSamples = math.MaxInt32 + 1 }),
	} {
		require.Nil(t, agentMetricsSettings(settings, logger), name)
	}

	large := with(func(s *proto.AgentMetricsSettings) { s.MaxBatchBytes = 1 << 20 })
	got := agentMetricsSettings(large, logger)
	require.Equal(t, with(func(s *proto.AgentMetricsSettings) { s.MaxBatchBytes = tunnel_client.MaxAgentMetricsBatchBytes }), got)
	require.EqualValues(t, 1<<20, large.MaxBatchBytes, "the received settings must not change")
}

// fakeTunnel is a tunnel_client.Tunnel with a fixed tunnel ID.
type fakeTunnel struct {
	tunnel_client.Tunnel
	tunnelID string
}

func (f *fakeTunnel) TunnelID() string { return f.tunnelID }

func TestTunnelID(t *testing.T) {
	tun := &tunnelImpl{Tunnel: &fakeTunnel{tunnelID: "tn_123"}}

	var asTunnel Tunnel = tun
	ider, ok := asTunnel.(TunnelIDer)
	require.True(t, ok)
	require.Equal(t, "tn_123", ider.TunnelID())

	var fwd Forwarder = &forwarder{Tunnel: tun}
	ider, ok = fwd.(TunnelIDer)
	require.True(t, ok)
	require.Equal(t, "tn_123", ider.TunnelID())

	// A Tunnel that a caller implements has no tunnel ID.
	fwd = &forwarder{Tunnel: struct{ Tunnel }{tun}}
	require.Equal(t, "", fwd.(TunnelIDer).TunnelID())
}
