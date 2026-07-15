package ngrok

import (
	"time"
)

// AgentSession represents an active connection from an Agent to the ngrok cloud
// service.
type AgentSession interface {
	// ID returns the server-assigned ID of the agent session
	ID() string
	// Warnings is a list of warnings returned by the ngrok cloud service after the Agent has connected
	Warnings() []error
	// Agent returns the agent that started this session
	Agent() Agent
	// StartedAt returns the time that the AgentSession was connected
	StartedAt() time.Time
}

// AgentSessionDetails is implemented by AgentSession values that carry
// account and connection details returned by the ngrok service on connect.
// Use a type assertion to access it:
//
//	details, ok := session.(ngrok.AgentSessionDetails)
type AgentSessionDetails interface {
	AgentSession

	// AccountName returns the name of the account this agent session is connected under.
	AccountName() string

	// PlanName returns the billing plan of the account this agent session is connected under.
	PlanName() string

	// Region returns the ngrok edge region this session is connected to.
	Region() string

	// Banner returns any informational banner message sent by the ngrok service on connect.
	Banner() string
}

// agentSession implements the AgentSession and AgentSessionDetails interfaces.
type agentSession struct {
	id          string
	warnings    []error
	agent       Agent
	startedAt   time.Time
	accountName string
	planName    string
	region      string
	banner      string
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

func (s *agentSession) AccountName() string {
	return s.accountName
}

func (s *agentSession) PlanName() string {
	return s.planName
}

func (s *agentSession) Region() string {
	return s.region
}

func (s *agentSession) Banner() string {
	return s.banner
}
