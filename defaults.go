package ngrok

import (
	"context"
	"os"
)

// A default Agent instance to use when you don't need a custom one.
var DefaultAgent, _ = NewAgent(
	WithAuthtoken(os.Getenv("NGROK_AUTHTOKEN")),
)

// Listen is equivalent to DefaultAgent.Listen().
func Listen(ctx context.Context, opts ...EndpointOption) (EndpointListener, error) {
	return DefaultAgent.Listen(ctx, opts...)
}

// Forward is sugar for DefaultAgent.Forward().
func Forward(ctx context.Context, upstream *Upstream, opts ...EndpointOption) (EndpointForwarder, error) {
	return DefaultAgent.Forward(ctx, upstream, opts...)
}
