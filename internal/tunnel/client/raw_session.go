package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"sync"
	"time"

	"golang.ngrok.com/muxado/v2"
	"golang.ngrok.com/ngrok/internal/tunnel/netx"
	"golang.ngrok.com/ngrok/internal/tunnel/proto"

	log "github.com/inconshreveable/log15/v3"
	logext "github.com/inconshreveable/log15/v3/ext"
)

type RawSession interface {
	Auth(id string, extra proto.AuthExtra) (proto.AuthResp, error)
	Listen(proto string, opts any, extra proto.BindExtra, id string, forwardsTo string, forwardsProto string) (proto.BindResp, error)
	ListenLabel(labels map[string]string, metadata string, forwardsTo string, forwardsProto string) (proto.StartTunnelWithLabelResp, error)
	Unlisten(id string) (proto.UnbindResp, error)
	Accept() (netx.LoggedConn, error)

	SrvInfo() (proto.SrvInfoResp, error)

	Latency() <-chan time.Duration
	Heartbeat() (time.Duration, error)

	Close() error
}

type HandlerRespFunc func(v any) error
type SessionHandler interface {
	OnStop(*proto.Stop, HandlerRespFunc)
	OnRestart(*proto.Restart, HandlerRespFunc)
	OnUpdate(*proto.Update, HandlerRespFunc)
	OnStopTunnel(*proto.StopTunnel, HandlerRespFunc)
}

// A RawSession is a client session which handles authorization with the tunnel
// server, then listening and unlistening of tunnels.
//
// When RawSession.Accept() returns an error, that means the session is dead.
// Client sessions run over a muxado session.
type rawSession struct {
	mux        *muxado.Heartbeat // the muxado session we're multiplexing streams over
	id         string            // session id for logging purposes
	handler    SessionHandler    // callbacks to allow the application to handle requests from the server
	latency    chan time.Duration
	closed     bool
	closedLock sync.RWMutex
	log.Logger
	remoteAddr net.Addr
}

// Creates a new client tunnel session with the given id
// running over the given muxado session.
func NewRawSession(logger log.Logger, mux muxado.Session, heartbeatConfig *muxado.HeartbeatConfig, handler SessionHandler) RawSession {
	return newRawSession(mux, newLogger(logger), heartbeatConfig, handler)
}

func newRawSession(mux muxado.Session, logger log.Logger, heartbeatConfig *muxado.HeartbeatConfig, handler SessionHandler) RawSession {
	s := &rawSession{Logger: logger, handler: handler, latency: make(chan time.Duration), remoteAddr: mux.RemoteAddr()}
	typed := muxado.NewTypedStreamSession(mux)
	heart := muxado.NewHeartbeat(typed, s.onHeartbeat, heartbeatConfig)
	s.mux = heart
	heart.Start()
	return s
}

// Auth sends an authentication message to the server and returns the server's response.
// The id string will be empty unless reconnecting an existing session.
// extra is an opaque struct useful for passing application-specific data.
func (s *rawSession) Auth(id string, extra proto.AuthExtra) (resp proto.AuthResp, err error) {
	req := proto.Auth{
		ClientID: id,
		Extra:    extra,
		Version:  proto.Version,
	}
	if err = s.rpc(proto.AuthReq, &req, &resp); err != nil {
		return
	}

	// set client id / log tag only if it changed
	if s.id != resp.ClientID {
		s.id = resp.ClientID
		s.Logger = s.Logger.New("clientid", s.id)
	}
	return
}

// Listen sends a listen message to the server and returns the server's response
// protocol is the requested protocol to listen.
// opts are protocol-specific options for listening.
// extra is an opaque struct useful for passing application-specific data.
// id is an session-unique identifier, if empty it will be assigned for you
func (s *rawSession) Listen(protocol string, opts any, extra proto.BindExtra, id string, forwardsTo string, forwardsProto string) (resp proto.BindResp, err error) {
	req := proto.Bind{
		ClientID:      id,
		Proto:         protocol,
		Opts:          opts,
		Extra:         extra,
		ForwardsTo:    forwardsTo,
		ForwardsProto: forwardsProto,
	}
	err = s.rpc(proto.BindReq, &req, &resp)
	if err != nil {
		return
	}
	// proto opts are only set if there was no error
	if resp.Error == "" {
		err = proto.UnpackProtoOpts(resp.Proto, resp.Opts, &resp)
	}
	return
}

// ListenLabel sends a listen message to the server and returns the server's response
func (s *rawSession) ListenLabel(labels map[string]string, metadata string, forwardsTo string, forwardsProto string) (resp proto.StartTunnelWithLabelResp, err error) {
	req := proto.StartTunnelWithLabel{
		Labels:        labels,
		Metadata:      metadata,
		ForwardsTo:    forwardsTo,
		ForwardsProto: forwardsProto,
	}
	err = s.rpc(proto.StartTunnelWithLabelReq, &req, &resp)
	return
}

// Unlisten sends an unlisten message to the server and returns the server's
// response. id is the bind id returned as part of the BindResp from a Listen
// call
func (s *rawSession) Unlisten(id string) (resp proto.UnbindResp, err error) {
	req := proto.Unbind{ClientID: id}
	err = s.rpc(proto.UnbindReq, &req, &resp)
	return
}

func (s *rawSession) SrvInfo() (resp proto.SrvInfoResp, err error) {
	req := proto.SrvInfo{}
	err = s.rpc(proto.SrvInfoReq, &req, &resp)
	return
}

func (s *rawSession) Heartbeat() (time.Duration, error) {
	if latency, ok := s.mux.Beat(); !ok {
		return 0, errors.New("remote failed to reply to heatbeat")
	} else {
		return latency, nil
	}
}

func (s *rawSession) Latency() <-chan time.Duration {
	return s.latency
}

// Accept returns the next stream initiated by the server over the underlying muxado session
func (s *rawSession) Accept() (netx.LoggedConn, error) {
	for {
		raw, err := s.mux.AcceptTypedStream()
		if err != nil {
			return nil, err
		}

		reqType := proto.ReqType(raw.StreamType())
		s.Debug("tunnel Accept", "reqType", reqType, "remoteAddr", s.remoteAddr)
		deserialize := func(v any) (ok bool) {
			if err := json.NewDecoder(raw).Decode(v); err != nil {
				s.Error("failed to deserialize", "type", reflect.TypeOf(v), "err", err)

				// we're abusing the fact that all error responses have the same type
				var errResp struct {
					Error string
				}
				errResp.Error = fmt.Sprintf("Failed to deserialize request payload: %v", err)

				buf, err := json.Marshal(&errResp)
				if err != nil {
					s.Error("failed to encode response", "err", err)
					return
				}
				if _, err := raw.Write(buf); err != nil {
					s.Warn("failed to write error response", "err", err)
					return
				}
				return false
			}
			return true
		}

		respFunc := s.respFunc(raw)
		switch reqType {
		case proto.RestartReq:
			var req proto.Restart
			if deserialize(&req) {
				go s.handler.OnRestart(&req, respFunc)
			}
		case proto.StopReq:
			var req proto.Stop
			if deserialize(&req) {
				go s.handler.OnStop(&req, respFunc)
			}
		case proto.UpdateReq:
			var req proto.Update
			if deserialize(&req) {
				go s.handler.OnUpdate(&req, respFunc)
			}
		case proto.StopTunnelReq:
			var req proto.StopTunnel
			if deserialize(&req) {
				go s.handler.OnStopTunnel(&req, respFunc)
			}
		default:
			return netx.NewLoggedConn(s.Logger, raw, "type", "proxy", "sess", s.id), nil
		}
	}
}

func (s *rawSession) respFunc(raw net.Conn) func(v any) error {
	return func(v any) error {
		buf, err := json.Marshal(v)
		if err != nil {
			s.Error("failed to write response", "err", err)
			return err
		}
		if _, err = raw.Write(buf); err != nil {
			return err
		}
		return nil
	}
}

func (s *rawSession) Close() error {
	// Close the muxado heartbeat session. After this, the goroutine calling the
	// callback handler should exit.
	err := s.mux.Close()

	// Prevent sending on a closed channel in the callback handler by ensuring
	// exclusive access to the channel and the closed boolean here.
	s.closedLock.Lock()
	defer s.closedLock.Unlock()
	if !s.closed {
		s.closed = true
		close(s.latency)
	}
	return err
}

// This is essentially the RPC protocol. The request and response are just JSON
// payloads serialized over a new stream. The stream is opened with a request
// type which allows the remote side to know in advance what type of payload to
// deserialize.
func (s *rawSession) rpc(reqtype proto.ReqType, req any, resp any) error {
	l := s.New("reqtype", reqtype)

	stream, err := s.mux.OpenTypedStream(muxado.StreamType(reqtype))
	l.Debug("open stream", "err", err)
	if err != nil {
		return err
	}
	defer stream.Close()

	enc := json.NewEncoder(stream)
	err = enc.Encode(req)
	s.Debug("encode request", "sid", stream.Id(), "req", req, "err", err)
	if err != nil {
		return err
	}

	dec := json.NewDecoder(stream)
	err = dec.Decode(resp)
	s.Debug("decoded response", "sid", stream.Id(), "resp", resp, "err", err)
	if err != nil {
		return err
	}

	return nil
}

func (s *rawSession) onHeartbeat(pingTime time.Duration, timeout bool) {
	if timeout {
		s.Error("heartbeat timeout, terminating session")
		s.Close()
		return
	}

	// make sure we don't send on a closed channel.
	// Any number of `onHeartbeat` callbacks can be in flight at a given time,
	// but only one Close.
	s.closedLock.RLock()
	defer s.closedLock.RUnlock()

	if s.closed {
		return
	}

	s.Debug("heartbeat received", "latency_ms", int(pingTime.Milliseconds()))
	select {
	case s.latency <- pingTime:
	default:
	}
}

func newLogger(parent log.Logger) log.Logger {
	return parent.New("obj", "csess", "id", logext.RandId(6))
}
