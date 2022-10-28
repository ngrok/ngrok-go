package ngrok

import "golang.ngrok.com/ngrok/internal/tunnel/proto"

type tunnelConfigPrivate interface {
	ForwardsTo() string
	Extra() proto.BindExtra
	Proto() string
	Opts() any
	Labels() map[string]string
}
