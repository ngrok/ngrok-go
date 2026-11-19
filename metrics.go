package ngrok

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/inconshreveable/log15/v3"

	tunnel_client "golang.ngrok.com/ngrok/internal/tunnel/client"
	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

// AgentMetricsSession is implemented by [Session] values that can publish
// agent metrics over the connection to the ngrok cloud service. It is not part
// of [Session], so that existing implementations of [Session] still compile.
// Use a type assertion to access it:
//
//	metrics, ok := session.(ngrok.AgentMetricsSession)
type AgentMetricsSession interface {
	// AgentMetricsSettings returns the agent metrics settings of the current
	// connection. It returns false if the connection does not accept metrics.
	// The settings can change when the session reconnects, so read them again
	// after each call of the handler set with [WithConnectHandler].
	AgentMetricsSettings() (AgentMetricsSettings, bool)

	// SendAgentMetrics sends one encoded agent.AgentMetricsBatch protobuf on a
	// new stream of the current connection and returns the encoded
	// agent.AgentMetricsBatchResult. The caller owns both encodings.
	//
	// Send at most one batch at a time. ctx must have a deadline. The deadline
	// and cancellation end the wait for the result. On a stalled connection,
	// sending the batch can last until the connection recovers or the session
	// closes, for example after missed heartbeats or a call to Close. Tunnel
	// traffic does not wait for it. Call SendAgentMetrics from its own
	// goroutine, and never from a connect, disconnect or heartbeat handler.
	// It returns [ErrAgentMetricsUnsupported] if the connection does not
	// accept metrics. Any other error means that the result is unknown.
	SendAgentMetrics(ctx context.Context, batch []byte) ([]byte, error)
}

// ErrAgentMetricsUnsupported is returned by
// [AgentMetricsSession].SendAgentMetrics when the current connection to the
// ngrok cloud service does not accept agent metrics.
var ErrAgentMetricsUnsupported = errors.New("the ngrok session does not accept agent metrics")

// AgentMetricsSettings tells an agent how to publish metrics batches on the
// current connection to the ngrok cloud service. All fields are above zero.
type AgentMetricsSettings struct {
	// Interval is the time between batches.
	Interval time.Duration
	// MaxBatchBytes is the maximum encoded size of a batch. It is never more
	// than the largest batch that SendAgentMetrics can send.
	MaxBatchBytes int
	// MaxSamples is the maximum number of samples in a batch.
	MaxSamples int
}

// compile-time check that the session implements the optional interface
var _ AgentMetricsSession = (*sessionImpl)(nil)

// agentMetricsSettings returns nil for settings that the agent cannot follow,
// so that the session sends no metrics. It lowers MaxBatchBytes to the
// largest batch that SendAgentMetrics can send.
func agentMetricsSettings(settings *proto.AgentMetricsSettings, logger log15.Logger) *proto.AgentMetricsSettings {
	if settings == nil {
		return nil
	}
	// The upper bounds keep the conversions in AgentMetricsSettings from
	// overflowing time.Duration or a 32-bit int into a value at or below zero.
	if settings.IntervalMs <= 0 || settings.IntervalMs > math.MaxInt64/int64(time.Millisecond) ||
		settings.MaxBatchBytes <= 0 ||
		settings.MaxSamples <= 0 || settings.MaxSamples > math.MaxInt32 {
		logger.Warn("ignoring invalid agent metrics settings",
			"interval_ms", settings.IntervalMs,
			"max_batch_bytes", settings.MaxBatchBytes,
			"max_samples", settings.MaxSamples)
		return nil
	}
	if settings.MaxBatchBytes > tunnel_client.MaxAgentMetricsBatchBytes {
		lowered := *settings
		lowered.MaxBatchBytes = tunnel_client.MaxAgentMetricsBatchBytes
		return &lowered
	}
	return settings
}

func (s *sessionImpl) AgentMetricsSettings() (AgentMetricsSettings, bool) {
	settings := s.inner().AgentMetrics
	if settings == nil {
		return AgentMetricsSettings{}, false
	}
	return AgentMetricsSettings{
		Interval:      time.Duration(settings.IntervalMs) * time.Millisecond,
		MaxBatchBytes: int(settings.MaxBatchBytes),
		MaxSamples:    int(settings.MaxSamples),
	}, true
}

func (s *sessionImpl) SendAgentMetrics(ctx context.Context, batch []byte) ([]byte, error) {
	inner := s.inner()
	if inner.AgentMetrics == nil || inner.raw == nil {
		return nil, ErrAgentMetricsUnsupported
	}
	return inner.raw.SendAgentMetrics(ctx, batch)
}
