package client

import (
	"errors"
	"testing"
	"time"

	"github.com/inconshreveable/log15/v3"

	"golang.ngrok.com/ngrok/internal/tunnel/netx"
	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

// fakeRaw is a RawSession that binds successfully, standing in for a leg whose
// region the client can reach.
type fakeRaw struct {
	listens int
}

func (f *fakeRaw) Auth(id string, extra proto.AuthExtra) (proto.AuthResp, error) {
	return proto.AuthResp{ClientID: "client"}, nil
}

func (f *fakeRaw) Listen(protocol string, opts any, extra proto.BindExtra, id string, forwardsTo string, forwardsProto string) (proto.BindResp, error) {
	f.listens++
	return proto.BindResp{ClientID: "tn_1", URL: "tcp://example:1234", Proto: protocol}, nil
}

func (f *fakeRaw) ListenLabel(labels map[string]string, metadata string, forwardsTo string, forwardsProto string) (proto.StartTunnelWithLabelResp, error) {
	return proto.StartTunnelWithLabelResp{ID: "tn_1"}, nil
}

func (f *fakeRaw) Unlisten(id string) (proto.UnbindResp, error) { return proto.UnbindResp{}, nil }
func (f *fakeRaw) Accept() (netx.LoggedConn, error)             { return nil, errors.New("not accepting") }
func (f *fakeRaw) SrvInfo() (proto.SrvInfoResp, error)          { return proto.SrvInfoResp{}, nil }
func (f *fakeRaw) Latency() <-chan time.Duration                { return nil }
func (f *fakeRaw) Heartbeat() (time.Duration, error)            { return 0, nil }
func (f *fakeRaw) Close() error                                 { return nil }

// newLeg builds a leg, connected only if raw is non-nil. A nil raw leaves the
// swapper unset, which is the state of a leg whose region never answered.
func newLeg(legNumber uint32, raw RawSession) *session {
	swapper := new(swapRaw)
	if raw != nil {
		swapper.set(raw)
	}
	return &session{
		swapper:   swapper,
		raw:       swapper,
		Logger:    newLogger(log15.New()),
		tunnels:   make(map[string]*tunnel),
		legNumber: legNumber,
	}
}

// An endpoint that bound on a reachable leg must keep serving when another leg
// never connected. Before this was fixed the unreachable leg returned
// ErrSessionNotReady and took the whole bind down, which terminated the agent.
func TestListenSucceedsWhenSecondLegNeverConnected(t *testing.T) {
	up := &fakeRaw{}
	s := &reconnectingSession{
		Logger:   log15.New(),
		sessions: []*session{newLeg(0, up), newLeg(1, nil)},
	}

	tun, err := s.Listen("tcp", nil, proto.BindExtra{}, "localhost:8080", "")
	if err != nil {
		t.Fatalf("Listen failed with one leg down: %v", err)
	}
	if tun == nil {
		t.Fatal("Listen returned no tunnel")
	}
	if up.listens != 1 {
		t.Fatalf("expected 1 bind on the reachable leg, got %d", up.listens)
	}
}

// No specific leg is required. Leg 0 being down must not deny service when a
// later leg is up.
func TestListenUsesAnyReadyLeg(t *testing.T) {
	up := &fakeRaw{}
	s := &reconnectingSession{
		Logger:   log15.New(),
		sessions: []*session{newLeg(0, nil), newLeg(1, up)},
	}

	if _, err := s.Listen("tcp", nil, proto.BindExtra{}, "localhost:8080", ""); err != nil {
		t.Fatalf("Listen failed with leg 0 down: %v", err)
	}
	if up.listens != 1 {
		t.Fatalf("expected the bind to land on leg 1, got %d binds", up.listens)
	}
}

// With no leg up at all there is nothing to serve on, so the caller still gets
// ErrSessionNotReady.
func TestListenFailsWhenNoLegReady(t *testing.T) {
	s := &reconnectingSession{
		Logger:   log15.New(),
		sessions: []*session{newLeg(0, nil), newLeg(1, nil)},
	}

	if _, err := s.Listen("tcp", nil, proto.BindExtra{}, "localhost:8080", ""); !errors.Is(err, ErrSessionNotReady) {
		t.Fatalf("expected ErrSessionNotReady, got %v", err)
	}
}

// A leg that joins late adopts the binds of a leg that is already serving,
// otherwise it sits idle holding no endpoints.
func TestLateLegAdoptsExistingBinds(t *testing.T) {
	serving := newLeg(0, &fakeRaw{})
	serving.tunnels["tn_1"] = &tunnel{}

	joining := newLeg(1, &fakeRaw{})

	s := &reconnectingSession{
		Logger:   log15.New(),
		sessions: []*session{serving, joining},
	}

	s.adoptBindsFromServingLeg(joining)

	joining.RLock()
	defer joining.RUnlock()
	if _, ok := joining.tunnels["tn_1"]; !ok {
		t.Fatalf("late leg did not adopt existing binds, has %d", len(joining.tunnels))
	}
}

// ready must mean "connected now", not "connected at some point". A leg that
// dropped and is backing off would otherwise be chosen to bind on and fail
// against a closed connection.
func TestClearedLegIsNotReady(t *testing.T) {
	leg := newLeg(0, &fakeRaw{})
	if !leg.swapper.ready() {
		t.Fatal("a connected leg should be ready")
	}

	leg.swapper.clear()
	if leg.swapper.ready() {
		t.Fatal("a leg whose connection dropped is still reported as ready")
	}

	up := &fakeRaw{}
	s := &reconnectingSession{
		Logger:   log15.New(),
		sessions: []*session{leg, newLeg(1, up)},
	}
	if _, err := s.Listen("tcp", nil, proto.BindExtra{}, "localhost:8080", ""); err != nil {
		t.Fatalf("Listen failed while leg 0 was reconnecting: %v", err)
	}
	if up.listens != 1 {
		t.Fatalf("expected the bind to skip the dropped leg, got %d binds on leg 1", up.listens)
	}
}
