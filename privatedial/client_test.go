package privatedial

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protodelim"

	pbpd "golang.ngrok.com/ngrok/privatedial/pb_private_dial"
)

func newFakeConn(id string) *sessionConn {
	return &sessionConn{
		serverID:    id,
		proto:       ProtocolH2,
		drainCh:     make(chan struct{}),
		serverErrCh: make(chan error, 1),
		stopCh:      make(chan struct{}),
		sendDone:    make(chan struct{}),
		dialURL:     &url.URL{Scheme: "https", Host: "fake.invalid", Path: "/dial"},
	}
}

func sequentialOpener() func(context.Context) (*sessionConn, error) {
	var n atomic.Int32
	return func(context.Context) (*sessionConn, error) {
		return newFakeConn(idFor(n.Add(1))), nil
	}
}

func newTestSession(t *testing.T, openFn func(context.Context) (*sessionConn, error)) *Session {
	t.Helper()
	s, _ := newTestSessionWithClock(t, openFn)
	return s
}

func newTestSessionWithClock(t *testing.T, openFn func(context.Context) (*sessionConn, error)) (*Session, *manualClock) {
	t.Helper()
	first, err := openFn(context.Background())
	if err != nil {
		t.Fatalf("initial openFn: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	clk := newManualClock(time.Now())
	s := &Session{
		ctx:         ctx,
		cancel:      cancel,
		proto:       ProtocolH2,
		ready:       make(chan struct{}),
		current:     first,
		openFn:      openFn,
		clock:       clk,
		drainCh:     make(chan struct{}),
		serverErrCh: make(chan error, 1),
	}
	go s.supervise()
	t.Cleanup(func() { _ = s.Close() })
	return s, clk
}

func TestReconnectBacksOffAfterTransientOpenError(t *testing.T) {
	first := newFakeConn("conn-1")
	second := newFakeConn("conn-2")
	transient := errors.New("transient")

	var calls atomic.Int32
	openFn := func(context.Context) (*sessionConn, error) {
		switch calls.Add(1) {
		case 1:
			return first, nil
		case 2:
			return nil, transient
		default:
			return second, nil
		}
	}
	s, clk := newTestSessionWithClock(t, openFn)

	first.serverErrCh <- errors.New("boom")

	waitFor(t, time.Second, func() bool { return calls.Load() == 2 })
	cur, _, _ := s.snapshot()
	if cur != nil {
		t.Fatalf("transient failure installed replacement: %v", cur.serverID)
	}
	waitFor(t, time.Second, clk.HasWaiters)
	clk.Step(reconnectBackoffMinDelay)
	waitFor(t, time.Second, func() bool { return s.ServerID() == "conn-2" })
}

func TestReconnectOnDrain(t *testing.T) {
	s := newTestSession(t, sequentialOpener())
	if got := s.ServerID(); got != "conn-1" {
		t.Fatalf("ServerID = %q, want conn-1", got)
	}

	first := s.snapshotCurrentForTest()
	first.markDraining(50 * time.Millisecond)

	waitFor(t, time.Second, func() bool { return s.ServerID() == "conn-2" })
}

func TestDrainKeepsOldConnAliveForGrace(t *testing.T) {
	s, clk := newTestSessionWithClock(t, sequentialOpener())

	first := s.snapshotCurrentForTest()
	first.markDraining(time.Hour)

	waitFor(t, time.Second, func() bool { return s.ServerID() == "conn-2" })
	waitFor(t, time.Second, clk.HasWaiters)
	if isClosed(first.stopCh) {
		t.Fatal("first conn closed before grace expired")
	}
	clk.Step(time.Hour)
	waitFor(t, time.Second, func() bool { return isClosed(first.stopCh) })
}

func TestDrainParksOldConnImmediately(t *testing.T) {
	first := newFakeConn("conn-1")
	second := newFakeConn("conn-2")

	gate := make(chan struct{})
	var calls atomic.Int32
	openFn := func(context.Context) (*sessionConn, error) {
		i := calls.Add(1)
		if i == 1 {
			return first, nil
		}
		<-gate
		return second, nil
	}
	s, clk := newTestSessionWithClock(t, openFn)

	first.markDraining(time.Hour)

	waitFor(t, time.Second, func() bool {
		cur, _, _ := s.snapshot()
		return cur == nil
	})
	waitFor(t, time.Second, clk.HasWaiters)
	if isClosed(first.stopCh) {
		t.Fatal("first conn closed before reconnect gate opened")
	}

	close(gate)
	waitFor(t, time.Second, func() bool { return s.ServerID() == "conn-2" })
	clk.Step(time.Hour)
	waitFor(t, time.Second, func() bool { return isClosed(first.stopCh) })
}

func TestReconnectOnAbruptError(t *testing.T) {
	s := newTestSession(t, sequentialOpener())
	if got := s.ServerID(); got != "conn-1" {
		t.Fatalf("ServerID = %q, want conn-1", got)
	}

	first := s.snapshotCurrentForTest()
	first.serverErrCh <- errors.New("boom")

	waitFor(t, time.Second, func() bool { return s.ServerID() == "conn-2" })
	waitFor(t, time.Second, func() bool { return isClosed(first.stopCh) })
}

func TestAuthFatalStopsReconnect(t *testing.T) {
	first := newFakeConn("conn-1")
	var calls atomic.Int32
	fatal := &authFatalError{status: http.StatusUnauthorized, err: errors.New("bad token")}
	openFn := func(context.Context) (*sessionConn, error) {
		i := calls.Add(1)
		if i == 1 {
			return first, nil
		}
		return nil, fatal
	}
	s := newTestSession(t, openFn)

	first.serverErrCh <- errors.New("boom")

	waitFor(t, time.Second, func() bool {
		_, _, ferr := s.snapshot()
		return ferr != nil
	})

	_, err := s.DialContext(context.Background(), "tcp", "x.private:80")
	if err == nil {
		t.Fatal("Dial succeeded, want fatal error")
	}
	var op *net.OpError
	if !errors.As(err, &op) {
		t.Fatalf("error %T does not wrap *net.OpError", err)
	}
	if !errors.Is(err, fatal) {
		t.Fatalf("error = %v, want auth fatal", err)
	}
}

func TestDialBlocksThenTimesOutWhenNoCurrent(t *testing.T) {
	first := newFakeConn("conn-1")
	transient := errors.New("transient")
	var calls atomic.Int32
	openFn := func(context.Context) (*sessionConn, error) {
		i := calls.Add(1)
		if i == 1 {
			return first, nil
		}
		return nil, transient
	}
	s, clk := newTestSessionWithClock(t, openFn)
	s.dialWait = 80 * time.Millisecond

	first.serverErrCh <- errors.New("boom")
	waitFor(t, time.Second, func() bool {
		cur, _, _ := s.snapshot()
		return cur == nil
	})

	errCh := make(chan error, 1)
	go func() {
		_, err := s.DialContext(context.Background(), "tcp", "x.private:80")
		errCh <- err
	}()
	waitFor(t, time.Second, func() bool { return clk.NumWaiters() >= 2 })
	clk.Step(80 * time.Millisecond)

	err := <-errCh
	if err == nil {
		t.Fatal("Dial succeeded, want timeout")
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Fatalf("error = %v, want ECONNREFUSED", err)
	}
}

func TestWaitForCurrentWakesOnReconnect(t *testing.T) {
	first := newFakeConn("conn-1")
	second := newFakeConn("conn-2")

	gate := make(chan struct{})
	var calls atomic.Int32
	openFn := func(context.Context) (*sessionConn, error) {
		i := calls.Add(1)
		if i == 1 {
			return first, nil
		}
		<-gate
		return second, nil
	}
	s := newTestSession(t, openFn)
	s.dialWait = 5 * time.Second

	first.serverErrCh <- errors.New("boom")
	waitFor(t, time.Second, func() bool {
		cur, _, _ := s.snapshot()
		return cur == nil
	})

	type result struct {
		cur *sessionConn
		err error
	}
	out := make(chan result, 1)
	go func() {
		cur, err := s.waitForCurrent(context.Background(), make(chan time.Time))
		out <- result{cur: cur, err: err}
	}()

	close(gate)

	select {
	case r := <-out:
		if r.err != nil {
			t.Fatalf("waitForCurrent error: %v", r.err)
		}
		if r.cur != second {
			t.Fatalf("waitForCurrent conn = %p, want %p", r.cur, second)
		}
	case <-time.After(time.Second):
		t.Fatal("waitForCurrent did not wake after reconnect")
	}
}

func TestDrainWinsOverConcurrentServerErr(t *testing.T) {
	s := newTestSession(t, sequentialOpener())
	first := s.snapshotCurrentForTest()

	first.markDraining(time.Second)
	first.serverErrCh <- errors.New("boom")

	waitFor(t, time.Second, func() bool { return s.ServerID() == "conn-2" })
	if isClosed(first.stopCh) {
		t.Fatal("first conn hard-closed instead of parked for grace")
	}
}

func TestCloseDuringReconnectDiscardsLateConn(t *testing.T) {
	first := newFakeConn("conn-1")
	second := newFakeConn("conn-2")

	release := make(chan struct{})
	entered := make(chan struct{})
	var calls atomic.Int32
	openFn := func(context.Context) (*sessionConn, error) {
		i := calls.Add(1)
		if i == 1 {
			return first, nil
		}
		close(entered)
		<-release
		return second, nil
	}
	s := newTestSession(t, openFn)

	first.serverErrCh <- errors.New("boom")
	waitForChan(t, entered, time.Second, "openFn entered")

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	close(release)
	waitFor(t, time.Second, func() bool { return isClosed(second.stopCh) })

	cur, _, _ := s.snapshot()
	if cur != nil {
		t.Fatalf("current resurrected after Close: %v", cur.serverID)
	}
}

func TestDialSkipsDrainedCurrent(t *testing.T) {
	first := newFakeConn("conn-1")
	clk := newManualClock(time.Now())
	openFn := func(context.Context) (*sessionConn, error) {
		return first, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{
		ctx:         ctx,
		cancel:      cancel,
		proto:       ProtocolH2,
		ready:       make(chan struct{}),
		current:     first,
		openFn:      openFn,
		clock:       clk,
		dialWait:    200 * time.Millisecond,
		drainCh:     make(chan struct{}),
		serverErrCh: make(chan error, 1),
	}
	t.Cleanup(func() { _ = s.Close() })

	first.markDraining(time.Second)

	errCh := make(chan error, 1)
	go func() {
		_, err := s.DialContext(context.Background(), "tcp", "x.private:80")
		errCh <- err
	}()
	waitFor(t, time.Second, clk.HasWaiters)
	clk.Step(200 * time.Millisecond)

	err := <-errCh
	if err == nil {
		t.Fatal("Dial succeeded on drained conn")
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Fatalf("error = %v, want ECONNREFUSED", err)
	}
}

func TestAcceptStreamSerializesWithDrain(t *testing.T) {
	h := newFakeConn("conn-1")
	if !h.acceptsStreams() {
		t.Fatal("fresh conn must accept streams")
	}

	h.markDraining(0)
	if h.acceptsStreams() {
		t.Fatal("drained conn accepts streams")
	}
	if !isClosed(h.drainCh) {
		t.Fatal("markDraining did not signal drainCh")
	}
}

func TestAcceptStreamSerializesWithClose(t *testing.T) {
	h := newFakeConn("conn-1")
	_ = h.close()
	if h.acceptsStreams() {
		t.Fatal("closed conn accepts streams")
	}
}

func TestCloseStopsGraceTimers(t *testing.T) {
	s, clk := newTestSessionWithClock(t, sequentialOpener())
	first := s.snapshotCurrentForTest()

	first.markDraining(time.Hour)
	waitFor(t, time.Second, func() bool { return s.ServerID() == "conn-2" })
	waitFor(t, time.Second, clk.HasWaiters)

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !isClosed(first.stopCh) {
		t.Fatal("first draining conn not closed by Close")
	}
	if n := clk.NumWaiters(); n != 0 {
		t.Fatalf("Close left %d grace timers", n)
	}
}

func TestSessionConnDialRefusesAfterClose(t *testing.T) {
	h := newFakeConn("conn-1")
	_ = h.close()

	_, err := h.dial(context.Background(), dialAddr{addr: "x.private:80", host: "x.private", port: 80})
	if err == nil {
		t.Fatal("dial succeeded after close")
	}
	var stale *staleConnError
	if !errors.As(err, &stale) {
		t.Fatalf("error %T does not wrap staleConnError", err)
	}
}

func TestParkDrainingNoOpAfterClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{
		ctx:         ctx,
		cancel:      cancel,
		ready:       make(chan struct{}),
		dialWait:    50 * time.Millisecond,
		openFn:      func(context.Context) (*sessionConn, error) { return nil, errors.New("never") },
		drainCh:     make(chan struct{}),
		serverErrCh: make(chan error, 1),
	}
	cancel()

	h := newFakeConn("conn-1")
	s.parkDraining(h, time.Hour)

	if !isClosed(h.stopCh) {
		t.Fatal("parkDraining did not close conn after Close")
	}
}

func TestDialFailsImmediatelyAfterClose(t *testing.T) {
	s := newTestSession(t, func(context.Context) (*sessionConn, error) {
		return newFakeConn("conn-1"), nil
	})
	s.dialWait = 5 * time.Second
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err := s.DialContext(context.Background(), "tcp", "x.private:80")
	if err == nil {
		t.Fatal("Dial succeeded after Close")
	}
}

func TestCloseStopsSupervisorAndConns(t *testing.T) {
	s := newTestSession(t, sequentialOpener())

	first := s.snapshotCurrentForTest()
	first.markDraining(time.Hour)
	waitFor(t, time.Second, func() bool { return s.ServerID() == "conn-2" })
	second := s.snapshotCurrentForTest()

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	if !isClosed(first.stopCh) {
		t.Fatal("first draining conn not closed")
	}
	if !isClosed(second.stopCh) {
		t.Fatal("current conn not closed")
	}
	if s.ctx.Err() == nil {
		t.Fatal("supervisor ctx not canceled")
	}
}

func TestDialContextCancelDuringWait(t *testing.T) {
	first := newFakeConn("conn-1")
	transient := errors.New("transient")
	var calls atomic.Int32
	openFn := func(context.Context) (*sessionConn, error) {
		i := calls.Add(1)
		if i == 1 {
			return first, nil
		}
		return nil, transient
	}
	s, clk := newTestSessionWithClock(t, openFn)
	s.dialWait = 5 * time.Second

	first.serverErrCh <- errors.New("boom")
	waitFor(t, time.Second, func() bool {
		cur, _, _ := s.snapshot()
		return cur == nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	var (
		wg  sync.WaitGroup
		err error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err = s.DialContext(ctx, "tcp", "x.private:80")
	}()
	waitFor(t, time.Second, func() bool { return clk.NumWaiters() >= 2 })
	cancel()
	wg.Wait()
	if err == nil {
		t.Fatal("Dial succeeded, want context cancel")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestReadControlHandlesPleaseDrain(t *testing.T) {
	pr, pw := io.Pipe()
	h := newFakeConn("conn-1")
	h.controlRespBody = newReadCloseByteReader(pr)

	done := make(chan struct{})
	go func() {
		h.readControl()
		close(done)
	}()
	t.Cleanup(func() {
		_ = pw.Close()
		waitForChan(t, done, time.Second, "readControl done")
	})

	_, err := protodelim.MarshalTo(pw, &pbpd.ControlFrame{
		Frame: &pbpd.ControlFrame_PleaseDrain{
			PleaseDrain: &pbpd.PleaseDrain{
				Reason:             "shutting down",
				GracePeriodSeconds: 7,
			},
		},
	})
	if err != nil {
		t.Fatalf("MarshalTo: %v", err)
	}

	waitFor(t, time.Second, func() bool { return isClosed(h.drainCh) })
	if h.acceptsStreams() {
		t.Fatal("admission gate must be closed by the time drainCh closes")
	}
	if h.drainGrace != 7*time.Second {
		t.Fatalf("drainGrace = %v, want 7s", h.drainGrace)
	}
}

func TestReadControlSessionErrorWritesServerErr(t *testing.T) {
	pr, pw := io.Pipe()
	h := newFakeConn("conn-1")
	h.controlRespBody = newReadCloseByteReader(pr)

	done := make(chan struct{})
	go func() {
		h.readControl()
		close(done)
	}()
	t.Cleanup(func() { _ = pw.Close() })

	_, err := protodelim.MarshalTo(pw, &pbpd.ControlFrame{
		Frame: &pbpd.ControlFrame_SessionError{
			SessionError: &pbpd.SessionError{Message: "auth revoked"},
		},
	})
	if err != nil {
		t.Fatalf("MarshalTo: %v", err)
	}

	select {
	case err := <-h.serverErrCh:
		if err == nil || !strings.Contains(err.Error(), "auth revoked") {
			t.Fatalf("serverErrCh = %v, want auth revoked", err)
		}
	case <-time.After(time.Second):
		t.Fatal("serverErrCh did not receive SessionError")
	}
	waitForChan(t, done, time.Second, "readControl done")
}

func TestReadControlEOFSurfacesAsServerErr(t *testing.T) {
	pr, pw := io.Pipe()
	h := newFakeConn("conn-1")
	h.controlRespBody = newReadCloseByteReader(pr)

	done := make(chan struct{})
	go func() {
		h.readControl()
		close(done)
	}()
	_ = pw.Close()

	select {
	case err := <-h.serverErrCh:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("serverErrCh = %v, want EOF", err)
		}
	case <-time.After(time.Second):
		t.Fatal("serverErrCh did not receive EOF")
	}
	waitForChan(t, done, time.Second, "readControl done")
}

func TestDialResponseErrorMapping(t *testing.T) {
	addr := dialAddr{addr: "x.private:80", host: "x.private", port: 80}

	cases := []struct {
		name      string
		status    int
		errorCode string
		body      string
		assert    func(*testing.T, error)
	}{
		{
			name:      "404 maps to DNSError IsNotFound",
			status:    http.StatusNotFound,
			errorCode: "ERR_NGROK_706",
			body:      "endpoint not found",
			assert: func(t *testing.T, err error) {
				var dnsErr *net.DNSError
				if !errors.As(err, &dnsErr) {
					t.Fatalf("error %T does not wrap *net.DNSError", err)
				}
				if !dnsErr.IsNotFound || dnsErr.Name != "x.private" {
					t.Fatalf("DNSError = %+v", dnsErr)
				}

				var se *ServerError
				if !errors.As(err, &se) {
					t.Fatalf("error %T does not wrap *ServerError", err)
				}
				if se.Code != "ERR_NGROK_706" || se.Message != "endpoint not found" || se.Status != http.StatusNotFound {
					t.Fatalf("ServerError = %+v", se)
				}
			},
		},
		{
			name:   "503 maps to ECONNREFUSED",
			status: http.StatusServiceUnavailable,
			body:   "no endpoint available",
			assert: func(t *testing.T, err error) {
				if !errors.Is(err, syscall.ECONNREFUSED) {
					t.Fatalf("error = %v, want ECONNREFUSED", err)
				}
			},
		},
		{
			name:   "401 maps to ECONNREFUSED",
			status: http.StatusUnauthorized,
			body:   "missing bearer token",
			assert: func(t *testing.T, err error) {
				if !errors.Is(err, syscall.ECONNREFUSED) {
					t.Fatalf("error = %v, want ECONNREFUSED", err)
				}
			},
		},
		{
			name:   "403 maps to ECONNREFUSED",
			status: http.StatusForbidden,
			body:   "invalid token",
			assert: func(t *testing.T, err error) {
				if !errors.Is(err, syscall.ECONNREFUSED) {
					t.Fatalf("error = %v, want ECONNREFUSED", err)
				}
			},
		},
		{
			name:   "429 maps to ECONNREFUSED",
			status: http.StatusTooManyRequests,
			body:   "rate limited",
			assert: func(t *testing.T, err error) {
				if !errors.Is(err, syscall.ECONNREFUSED) {
					t.Fatalf("error = %v, want ECONNREFUSED", err)
				}
			},
		},
		{
			name:   "500 has no sentinel",
			status: http.StatusInternalServerError,
			body:   "internal error",
			assert: func(t *testing.T, err error) {
				var se *ServerError
				if !errors.As(err, &se) {
					t.Fatalf("error %T does not wrap *ServerError", err)
				}
				if se.Message != "internal error" || se.Status != http.StatusInternalServerError {
					t.Fatalf("ServerError = %+v", se)
				}
				var dnsErr *net.DNSError
				if errors.As(err, &dnsErr) {
					t.Fatalf("unexpected DNSError: %+v", dnsErr)
				}
				if errors.Is(err, syscall.ECONNREFUSED) {
					t.Fatal("unexpected ECONNREFUSED")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tc.status,
				Header:     http.Header{},
			}
			if tc.errorCode != "" {
				resp.Header.Set(dialErrorCodeHeader, tc.errorCode)
			}
			err := dialResponseError(resp, addr, tc.body)
			if err == nil {
				t.Fatal("dialResponseError returned nil")
			}
			var op *net.OpError
			if !errors.As(err, &op) {
				t.Fatalf("error %T does not wrap *net.OpError", err)
			}
			if op.Op != "dial" || op.Net != "tcp" {
				t.Fatalf("OpError = %+v", op)
			}
			tc.assert(t, err)
		})
	}
}

type manualClock struct {
	mu     sync.Mutex
	now    time.Time
	timers map[*manualTimer]struct{}
}

func newManualClock(now time.Time) *manualClock {
	return &manualClock{
		now:    now,
		timers: make(map[*manualTimer]struct{}),
	}
}

func (c *manualClock) NewTimer(d time.Duration) sessionTimer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &manualTimer{
		clock:    c,
		deadline: c.now.Add(d),
		ch:       make(chan time.Time, 1),
	}
	c.timers[t] = struct{}{}
	c.fireReadyLocked()
	return t
}

func (c *manualClock) Step(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	c.fireReadyLocked()
}

func (c *manualClock) HasWaiters() bool {
	return c.NumWaiters() > 0
}

func (c *manualClock) NumWaiters() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}

func (c *manualClock) fireReadyLocked() {
	for t := range c.timers {
		if t.stopped || t.fired || t.deadline.After(c.now) {
			continue
		}
		t.fired = true
		delete(c.timers, t)
		t.ch <- c.now
	}
}

type manualTimer struct {
	clock    *manualClock
	deadline time.Time
	ch       chan time.Time
	stopped  bool
	fired    bool
}

func (t *manualTimer) C() <-chan time.Time {
	return t.ch
}

func (t *manualTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	if t.stopped || t.fired {
		return false
	}
	t.stopped = true
	delete(t.clock.timers, t)
	return true
}

func (s *Session) snapshotCurrentForTest() *sessionConn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

func idFor(i int32) string {
	return "conn-" + strconv.Itoa(int(i))
}

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func waitForChan(t *testing.T, ch <-chan struct{}, timeout time.Duration, name string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for %s", name)
	}
}

var _ = func() bool {
	err := &net.OpError{Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}}
	return errors.Is(err, syscall.ECONNREFUSED)
}()
