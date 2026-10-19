package ngrok

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/v2/internal/legacy"
	"golang.ngrok.com/ngrok/v2/internal/tunnel/proto"
)

type metricsLegacySession struct {
	legacy.Session
}

func (metricsLegacySession) AgentMetricsSettings() *proto.AgentMetricsSettings {
	return &proto.AgentMetricsSettings{IntervalMs: 30000, MaxBatchBytes: 65536, MaxSamples: 1024}
}

func TestAgentSessionMetrics(t *testing.T) {
	impl := &agentSession{}
	sess, ok := AgentSession(impl).(AgentSessionMetrics)
	require.True(t, ok, "AgentSession must implement AgentSessionMetrics")

	_, ok = sess.MetricsSettings()
	require.False(t, ok, "no settings before the first connect")
	_, err := sess.SendMetrics(context.Background(), []byte{})
	require.ErrorIs(t, err, ErrMetricsUnsupported)

	impl.legacy.Store(legacy.Session(metricsLegacySession{}))

	settings, ok := sess.MetricsSettings()
	require.True(t, ok)
	require.Equal(t, MetricsSettings{Interval: 30 * time.Second, MaxBatchBytes: 65536, MaxSamples: 1024}, settings)
}
