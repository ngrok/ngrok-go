package ngrok

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestEventTypeString(t *testing.T) {
	tests := []struct {
		eventType EventType
		expected  string
	}{
		{EventTypeAgentConnectSucceeded, "AgentConnectSucceeded"},
		{EventTypeAgentDisconnected, "AgentDisconnected"},
		{EventTypeAgentHeartbeatReceived, "AgentHeartbeatReceived"},
	}

	for _, test := range tests {
		assert.Equal(t, test.expected, test.eventType.String())
	}
}

func TestBaseEvent(t *testing.T) {
	now := time.Now()
	be := baseEvent{
		Type:       EventTypeAgentConnectSucceeded,
		OccurredAt: now,
	}

	assert.Equal(t, EventTypeAgentConnectSucceeded, be.EventType())
	assert.Equal(t, now, be.Timestamp())
}

func TestEventCreation(t *testing.T) {
	// Create a mock agent and session for testing
	agent := &agent{}
	session := &agentSession{}

	// Test EventAgentConnectSucceeded creation
	connectEvent := newAgentConnectSucceeded(agent, session)
	assert.Equal(t, EventTypeAgentConnectSucceeded, connectEvent.EventType())
	assert.NotZero(t, connectEvent.Timestamp())
	assert.Equal(t, agent, connectEvent.Agent)
	assert.Equal(t, session, connectEvent.Session)

	// Test EventAgentDisconnected creation
	expectedErr := assert.AnError
	disconnectEvent := newAgentDisconnected(agent, session, expectedErr)
	assert.Equal(t, EventTypeAgentDisconnected, disconnectEvent.EventType())
	assert.NotZero(t, disconnectEvent.Timestamp())
	assert.Equal(t, agent, disconnectEvent.Agent)
	assert.Equal(t, session, disconnectEvent.Session)
	assert.Equal(t, expectedErr, disconnectEvent.Error)

	// Test EventAgentHeartbeatReceived creation
	heartbeatEvent := newAgentHeartbeatReceived(agent, session, 100*time.Millisecond)
	assert.Equal(t, EventTypeAgentHeartbeatReceived, heartbeatEvent.EventType())
	assert.NotZero(t, heartbeatEvent.Timestamp())
	assert.Equal(t, agent, heartbeatEvent.Agent)
	assert.Equal(t, session, heartbeatEvent.Session)
	assert.Equal(t, 100*time.Millisecond, heartbeatEvent.Latency)
}

func ExampleEventHandler() {
	// Define an event handler that logs known event types. For unknown events,
	// it logs a warning message with the event type.
	// This is useful for debugging and understanding the flow of events.
	// Note: the pointer to event types is used when using a type switch.
	var handler EventHandler = func(e Event) {
		switch v := e.(type) {
		case *EventAgentHeartbeatReceived:
			slog.Info("ngrok agent heartbeat received")
		case *EventAgentConnectSucceeded:
			slog.Info("ngrok agent connected")
		case *EventAgentDisconnected:
			slog.Error("ngrok agent disconnected", "error", v.Error)
		default:
			slog.Warn("Received unknown event", "type", e.EventType())
		}
	}

	agent, err := NewAgent(WithEventHandler(handler))
	if err != nil {
		slog.Error("Failed to create ngrok agent", "error", err)
		return
	}

	_ = agent.Connect(context.Background())
}

func ExampleEventHandler_withChannel() {
	// Create a buffered channel to receive events.
	eventChan := make(chan Event, 10)

	// Start a goroutine to handle events from the channel.
	go func() {
		for e := range eventChan {
			switch v := e.(type) {
			case *EventAgentHeartbeatReceived:
				slog.Info("ngrok agent heartbeat received", "latency", v.Latency)
				// Some long potentially blocking operation here
			case *EventAgentConnectSucceeded:
				slog.Info("ngrok agent connected", "agent", v.Agent, "session", v.Session)
				// Some long potentially blocking operation here
			case *EventAgentDisconnected:
				slog.Error("ngrok agent disconnected", "error", v.Error, "agent", v.Agent, "session", v.Session)
				// Some long potentially blocking operation here
			default:
				slog.Warn("Received unknown event", "type", e.EventType())
			}
		}
	}()

	// The event handler will send events to the channel, if the channel is full,
	// it will log a warning and drop the event to prevent blocking the agent's event processing.
	var handler EventHandler = func(e Event) {
		select {
		case eventChan <- e:
		default:
			slog.Warn("Event channel is full, dropping event", "type", e.EventType())
		}
	}

	agent, err := NewAgent(WithEventHandler(handler))
	if err != nil {
		slog.Error("Failed to create ngrok agent", "error", err)
		return
	}

	_ = agent.Connect(context.Background())
}
