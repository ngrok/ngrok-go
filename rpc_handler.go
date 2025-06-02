package ngrok

import (
	"context"

	"golang.ngrok.com/ngrok/v2/rpc"
)

// RPCHandler is a function that processes RPC requests from the ngrok service.
// It receives the context, agent session, and request, and returns an optional
// response payload and error.
type RPCHandler func(context.Context, AgentSession, rpc.Request) ([]byte, error)

// Private request implementation that satisfies the rpc.Request interface
type rpcRequest struct {
	method  string
	payload []byte
}

func (r *rpcRequest) Method() string { return r.method }
