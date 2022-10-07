package ngrok

import "github.com/ngrok/ngrok-go/internal/tunnel/proto"

type tunnelConfigPrivate interface {
	ForwardsTo() string
	Extra() proto.BindExtra
	Proto() string
	Opts() any
	Labels() map[string]string
}
