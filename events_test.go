package ngrok

import (
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
