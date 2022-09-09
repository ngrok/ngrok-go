package libngrok

import (
	"fmt"
	"net/url"
	"reflect"
)

type ErrContext interface {
	message() string
}
type Error[C ErrContext] struct {
	Inner   error
	Context C
}

func (e Error[C]) Unwrap() error {
	return e.Inner
}

func (e Error[C]) Error() string {
	msg := e.Context.message()
	if e.Inner != nil {
		return fmt.Sprintf("%s: %v", msg, e.Inner.Error())
	} else {
		return msg
	}
}

func (e Error[C]) Is(other error) bool {
	return reflect.TypeOf(e) == reflect.TypeOf(other)
}

type ErrAuthFailed = Error[AuthFailedContext]
type AuthFailedContext struct {
	Remote bool
}

func (c AuthFailedContext) message() string {
	if c.Remote {
		return "authentication failed"
	} else {
		return "failed to send authentication request"
	}
}

type ErrAcceptFailed = Error[AcceptContext]
type AcceptContext struct{}

func (c AcceptContext) message() string {
	return "failed to accept connection"
}

type ErrStartTunnel = Error[StartContext]
type StartContext struct {
	Config TunnelConfig
}

func (c StartContext) message() string {
	return "failed to start tunnel"
}

type ErrProxyInit = Error[ProxyInitContext]
type ProxyInitContext struct {
	URL *url.URL
}

func (c ProxyInitContext) message() string {
	return fmt.Sprintf("failed to construct proxy dialer from \"%s\"", c.URL.String())
}

type ErrSessionDial = Error[DialContext]
type DialContext struct {
	Addr string
}

func (c DialContext) message() string {
	return fmt.Sprintf("failed to dial ngrok server with address \"%s\"", c.Addr)
}
