package ngrok

import (
	"fmt"
	"net/url"
)

type ErrAuthFailed struct {
	Remote bool
	Inner  error
}

func (e ErrAuthFailed) Error() string {
	var msg string
	if e.Remote {
		msg = "authentication failed"
	} else {
		msg = "failed to send authentication request"
	}

	return fmt.Sprintf("%s: %v", msg, e.Inner)
}

func (e ErrAuthFailed) Unwrap() error {
	return e.Inner
}

func (e ErrAuthFailed) Is(target error) bool {
	_, ok := target.(ErrAuthFailed)
	return ok
}

type ErrAcceptFailed struct {
	Inner error
}

func (e ErrAcceptFailed) Error() string {
	return fmt.Sprintf("failed to accept connection: %v", e.Inner)
}

func (e ErrAcceptFailed) Unwrap() error {
	return e.Inner
}

func (e ErrAcceptFailed) Is(target error) bool {
	_, ok := target.(ErrAcceptFailed)
	return ok
}

type ErrStartTunnel struct {
	Config TunnelConfig
	Inner  error
}

func (e ErrStartTunnel) Error() string {
	return fmt.Sprintf("failed to start tunnel: %v", e.Inner)
}

func (e ErrStartTunnel) Unwrap() error {
	return e.Inner
}

func (e ErrStartTunnel) Is(target error) bool {
	_, ok := target.(ErrStartTunnel)
	return ok
}

type ErrProxyInit struct {
	URL   *url.URL
	Inner error
}

func (e ErrProxyInit) Error() string {
	return fmt.Sprintf("failed to construct proxy dialer from \"%s\": %v", e.URL.String(), e.Inner)
}

func (e ErrProxyInit) Unwrap() error {
	return e.Inner
}

func (e ErrProxyInit) Is(target error) bool {
	_, ok := target.(ErrProxyInit)
	return ok
}

type ErrSessionDial struct {
	Addr  string
	Inner error
}

func (e ErrSessionDial) Error() string {
	return fmt.Sprintf("failed to dial ngrok server with address \"%s\": %v", e.Addr, e.Inner)
}

func (e ErrSessionDial) Unwrap() error {
	return e.Inner
}

func (e ErrSessionDial) Is(target error) bool {
	_, ok := target.(ErrSessionDial)
	return ok
}
