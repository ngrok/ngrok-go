package ngrok

import "time"

// EventType represents the type of event that occurred
type EventType int

const (
	EventTypeAgentConnectSucceeded EventType = iota
	EventTypeAgentDisconnected
	EventTypeAgentHeartbeatReceived
	EventTypeConnectionOpened
	EventTypeConnectionClosed
	EventTypeHTTPRequestComplete
	EventTypeUpstreamDialComplete
	EventTypeUpstreamTLSHandshakeComplete
)

func (t EventType) String() string {
	return [...]string{
		"AgentConnectSucceeded",
		"AgentDisconnected",
		"AgentHeartbeatReceived",
		"ConnectionOpened",
		"ConnectionClosed",
		"HTTPRequestComplete",
		"UpstreamDialComplete",
		"UpstreamTLSHandshakeComplete",
	}[t]
}

// Event is the interface implemented by all event types
type Event interface {
	EventType() EventType
	Timestamp() time.Time
}

// baseEvent provides common functionality for all events
type baseEvent struct {
	Type       EventType
	OccurredAt time.Time
}

func (e baseEvent) EventType() EventType { return e.Type }
func (e baseEvent) Timestamp() time.Time { return e.OccurredAt }

// EventHandler is the function type for event callbacks. EventHandlers must not
// block. If you would like to run operations on an event that will block or
// fail, instead write your handler to either non-blockingly push the Event onto
// a channel or spin up a goroutine.
type EventHandler func(Event)

// EventAgentConnectSucceeded is emitted when an agent successfully establishes a connection
type EventAgentConnectSucceeded struct {
	baseEvent
	Agent   Agent
	Session AgentSession
}

// EventAgentDisconnected is emitted when an agent disconnects
type EventAgentDisconnected struct {
	baseEvent
	Agent   Agent
	Session AgentSession
	Error   error
}

// EventAgentHeartbeatReceived is emitted when a heartbeat is successful
type EventAgentHeartbeatReceived struct {
	baseEvent
	Agent   Agent
	Session AgentSession
	Latency time.Duration
}

func newAgentConnectSucceeded(agent Agent, session AgentSession) *EventAgentConnectSucceeded {
	return &EventAgentConnectSucceeded{
		baseEvent: baseEvent{
			Type:       EventTypeAgentConnectSucceeded,
			OccurredAt: time.Now(),
		},
		Agent:   agent,
		Session: session,
	}
}

func newAgentDisconnected(agent Agent, session AgentSession, err error) *EventAgentDisconnected {
	return &EventAgentDisconnected{
		baseEvent: baseEvent{
			Type:       EventTypeAgentDisconnected,
			OccurredAt: time.Now(),
		},
		Agent:   agent,
		Session: session,
		Error:   err,
	}
}

func newAgentHeartbeatReceived(agent Agent, session AgentSession, latency time.Duration) *EventAgentHeartbeatReceived {
	return &EventAgentHeartbeatReceived{
		baseEvent: baseEvent{
			Type:       EventTypeAgentHeartbeatReceived,
			OccurredAt: time.Now(),
		},
		Agent:   agent,
		Session: session,
		Latency: latency,
	}
}

// EventConnectionOpened is emitted when a new connection is accepted by a forwarder
type EventConnectionOpened struct {
	baseEvent
	Endpoint   Endpoint
	RemoteAddr string
}

// EventConnectionClosed is emitted when a forwarded connection is closed
type EventConnectionClosed struct {
	baseEvent
	Endpoint   Endpoint
	RemoteAddr string
	Duration   time.Duration
	BytesIn    int64
	BytesOut   int64
}

func newConnectionOpened(endpoint Endpoint, remoteAddr string) *EventConnectionOpened {
	return &EventConnectionOpened{
		baseEvent: baseEvent{
			Type:       EventTypeConnectionOpened,
			OccurredAt: time.Now(),
		},
		Endpoint:   endpoint,
		RemoteAddr: remoteAddr,
	}
}

func newConnectionClosed(endpoint Endpoint, remoteAddr string, duration time.Duration, bytesIn, bytesOut int64) *EventConnectionClosed {
	return &EventConnectionClosed{
		baseEvent: baseEvent{
			Type:       EventTypeConnectionClosed,
			OccurredAt: time.Now(),
		},
		Endpoint:   endpoint,
		RemoteAddr: remoteAddr,
		Duration:   duration,
		BytesIn:    bytesIn,
		BytesOut:   bytesOut,
	}
}

// EventHTTPRequestComplete is emitted when an HTTP request/response cycle completes
type EventHTTPRequestComplete struct {
	baseEvent
	Endpoint   Endpoint
	Method     string
	Path       string
	StatusCode int
	Duration   time.Duration
}

func newHTTPRequestComplete(endpoint Endpoint, method, path string, statusCode int, duration time.Duration) *EventHTTPRequestComplete {
	return &EventHTTPRequestComplete{
		baseEvent: baseEvent{
			Type:       EventTypeHTTPRequestComplete,
			OccurredAt: time.Now(),
		},
		Endpoint:   endpoint,
		Method:     method,
		Path:       path,
		StatusCode: statusCode,
		Duration:   duration,
	}
}

// EventUpstreamDialComplete is emitted each time a forwarder's dialer returns while
// it connects to the upstream. Duration is the time DialContext took,
// including any name resolution. It excludes the TLS handshake. Err is the
// dialer's error, or nil.
//
// A forwarder proxies HTTP when both the endpoint URL and the upstream URL use
// http or https. It then dials only when no idle upstream connection is free,
// so it can emit fewer of these events than EventHTTPRequestComplete. Any
// other forwarder dials once for each connection it accepts.
//
// Err wraps context.Canceled if the endpoint closes during the dial, or if the
// client disconnects from a forwarder that proxies HTTP. Ignore that error
// when you count upstream failures.
type EventUpstreamDialComplete struct {
	baseEvent
	Endpoint Endpoint
	Duration time.Duration
	Err      error
}

func newUpstreamDialComplete(endpoint Endpoint, duration time.Duration, err error) *EventUpstreamDialComplete {
	return &EventUpstreamDialComplete{
		baseEvent: baseEvent{
			Type:       EventTypeUpstreamDialComplete,
			OccurredAt: time.Now(),
		},
		Endpoint: endpoint,
		Duration: duration,
		Err:      err,
	}
}

// EventUpstreamTLSHandshakeComplete is emitted each time a TLS handshake with an https
// or tls upstream finishes. It follows a successful EventUpstreamDialComplete for
// the same connection. Duration is the handshake time only. Err is the
// handshake error, such as a *tls.CertificateVerificationError, or nil. A
// forwarder that proxies HTTP reuses idle upstream connections without a new
// handshake, so it can emit fewer of these events than
// EventHTTPRequestComplete.
//
// Under TLS 1.3, the upstream checks a client certificate after the forwarder
// finishes its side of the handshake. A rejected client certificate then fails
// the connection after an event with a nil Err.
//
// Err wraps context.Canceled if the client disconnects from a forwarder that
// proxies HTTP. For any other forwarder, Err wraps net.ErrClosed if the client
// disconnects before it sends data. Ignore both errors when you count upstream
// failures.
type EventUpstreamTLSHandshakeComplete struct {
	baseEvent
	Endpoint Endpoint
	Duration time.Duration
	Err      error
}

func newUpstreamTLSHandshakeComplete(endpoint Endpoint, duration time.Duration, err error) *EventUpstreamTLSHandshakeComplete {
	return &EventUpstreamTLSHandshakeComplete{
		baseEvent: baseEvent{
			Type:       EventTypeUpstreamTLSHandshakeComplete,
			OccurredAt: time.Now(),
		},
		Endpoint: endpoint,
		Duration: duration,
		Err:      err,
	}
}
