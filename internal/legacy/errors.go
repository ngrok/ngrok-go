package legacy

import (
	"fmt"
	"net/url"
	"strings"
)

// Error is an error enriched with a specific ErrorCode.
// All ngrok error codes are documented at https://ngrok.com/docs/errors.
//
// An [Error] can be extracted from a generic error using [errors.As].
//
// Example:
//
//	var nerr ngrok.Error
//	if errors.As(err, &nerr) {
//	  fmt.Printf("%s: %s\n", nerr.ErrorCode(), nerr.Msg())
//	}
type Error interface {
	error
	// Msg returns the error string without the error code.
	Msg() string
	// ErrorCode returns the ngrok error code, if one exists.
	ErrorCode() string
}

// Errors arising from authentication failure.
type errAuthFailed struct {
	// Whether the error was generated by the remote server, or in the sending
	// of the authentication request.
	Remote bool
	// The underlying error.
	Inner error
}

func (e errAuthFailed) Error() string {
	var msg string
	if e.Remote {
		msg = "authentication failed"
	} else {
		msg = "failed to send authentication request"
	}

	return fmt.Sprintf("%s: %v", msg, e.Inner)
}

func (e errAuthFailed) Unwrap() error {
	return e.Inner
}

func (e errAuthFailed) Is(target error) bool {
	_, ok := target.(errAuthFailed)
	return ok
}

// The error returned by [Tunnel]'s [net.Listener.Accept] method.
type errAcceptFailed struct {
	// The underlying error.
	Inner error
}

func (e errAcceptFailed) Error() string {
	return fmt.Sprintf("failed to accept connection: %v", e.Inner)
}

func (e errAcceptFailed) Unwrap() error {
	return e.Inner
}

func (e errAcceptFailed) Is(target error) bool {
	_, ok := target.(errAcceptFailed)
	return ok
}

// Errors arising from a failure to start a tunnel.
type errListen struct {
	// The underlying error.
	Inner error
}

func (e errListen) Error() string {
	return fmt.Sprintf("failed to start tunnel: %v", e.Inner)
}

func (e errListen) Unwrap() error {
	return e.Inner
}

func (e errListen) Is(target error) bool {
	_, ok := target.(errListen)
	return ok
}

// Errors arising from a failure to construct a [golang.org/x/net/proxy.Dialer] from a [url.URL].
type errProxyInit struct {
	// The provided proxy URL.
	URL *url.URL
	// The underlying error.
	Inner error
}

func (e errProxyInit) Error() string {
	return fmt.Sprintf("failed to construct proxy dialer from \"%s\": %v", e.URL.String(), e.Inner)
}

func (e errProxyInit) Unwrap() error {
	return e.Inner
}

func (e errProxyInit) Is(target error) bool {
	_, ok := target.(errProxyInit)
	return ok
}

// Error arising from a failure to dial the ngrok server.
type errSessionDial struct {
	// The address to which a connection was attempted.
	Addr string
	// The underlying error.
	Inner error
}

func (e errSessionDial) Error() string {
	return fmt.Sprintf("failed to dial ngrok server with address \"%s\": %v", e.Addr, e.Inner)
}

func (e errSessionDial) Unwrap() error {
	return e.Inner
}

func (e errSessionDial) Is(target error) bool {
	_, ok := target.(errSessionDial)
	return ok
}

// Generic ngrok error that requires no parsing
type ngrokError struct {
	Message string
	ErrCode string
}

const errUrl = "https://ngrok.com/docs/errors"

func (m ngrokError) Error() string {
	out := m.Message
	if m.ErrCode != "" {
		out = fmt.Sprintf("%s\n\n%s/%s", out, errUrl, strings.ToLower(m.ErrCode))
	}
	return out
}

func (m ngrokError) Msg() string {
	return m.Message
}

func (m ngrokError) ErrorCode() string {
	return m.ErrCode
}

func (e ngrokError) Is(target error) bool {
	_, ok := target.(ngrokError)
	return ok
}
