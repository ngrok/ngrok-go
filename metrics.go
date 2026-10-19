package ngrok

import (
	"context"
	"time"

	"golang.ngrok.com/ngrok/v2/internal/legacy"
)

// AgentSessionMetrics is implemented by AgentSession values that can publish
// agent metrics over the connection to the ngrok cloud service. Use a type
// assertion to access it:
//
//	metrics, ok := session.(ngrok.AgentSessionMetrics)
type AgentSessionMetrics interface {
	AgentSession

	// MetricsSettings returns the agent metrics settings of the current
	// connection. It returns false if the connection does not accept metrics.
	// The settings can change when the Agent reconnects, so read them again
	// after each EventAgentConnectSucceeded.
	MetricsSettings() (MetricsSettings, bool)

	// SendMetrics sends one encoded agent.AgentMetricsBatch protobuf on a new
	// stream of the current connection and returns the encoded
	// agent.AgentMetricsBatchResult. The caller owns both encodings.
	//
	// Send at most one batch at a time. ctx must have a deadline. The deadline
	// and cancellation end the wait for the result. On a stalled connection,
	// sending the batch can last until the connection recovers or the Agent
	// closes the session, for example after missed heartbeats or a call to
	// Disconnect. Tunnel traffic does not wait for it. Call SendMetrics from
	// its own goroutine, and never from an EventHandler. It returns
	// ErrMetricsUnsupported if the connection does not accept metrics. Any
	// other error means that the result is unknown.
	SendMetrics(ctx context.Context, batch []byte) ([]byte, error)
}

// ErrMetricsUnsupported is returned by AgentSessionMetrics.SendMetrics when
// the current connection to the ngrok cloud service does not accept agent
// metrics.
var ErrMetricsUnsupported = legacy.ErrAgentMetricsUnsupported

// MetricsSettings tells an agent how to publish metrics batches on the current
// connection to the ngrok cloud service. All fields are above zero.
type MetricsSettings struct {
	// Interval is the time between batches.
	Interval time.Duration
	// MaxBatchBytes is the maximum encoded size of a batch. It is never more
	// than the largest batch that SendMetrics can send.
	MaxBatchBytes int
	// MaxSamples is the maximum number of samples in a batch.
	MaxSamples int
}

func (s *agentSession) MetricsSettings() (MetricsSettings, bool) {
	sess := s.legacySession()
	if sess == nil {
		return MetricsSettings{}, false
	}
	settings := sess.AgentMetricsSettings()
	if settings == nil {
		return MetricsSettings{}, false
	}
	return MetricsSettings{
		Interval:      time.Duration(settings.IntervalMs) * time.Millisecond,
		MaxBatchBytes: int(settings.MaxBatchBytes),
		MaxSamples:    int(settings.MaxSamples),
	}, true
}

func (s *agentSession) SendMetrics(ctx context.Context, batch []byte) ([]byte, error) {
	sess := s.legacySession()
	if sess == nil {
		return nil, ErrMetricsUnsupported
	}
	return sess.SendAgentMetrics(ctx, batch)
}
