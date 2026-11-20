package ngrok

import (
	"sync/atomic"
	"time"

	"golang.ngrok.com/ngrok/v2/internal/legacy"
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

// AgentSessionIdentity is implemented by AgentSession values that report the
// owner of the credential that authenticated the current connection to the
// ngrok cloud service. Use a type assertion to access it:
//
//	identity, ok := session.(ngrok.AgentSessionIdentity)
//
// The values are readable once Connect returns and when
// EventAgentConnectSucceeded is emitted, including from its EventHandler.
// They can change when the Agent reconnects, so read them again after each
// EventAgentConnectSucceeded.
type AgentSessionIdentity interface {
	AgentSession

	// PrincipalID returns the ID of the principal (a user or a bot user) that
	// owns the credential, in the form the ngrok API uses. It is empty if the
	// server did not send it or the credential has no owner.
	PrincipalID() string

	// AccountID returns the ID of the account that owns the credential, an
	// ac_ ID as the ngrok API uses. It is set when the credential has no
	// owner principal, and empty if the server did not send it.
	AccountID() string
}

// agentSession implements the AgentSession, AgentSessionDetails,
// AgentSessionIdentity and AgentSessionMetrics interfaces.
type agentSession struct {
	id          string
	warnings    []error
	agent       Agent
	startedAt   time.Time
	accountName string
	planName    string
	region      string
	banner      string
	// legacy holds the legacy.Session after the first connect. The connect
	// handler stores it while other goroutines can read it.
	legacy atomic.Value
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

func (s *agentSession) PrincipalID() string {
	if sess := s.legacySession(); sess != nil {
		return sess.PrincipalID()
	}
	return ""
}

func (s *agentSession) AccountID() string {
	if sess := s.legacySession(); sess != nil {
		return sess.AccountID()
	}
	return ""
}

func (s *agentSession) legacySession() legacy.Session {
	sess, _ := s.legacy.Load().(legacy.Session)
	return sess
}
