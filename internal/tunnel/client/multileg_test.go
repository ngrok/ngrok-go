package client

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"golang.ngrok.com/ngrok/v2/internal/tunnel/netx"
	"golang.ngrok.com/ngrok/v2/internal/tunnel/proto"
)

// fakeRaw is a RawSession that binds successfully, standing in for a leg whose
// region the client can reach.
type fakeRaw struct {
	mu sync.Mutex
	// blockAccept stands in for a leg that stays connected, so the session
	// under test does not run its reconnect loop.
	blockAccept bool
	listens     int
	patches     int
	unlistens   []string
}

func (f *fakeRaw) Auth(id string, extra proto.AuthExtra) (proto.AuthResp, error) {
	return proto.AuthResp{ClientID: "client"}, nil
}

func (f *fakeRaw) Listen(protocol string, opts any, extra proto.BindExtra, id string, forwardsTo string, forwardsProto string) (proto.BindResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listens++
	return proto.BindResp{ClientID: "tn_1", URL: "tcp://example:1234", Proto: protocol}, nil
}

func (f *fakeRaw) ListenLabel(labels map[string]string, metadata string, forwardsTo string, forwardsProto string) (proto.StartTunnelWithLabelResp, error) {
	return proto.StartTunnelWithLabelResp{ID: "tn_1"}, nil
}

func (f *fakeRaw) Unlisten(id string) (proto.UnbindResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unlistens = append(f.unlistens, id)
	return proto.UnbindResp{}, nil
}

func (f *fakeRaw) Accept() (netx.LoggedConn, error) {
	if f.blockAccept {
		select {}
	}
	return nil, errors.New("not accepting")
}

func (f *fakeRaw) PatchTunnelState(id string, name, description, metadata *string, poolingEnabled *bool, trafficPolicy *string) (proto.PatchTunnelStateResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.patches++
	return proto.PatchTunnelStateResp{}, nil
}

func (f *fakeRaw) SendAgentMetrics(ctx context.Context, batch []byte) ([]byte, error) {
	return nil, nil
}
func (f *fakeRaw) SrvInfo() (proto.SrvInfoResp, error) { return proto.SrvInfoResp{}, nil }
func (f *fakeRaw) Latency() <-chan time.Duration       { return nil }
func (f *fakeRaw) Heartbeat() (time.Duration, error)   { return 0, nil }
func (f *fakeRaw) Close() error                        { return nil }

func (f *fakeRaw) bindCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listens
}

func (f *fakeRaw) unbound() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.unlistens...)
}

// testSession builds a reconnecting session with the given legs, connected
// only where raw is non-nil. A nil raw leaves the swapper unset, which is the
// state of a leg whose region never answered.
func testSession(raws ...RawSession) *reconnectingSession {
	s := &reconnectingSession{
		Logger:       slog.Default(),
		stateChanges: make(chan error, len(raws)+1),
		tunnels:      make(map[*tunnel]struct{}),
	}
	for i, raw := range raws {
		l := newLeg(s, slog.Default(), uint32(i))
		if raw != nil {
			l.swapper.set(raw)
			l.swapper.serving()
			s.readyCount++
		}
		s.sessions = append(s.sessions, l)
	}
	return s
}

// An endpoint that bound on a reachable leg must keep serving when another leg
// never connected. Before this was fixed the unreachable leg returned
// ErrSessionNotReady and took the whole bind down, which terminated the agent.
func TestListenSucceedsWhenSecondLegNeverConnected(t *testing.T) {
	up := &fakeRaw{}
	s := testSession(up, nil)

	tun, err := s.Listen("tcp", nil, proto.BindExtra{}, "localhost:8080", "")
	if err != nil {
		t.Fatalf("Listen failed with one leg down: %v", err)
	}
	if tun == nil {
		t.Fatal("Listen returned no tunnel")
	}
	if up.bindCount() != 1 {
		t.Fatalf("expected 1 bind on the reachable leg, got %d", up.bindCount())
	}
}

// No specific leg is required. Leg 0 being down must not deny service when a
// later leg is up.
func TestListenUsesAnyReadyLeg(t *testing.T) {
	up := &fakeRaw{}
	s := testSession(nil, up)

	if _, err := s.Listen("tcp", nil, proto.BindExtra{}, "localhost:8080", ""); err != nil {
		t.Fatalf("Listen failed with leg 0 down: %v", err)
	}
	if up.bindCount() != 1 {
		t.Fatalf("expected the bind to land on leg 1, got %d binds", up.bindCount())
	}
}

// With no leg up at all there is nothing to serve on, so the caller still gets
// ErrSessionNotReady.
func TestListenFailsWhenNoLegReady(t *testing.T) {
	s := testSession(nil, nil)

	if _, err := s.Listen("tcp", nil, proto.BindExtra{}, "localhost:8080", ""); !errors.Is(err, ErrSessionNotReady) {
		t.Fatalf("expected ErrSessionNotReady, got %v", err)
	}
}

// Session-wide calls are not tied to leg 0 either. A region the client cannot
// reach must not fail a call another leg can serve.
func TestSessionCallsUseAnyReadyLeg(t *testing.T) {
	up := &fakeRaw{}
	s := testSession(nil, up)

	if err := s.PatchTunnelState("tn_1", nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("PatchTunnelState failed with leg 0 down: %v", err)
	}
	up.mu.Lock()
	patches := up.patches
	up.mu.Unlock()
	if patches != 1 {
		t.Fatalf("expected the patch to land on leg 1, got %d", patches)
	}

	if _, err := s.SrvInfo(); err != nil {
		t.Fatalf("SrvInfo failed with leg 0 down: %v", err)
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
	s := testSession(&fakeRaw{}, rejoiningRaw)
	rejoining := s.sessions[1]
	// came back holding only what it had bound before it dropped
	rejoining.tunnels["tn_a"] = a
	s.tunnels[a] = struct{}{}
	s.tunnels[b] = struct{}{}

	if _, err := s.restartBinds(rejoining); err != nil {
		t.Fatalf("restartBinds failed: %v", err)
	}

	rejoining.RLock()
	defer rejoining.RUnlock()
	if len(rejoining.tunnels) != 2 {
		t.Fatalf("reconnecting leg bound %d of 2 open tunnels", len(rejoining.tunnels))
	}
	if rejoiningRaw.bindCount() != 2 {
		t.Fatalf("expected 2 binds on the reconnecting leg, got %d", rejoiningRaw.bindCount())
	}
}

// Closing a tunnel has to unbind it on every leg carrying it. The bind
// response's own closure only knows the leg that first bound it, which would
// leave the endpoint serving in the other regions after Close returned.
func TestCloseUnbindsEveryLeg(t *testing.T) {
	first, second := &fakeRaw{}, &fakeRaw{}
	s := testSession(first, second)

	tun, err := s.Listen("tcp", nil, proto.BindExtra{}, "localhost:8080", "")
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	if first.bindCount() != 1 || second.bindCount() != 1 {
		t.Fatalf("expected a bind on each leg, got %d and %d", first.bindCount(), second.bindCount())
	}

	if err := tun.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if len(first.unbound()) != 1 || len(second.unbound()) != 1 {
		t.Fatalf("Close left the endpoint bound, unlistens were %v and %v",
			first.unbound(), second.unbound())
	}
}

// A leg that is down has nothing bound remotely, so the entries left in its
// map must not turn the user's Close into an error.
func TestCloseSucceedsWhenALegIsDown(t *testing.T) {
	up := &fakeRaw{}
	s := testSession(up, nil)

	tun, err := s.Listen("tcp", nil, proto.BindExtra{}, "localhost:8080", "")
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	// the leg dropped after it had bound, so its map still names the endpoint
	s.sessions[1].tunnels["tn_stale"] = tun.(*tunnel)

	if err := tun.Close(); err != nil {
		t.Fatalf("Close failed because a leg was down: %v", err)
	}
	if len(up.unbound()) != 1 {
		t.Fatalf("expected the ready leg to unbind once, got %v", up.unbound())
	}
}

// Auth needs the raw session set, so a leg is dialed but not yet authed for a
// round trip. Binding there sends the request down a connection the server
// has not authed, which fails a Listen that a healthy leg could have served.
func TestLegIsNotReadyUntilAuthed(t *testing.T) {
	up := &fakeRaw{}
	s := testSession(nil, up)
	dialing := s.sessions[0]
	dialing.swapper.set(&fakeRaw{})
	if dialing.swapper.ready() {
		t.Fatal("a leg that has dialed but not authed is reported as ready")
	}

	if _, err := s.Listen("tcp", nil, proto.BindExtra{}, "localhost:8080", ""); err != nil {
		t.Fatalf("Listen failed while leg 0 was authenticating: %v", err)
	}
	if up.bindCount() != 1 {
		t.Fatalf("expected the bind to skip the unauthed leg, got %d binds on leg 1", up.bindCount())
	}
}

// ready must mean "connected now", not "connected at some point". A leg that
// dropped and is backing off would otherwise be chosen to bind on and fail
// against a closed connection.
func TestClearedLegIsNotReady(t *testing.T) {
	up := &fakeRaw{}
	s := testSession(&fakeRaw{}, up)
	dropped := s.sessions[0]
	if !dropped.swapper.ready() {
		t.Fatal("a connected leg should be ready")
	}

	s.legDown(dropped)
	if dropped.swapper.ready() {
		t.Fatal("a leg whose connection dropped is still reported as ready")
	}

	if _, err := s.Listen("tcp", nil, proto.BindExtra{}, "localhost:8080", ""); err != nil {
		t.Fatalf("Listen failed while leg 0 was reconnecting: %v", err)
	}
	if up.bindCount() != 1 {
		t.Fatalf("expected the bind to skip the dropped leg, got %d binds on leg 1", up.bindCount())
	}
}

// A tunnel the server rejected must not be rebound by the next leg to
// reconnect, and the legs that hold it under a different id must not turn
// the close into an error.
func TestCloseTunnelDropsItFromTheOpenSet(t *testing.T) {
	rejected := newTestTunnel("tn_1")

	s := testSession(&fakeRaw{}, &fakeRaw{})
	s.sessions[0].tunnels["tn_1"] = rejected
	s.sessions[1].tunnels["tn_other"] = rejected
	s.tunnels[rejected] = struct{}{}

	if err := s.CloseTunnel("tn_1", errors.New("rejected")); err != nil {
		t.Fatalf("CloseTunnel failed: %v", err)
	}
	if got := len(s.openTunnels()); got != 0 {
		t.Fatalf("rejected tunnel still open, %d in the set", got)
	}
}

// The established signal describes the session, not a leg. Three legs coming
// up must look like one connection to the consumer, whose ConnectHandler runs
// once per signal.
func TestOneEstablishedSignalForTheWholeSession(t *testing.T) {
	stateChanges := make(chan error, 8)
	dialer := func(legNumber uint32) (RawSession, error) {
		return &fakeRaw{blockAccept: true}, nil
	}

	var authed sync.WaitGroup
	authed.Add(3)
	cb := func(s Session, r RawSession, legNumber uint32) (int, error) {
		authed.Done()
		return 3, nil
	}

	_ = NewReconnectingSession(slog.Default(), dialer, stateChanges, cb)

	if err := <-stateChanges; err != nil {
		t.Fatalf("expected the session to report established, got %v", err)
	}
	authed.Wait()

	select {
	case extra := <-stateChanges:
		t.Fatalf("a second leg published its own state change, %v", extra)
	case <-time.After(200 * time.Millisecond):
	}
}

// A region the client cannot reach keeps failing for as long as the session
// lives. While another leg serves traffic, those failures are not the
// session's state and must not reach the consumer.
func TestFailingLegIsQuietWhileAnotherServes(t *testing.T) {
	s := testSession(nil, nil)

	serving, failing := s.sessions[0], s.sessions[1]
	if !s.legServing(serving) {
		t.Fatal("the first leg to come up did not report the session established")
	}

	if s.legDown(failing) {
		t.Fatal("a failing leg reported the serving session as down")
	}
	if s.legDown(failing) {
		t.Fatal("a leg failing again reported the serving session as down")
	}
	if s.legServing(serving) {
		t.Fatal("a leg already serving reported the session established twice")
	}
}

// With every leg gone the session really is down, so the failure is published.
func TestLastLegDownIsPublished(t *testing.T) {
	s := testSession(nil)

	only := s.sessions[0]
	s.legServing(only)
	if !s.legDown(only) {
		t.Fatal("losing the last leg did not report the session down")
	}
}
