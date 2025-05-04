package ngrok

import (
	"time"
)

// AgentSession represents an active connection from an Agent to the ngrok cloud
// service.
type AgentSession interface {
	// ID returns the server-assigned ID of the agent session
	// TODO(alan): implement when the server begins setting this value
	// ID() string
	// Warnings is a list of warnings returned by the ngrok cloud service after the Agent has connected
	Warnings() []error
	// Agent returns the agent that started this session
	Agent() Agent
	// StartedAt returns the time that the AgentSession was connected
	StartedAt() time.Time
}

// agentSession implements the AgentSession interface.
type agentSession struct {
	id        string
	warnings  []error
	agent     Agent
	startedAt time.Time
}

func (s *agentSession) ID() string {
	return s.id
}

func (s *agentSession) Warnings() []error {
	return s.warnings
}

func (s *agentSession) Agent() Agent {
	return s.agent
}

func (s *agentSession) StartedAt() time.Time {
	return s.startedAt
}
