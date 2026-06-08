package privatedial

import (
	"bufio"
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
	"testing/synctest"
	"time"

	"google.golang.org/protobuf/encoding/protodelim"

	pbpd "golang.ngrok.com/ngrok/privatedial/internal/pb_private_dial"
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

func newTestSession(t *testing.T, openFn func(context.Context) (*sessionConn, error)) *Dialer {
	t.Helper()
	s, _ := newTestSessionWithClock(t, openFn)
	return s
}

func newTestSessionWithClock(t *testing.T, openFn func(context.Context) (*sessionConn, error)) (*Dialer, *manualClock) {
	t.Helper()
	first, err := openFn(context.Background())
	if err != nil {
		t.Fatalf("initial openFn: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	clk := newManualClock(time.Now())
	s := &Dialer{
		ctx:       ctx,
		cancel:    cancel,
		proto:     ProtocolH2,
		ready:     make(chan struct{}),
		connected: true,
		current:   first,
		openFn:    openFn,
		clock:     clk,
		done:      make(chan struct{}),
	}
	go s.supervise()
	t.Cleanup(func() { _ = s.Close() })
	return s, clk
}

func newBareDialerForTest(current *sessionConn, dialWait time.Duration) *Dialer {
	ctx, cancel := context.WithCancel(context.Background())
	return &Dialer{
		ctx:       ctx,
		cancel:    cancel,
		proto:     ProtocolH2,
		ready:     make(chan struct{}),
		connected: true,
		current:   current,
		dialWait:  dialWait,
		done:      make(chan struct{}),
	}
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

	select {
	case <-s.Done():
	case <-time.After(time.Second):
		t.Fatal("Done not closed after fatal error")
	}
	if err := s.Err(); !errors.Is(err, fatal) {
		t.Fatalf("Err = %v, want auth fatal", err)
	}

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
	synctest.Test(t, func(t *testing.T) {
		s := newBareDialerForTest(nil, 80*time.Millisecond)
		defer s.Close() //nolint:errcheck

		errCh := make(chan error, 1)
		go func() {
			_, err := s.DialContext(context.Background(), "tcp", "x.private:80")
			errCh <- err
		}()

		synctest.Wait()
		select {
		case err := <-errCh:
			t.Fatalf("Dial returned before timeout: %v", err)
		default:
		}

		time.Sleep(80 * time.Millisecond)
		synctest.Wait()

		err := <-errCh
		if err == nil {
			t.Fatal("Dial succeeded, want timeout")
		}
		if !errors.Is(err, syscall.ECONNREFUSED) {
			t.Fatalf("error = %v, want ECONNREFUSED", err)
		}
	})
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
		cur, err := s.waitForCurrent(context.Background())
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
	synctest.Test(t, func(t *testing.T) {
		first := newFakeConn("conn-1")
		s := newBareDialerForTest(first, 200*time.Millisecond)
		defer s.Close() //nolint:errcheck

		first.markDraining(time.Second)

		errCh := make(chan error, 1)
		go func() {
			_, err := s.DialContext(context.Background(), "tcp", "x.private:80")
			errCh <- err
		}()

		synctest.Wait()
		select {
		case err := <-errCh:
			t.Fatalf("Dial returned before timeout: %v", err)
		default:
		}

		time.Sleep(200 * time.Millisecond)
		synctest.Wait()

		err := <-errCh
		if err == nil {
			t.Fatal("Dial succeeded on drained conn")
		}
		if !errors.Is(err, syscall.ECONNREFUSED) {
			t.Fatalf("error = %v, want ECONNREFUSED", err)
		}
	})
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

// blockingTransport blocks RoundTrip until the request context is cancelled,
// modeling a conn whose peer has silently gone away.
type blockingTransport struct{}

func (blockingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var dreq pbpd.DialReq
	if err := protodelim.UnmarshalFrom(bufio.NewReader(req.Body), &dreq); err != nil {
		return nil, err
	}
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func (blockingTransport) CloseIdleConnections() {}

func TestDialBudgetBoundsHangingRoundTrip(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newFakeConn("conn-1")
		h.transport = blockingTransport{}
		h.authToken = "tok"

		s := newBareDialerForTest(h, 80*time.Millisecond)
		defer s.Close() //nolint:errcheck

		errCh := make(chan error, 1)
		go func() {
			_, err := s.DialContext(context.Background(), "tcp", "x.private:80")
			errCh <- err
		}()

		synctest.Wait()
		select {
		case err := <-errCh:
			t.Fatalf("Dial returned before timeout: %v", err)
		default:
		}

		time.Sleep(80 * time.Millisecond)
		synctest.Wait()

		err := <-errCh
		if err == nil {
			t.Fatal("Dial succeeded on hanging conn, want timeout")
		}
		if !errors.Is(err, syscall.ECONNREFUSED) {
			t.Fatalf("error = %v, want ECONNREFUSED", err)
		}
	})
}

func TestParkDrainingNoOpAfterClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Dialer{
		ctx:      ctx,
		cancel:   cancel,
		ready:    make(chan struct{}),
		dialWait: 50 * time.Millisecond,
		openFn:   func(context.Context) (*sessionConn, error) { return nil, errors.New("never") },
		done:     make(chan struct{}),
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

	if err := s.Err(); err != nil {
		t.Fatalf("Err = %v before Close, want nil", err)
	}
	if isClosed(s.Done()) {
		t.Fatal("Done closed before Close")
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if !isClosed(s.Done()) {
		t.Fatal("Done not closed after Close")
	}
	if err := s.Err(); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("Err = %v, want ErrSessionClosed", err)
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
	synctest.Test(t, func(t *testing.T) {
		s := newBareDialerForTest(nil, 5*time.Second)
		defer s.Close() //nolint:errcheck

		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)
		go func() {
			_, err := s.DialContext(ctx, "tcp", "x.private:80")
			errCh <- err
		}()

		synctest.Wait()
		cancel()
		synctest.Wait()

		err := <-errCh
		if err == nil {
			t.Fatal("Dial succeeded, want context cancel")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	})
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

func TestErrorFromTrailer(t *testing.T) {
	cases := []struct {
		name      string
		code      string
		message   string
		wantNil   bool
		wantCode  string
		wantMsg   string // substring expected in Error()
		wantRefus bool   // expect syscall.ECONNREFUSED to bubble
	}{
		{
			name:      "unauthorized maps to ECONNREFUSED",
			code:      errCodeUnauthorized,
			message:   "unauthorized",
			wantCode:  errCodeUnauthorized,
			wantMsg:   "unauthorized",
			wantRefus: true,
		},
		{
			name:      "session draining maps to ECONNREFUSED",
			code:      errCodeSessionDraining,
			message:   "session draining",
			wantCode:  errCodeSessionDraining,
			wantMsg:   "session draining",
			wantRefus: true,
		},
		{
			name:     "unmapped code carries no sentinel",
			code:     "ERR_NGROK_9999",
			message:  "some other failure",
			wantCode: "ERR_NGROK_9999",
			wantMsg:  "some other failure",
		},
		{
			name:    "message only",
			message: "no endpoint available",
			wantMsg: "no endpoint available",
		},
		{
			name:    "empty trailers are not an error",
			wantNil: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			trailer := http.Header{}
			if tc.code != "" {
				trailer.Set(dialErrorCodeTrailer, tc.code)
			}
			if tc.message != "" {
				trailer.Set(dialErrorMessageTrailer, tc.message)
			}

			err := errorFromTrailer(trailer)
			if tc.wantNil {
				if err != nil {
					t.Fatalf("errorFromTrailer = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("errorFromTrailer returned nil")
			}
			if err.Code() != tc.wantCode {
				t.Fatalf("Code() = %q, want %q", err.Code(), tc.wantCode)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("Error() = %q, want substring %q", err.Error(), tc.wantMsg)
			}
			// The rehydrated error must be extractable via the public
			// Error interface using errors.As.
			var nerr Error
			if !errors.As(err, &nerr) {
				t.Fatalf("error %T not assignable to privatedial.Error", err)
			}
			if tc.wantCode != "" && !strings.Contains(err.Error(), ngrokErrorsURL) {
				t.Fatalf("Error() = %q, want docs URL when a code is present", err.Error())
			}
			if gotRefus := errors.Is(err, syscall.ECONNREFUSED); gotRefus != tc.wantRefus {
				t.Fatalf("errors.Is(ECONNREFUSED) = %v, want %v", gotRefus, tc.wantRefus)
			}
		})
	}
}

// TestDialConnReadSurfacesTrailerError proves a dialConn surfaces a
// server-reported trailer error in place of io.EOF, while a clean stream
// termination still surfaces as io.EOF.
func TestDialConnReadSurfacesTrailerError(t *testing.T) {
	t.Run("trailer error preempts EOF", func(t *testing.T) {
		addr := dialAddr{addr: "x.private:80", host: "x.private", port: 80}
		resp := &http.Response{Trailer: http.Header{}}
		resp.Trailer.Set(dialErrorCodeTrailer, errCodeSessionDraining)
		resp.Trailer.Set(dialErrorMessageTrailer, "session draining")
		c := &dialConn{remoteAddr: addr, resp: resp, respBody: io.NopCloser(strings.NewReader(""))}

		_, err := c.Read(make([]byte, 8))
		if errors.Is(err, io.EOF) {
			t.Fatalf("Read returned io.EOF, want trailer error")
		}

		// The dial failure must surface as a *net.OpError wrapping the
		// ngrok Error and the net sentinel, just like the old path.
		var op *net.OpError
		if !errors.As(err, &op) {
			t.Fatalf("error %T does not wrap *net.OpError", err)
		}
		if op.Op != "dial" || op.Net != "tcp" {
			t.Fatalf("OpError = %+v", op)
		}
		var nerr Error
		if !errors.As(err, &nerr) {
			t.Fatalf("error %T not assignable to privatedial.Error", err)
		}
		if nerr.Code() != errCodeSessionDraining {
			t.Fatalf("Code() = %q", nerr.Code())
		}
		if !errors.Is(err, syscall.ECONNREFUSED) {
			t.Fatalf("want ECONNREFUSED to bubble, got %v", err)
		}
	})

	t.Run("clean EOF without trailer", func(t *testing.T) {
		c := &dialConn{resp: &http.Response{Trailer: http.Header{}}, respBody: io.NopCloser(strings.NewReader(""))}
		if _, err := c.Read(make([]byte, 8)); !errors.Is(err, io.EOF) {
			t.Fatalf("Read = %v, want io.EOF", err)
		}
	})
}

func TestBothTransportsFailed(t *testing.T) {
	sameQUIC := &serverError{code: "ERR_NGROK_4040", message: "rejected via quic"}
	sameH2 := &serverError{code: "ERR_NGROK_4040", message: "rejected via h2"}

	t.Run("same code collapses to one error", func(t *testing.T) {
		err := bothTransportsFailed(sameQUIC, sameH2)
		var nerr Error
		if !errors.As(err, &nerr) {
			t.Fatalf("error %T not assignable to privatedial.Error", err)
		}
		if nerr.Code() != "ERR_NGROK_4040" {
			t.Fatalf("Code() = %q", nerr.Code())
		}
		if strings.Contains(err.Error(), "both transports failed") {
			t.Fatalf("error should not mention both transports: %q", err.Error())
		}
	})

	t.Run("different codes keep both", func(t *testing.T) {
		other := &serverError{code: "ERR_NGROK_706", message: "not found"}
		err := bothTransportsFailed(sameQUIC, other)
		if !strings.Contains(err.Error(), "both transports failed") {
			t.Fatalf("want combined error, got %q", err.Error())
		}
	})

	t.Run("missing codes keep both", func(t *testing.T) {
		err := bothTransportsFailed(errors.New("quic boom"), errors.New("h2 boom"))
		if !strings.Contains(err.Error(), "quic=quic boom") || !strings.Contains(err.Error(), "h2=h2 boom") {
			t.Fatalf("want combined error, got %q", err.Error())
		}
	})
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

func (d *Dialer) snapshotCurrentForTest() *sessionConn {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.current
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

func TestConnectRequiresServerAddr(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
		want string
	}{
		{"forced h2", Config{ForceProtocol: ProtocolH2}, "H2ServerAddr"},
		{"forced quic", Config{ForceProtocol: ProtocolQUIC}, "QUICServerAddr"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := New(tc.cfg)
			defer func() { _ = d.Close() }()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := d.Connect(ctx)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Connect = %v, want error mentioning %s", err, tc.want)
			}
		})
	}
}
