package rpc

// Method constants defining standard RPC methods
const (
	StopAgentMethod    = "StopAgent"
	RestartAgentMethod = "RestartAgent"
	UpdateAgentMethod  = "UpdateAgent"
)

// Request defines an interface of RPC messages received from the ngrok cloud
// service.
type Request interface {
	// Method returns the RPC method name being called.
	Method() string
}
