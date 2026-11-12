package client

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"golang.ngrok.com/ngrok/v2/internal/tunnel/netx"
	"golang.ngrok.com/ngrok/v2/internal/tunnel/proto"
)

// fakeRaw is a RawSession that binds successfully, standing in for a leg whose
// region the client can reach.
type fakeRaw struct {
	listens   int
	unlistens []string
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

func (f *fakeRaw) Unlisten(id string) (proto.UnbindResp, error) {
	f.unlistens = append(f.unlistens, id)
	return proto.UnbindResp{}, nil
}
func (f *fakeRaw) Accept() (netx.LoggedConn, error) { return nil, errors.New("not accepting") }

func (f *fakeRaw) PatchTunnelState(id string, name, description, metadata *string, poolingEnabled *bool, trafficPolicy *string) (proto.PatchTunnelStateResp, error) {
	return proto.PatchTunnelStateResp{}, nil
}

func (f *fakeRaw) SendAgentMetrics(ctx context.Context, batch []byte) ([]byte, error) {
	return nil, nil
}
func (f *fakeRaw) SrvInfo() (proto.SrvInfoResp, error) { return proto.SrvInfoResp{}, nil }
func (f *fakeRaw) Latency() <-chan time.Duration       { return nil }
func (f *fakeRaw) Heartbeat() (time.Duration, error)   { return 0, nil }
func (f *fakeRaw) Close() error                        { return nil }

// newLeg builds a leg, connected only if raw is non-nil. A nil raw leaves the
// swapper unset, which is the state of a leg whose region never answered.
func newLeg(legNumber uint32, raw RawSession) *session {
	swapper := new(swapRaw)
	if raw != nil {
		swapper.set(raw)
		swapper.serving()
	}
	return &session{
		swapper:   swapper,
		raw:       swapper,
		Logger:    newLogger(slog.Default()),
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
		Logger:   slog.Default(),
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
		Logger:   slog.Default(),
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
		Logger:   slog.Default(),
		sessions: []*session{newLeg(0, nil), newLeg(1, nil)},
	}

	if _, err := s.Listen("tcp", nil, proto.BindExtra{}, "localhost:8080", ""); !errors.Is(err, ErrSessionNotReady) {
		t.Fatalf("expected ErrSessionNotReady, got %v", err)
	}
}

func newTestTunnel(id string) *tunnel {
	t := &tunnel{accept: make(chan *ProxyConn)}
	t.id.Store(id)
	return t
}

// Every leg binds the whole open set. A leg that reconnects after an endpoint
// was created while it was down has to come back carrying that endpoint too,
// otherwise it serves a subset and nothing reports the gap.
func TestLegRebindsWholeOpenSet(t *testing.T) {
	a, b := newTestTunnel("tn_a"), newTestTunnel("tn_b")

	rejoiningRaw := &fakeRaw{}
	rejoining := newLeg(1, rejoiningRaw)
	// came back holding only what it had bound before it dropped
	rejoining.tunnels["tn_a"] = a

	s := &reconnectingSession{
		Logger:   slog.Default(),
		sessions: []*session{newLeg(0, &fakeRaw{}), rejoining},
		tunnels:  map[*tunnel]struct{}{a: {}, b: {}},
	}

	if err := s.restartBinds(rejoining); err != nil {
		t.Fatalf("restartBinds failed: %v", err)
	}

	rejoining.RLock()
	defer rejoining.RUnlock()
	if len(rejoining.tunnels) != 2 {
		t.Fatalf("reconnecting leg bound %d of 2 open tunnels", len(rejoining.tunnels))
	}
	if rejoiningRaw.listens != 2 {
		t.Fatalf("expected 2 binds on the reconnecting leg, got %d", rejoiningRaw.listens)
	}
}

// Closing a tunnel has to unbind it on every leg carrying it. The bind
// response's own closure only knows the leg that first bound it, which would
// leave the endpoint serving in the other regions after Close returned.
func TestCloseUnbindsEveryLeg(t *testing.T) {
	first, second := &fakeRaw{}, &fakeRaw{}
	s := &reconnectingSession{
		Logger:   slog.Default(),
		sessions: []*session{newLeg(0, first), newLeg(1, second)},
	}

	tun, err := s.Listen("tcp", nil, proto.BindExtra{}, "localhost:8080", "")
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	if first.listens != 1 || second.listens != 1 {
		t.Fatalf("expected a bind on each leg, got %d and %d", first.listens, second.listens)
	}

	if err := tun.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if len(first.unlistens) != 1 || len(second.unlistens) != 1 {
		t.Fatalf("Close left the endpoint bound, unlistens were %v and %v",
			first.unlistens, second.unlistens)
	}
}

// Auth needs the raw session set, so a leg is dialed but not yet authed for a
// round trip. Binding there sends the request down a connection the server
// has not authed, which fails a Listen that a healthy leg could have served.
func TestLegIsNotReadyUntilAuthed(t *testing.T) {
	dialing := newLeg(0, nil)
	dialing.swapper.set(&fakeRaw{})
	if dialing.swapper.ready() {
		t.Fatal("a leg that has dialed but not authed is reported as ready")
	}

	up := &fakeRaw{}
	s := &reconnectingSession{
		Logger:   slog.Default(),
		sessions: []*session{dialing, newLeg(1, up)},
	}
	if _, err := s.Listen("tcp", nil, proto.BindExtra{}, "localhost:8080", ""); err != nil {
		t.Fatalf("Listen failed while leg 0 was authenticating: %v", err)
	}
	if up.listens != 1 {
		t.Fatalf("expected the bind to skip the unauthed leg, got %d binds on leg 1", up.listens)
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
		Logger:   slog.Default(),
		sessions: []*session{leg, newLeg(1, up)},
	}
	if _, err := s.Listen("tcp", nil, proto.BindExtra{}, "localhost:8080", ""); err != nil {
		t.Fatalf("Listen failed while leg 0 was reconnecting: %v", err)
	}
	if up.listens != 1 {
		t.Fatalf("expected the bind to skip the dropped leg, got %d binds on leg 1", up.listens)
	}
}

// A tunnel the server rejected must not be rebound by the next leg to
// reconnect, and the legs that hold it under a different id must not turn
// the close into an error.
func TestCloseTunnelDropsItFromTheOpenSet(t *testing.T) {
	rejected := newTestTunnel("tn_1")

	holding := newLeg(0, &fakeRaw{})
	holding.tunnels["tn_1"] = rejected
	other := newLeg(1, &fakeRaw{})
	other.tunnels["tn_other"] = rejected

	s := &reconnectingSession{
		Logger:   slog.Default(),
		sessions: []*session{holding, other},
		tunnels:  map[*tunnel]struct{}{rejected: {}},
	}

	if err := s.CloseTunnel("tn_1", errors.New("rejected")); err != nil {
		t.Fatalf("CloseTunnel failed: %v", err)
	}
	if got := len(s.openTunnels()); got != 0 {
		t.Fatalf("rejected tunnel still open, %d in the set", got)
	}
}
