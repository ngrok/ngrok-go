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
)

func (t EventType) String() string {
	return [...]string{
		"AgentConnectSucceeded",
		"AgentDisconnected",
		"AgentHeartbeatReceived",
		"ConnectionOpened",
		"ConnectionClosed",
		"HTTPRequestComplete",
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

// newAgentConnectSucceeded creates a new EventAgentConnectSucceeded event
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

// newAgentDisconnected creates a new EventAgentDisconnected event
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

// newAgentHeartbeatReceived creates a new EventAgentHeartbeatReceived event
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

// newConnectionOpened creates a new EventConnectionOpened event
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

// newConnectionClosed creates a new EventConnectionClosed event
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
