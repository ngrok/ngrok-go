package legacy

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.ngrok.com/muxado/v2"

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

func TestAuthExtraAgentID(t *testing.T) {
	id := "agt_first"
	cfg := connectConfig{ClientInfo: []clientInfo{{Type: "agent", Version: "4.0.0"}}}
	WithAgentID(func() string { return id })(&cfg)
	auth := &sessionAuth{base: newAuthExtra(&cfg, muxado.NewHeartbeatConfig()), cfg: &cfg}

	require.Equal(t, "agt_first", auth.forLeg(0).AgentID)
	require.Empty(t, auth.forLeg(0).DPoPProof)
	require.Nil(t, auth.forLeg(0).DPoPUnsupportedError)
	id = ""
	require.Empty(t, auth.forLeg(0).AgentID)
	id = "agt_second"
	require.Equal(t, "agt_second", auth.forLeg(0).AgentID)

	none := connectConfig{ClientInfo: []clientInfo{{Type: "agent", Version: "4.0.0"}}}
	require.Empty(t, (&sessionAuth{base: newAuthExtra(&none, muxado.NewHeartbeatConfig()), cfg: &none}).forLeg(0).AgentID)
}

// Legs reconnect concurrently; run with -race.
func TestAuthExtraConcurrentLegs(t *testing.T) {
	cfg := connectConfig{ClientInfo: []clientInfo{{Type: "agent", Version: "4.0.0"}}}
	WithAgentID(func() string { return "agt_123" })(&cfg)
	auth := &sessionAuth{base: newAuthExtra(&cfg, muxado.NewHeartbeatConfig()), cfg: &cfg}

	var wg sync.WaitGroup
	for leg := uint32(0); leg < 4; leg++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				a := auth.forLeg(leg)
				require.Equal(t, leg, a.LegNumber)
				require.Equal(t, "agt_123", a.AgentID)
				cookie := "cookie"
				auth.cookie.Store(&cookie)
			}
		}()
	}
	wg.Wait()
	require.Zero(t, auth.base.LegNumber)
	require.Empty(t, auth.base.Cookie)
	require.Equal(t, "cookie", auth.forLeg(1).Cookie)
}
