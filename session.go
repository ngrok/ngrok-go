package ngrok

import (
	"slices"
	"time"
)

// AgentSession describes an active connection from an [*Agent]
// to the ngrok cloud service.
type AgentSession struct {
	// ID is the server-assigned ID of the agent session.
	ID string
	// Warnings is a list of warnings returned by the ngrok cloud service
	// after the Agent has connected.
	Warnings []error
	// Agent is the agent that started this session.
	Agent *Agent
	// StartedAt returns the time that the session was connected.
	StartedAt time.Time
}

func (sess *AgentSession) clone() *AgentSession {
	if sess == nil {
		return nil
	}
	sessionCopy := new(AgentSession)
	*sessionCopy = *sess
	sessionCopy.Warnings = slices.Clone(sess.Warnings)
	return sessionCopy
}
