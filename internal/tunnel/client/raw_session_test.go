package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/muxado/v2"
	"golang.ngrok.com/muxado/v2/frame"
	"golang.ngrok.com/ngrok/v2/internal/testcontext"
	"golang.ngrok.com/ngrok/v2/internal/tunnel/proto"
)

type dummyStream struct{}

func (d *dummyStream) Read(bs []byte) (int, error)  { return 0, nil }
func (d *dummyStream) Write(bs []byte) (int, error) { return 0, nil }
func (d *dummyStream) Close() error                 { return nil }

func TestRawSessionDoubleClose(t *testing.T) {
	r := NewRawSession(slog.Default(), muxado.Client(&dummyStream{}, nil), nil, nil)

	// Verify that closing the session twice doesn't cause a panic
	r.Close()
	r.Close()
}

func TestHeartbeatTimeout(t *testing.T) {
	r := NewRawSession(slog.Default(), muxado.Client(&dummyStream{}, nil), nil, nil)
	// Make sure we don't deadlock
	r.(*rawSession).onHeartbeat(1, true)
}

func TestRawSessionCloseRace(t *testing.T) {
	ctx, cancel := context.WithTimeout(testcontext.ForTB(t), time.Second*1)
	defer cancel()

	// Since this is a race condition, run the test as many times as we can
	// within the timebox to see if we can hit it.
testloop:
	for {
		select {
		case <-ctx.Done():
			break testloop
		default:
		}

		ctx, cancel := context.WithCancel(ctx)
		logger := slog.Default()
		r := NewRawSession(logger, muxado.Client(&dummyStream{}, nil), nil, nil)

		wg := sync.WaitGroup{}

		// Call onHeartbeat as fast as we can in the background.
		wg.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				r.(*rawSession).onHeartbeat(time.Millisecond*1, false)
			}
		})

		// Verify that closing the session while a heartbeat is in flight won't
		// cause a panic
		r.Close()

		cancel()

		// Wait till the heartbeat goroutine exists to make sure we capture the
		// panic and it doesn't occur after the test completes.
		wg.Wait()
	}
}

// newAgentMetricsSession returns a RawSession whose server runs serve on each
// stream that the client opens.
func newAgentMetricsSession(t *testing.T, serve func(muxado.TypedStream)) RawSession {
	clientConn, serverConn := net.Pipe()
	return startAgentMetricsSession(t, clientConn, serverConn, serve)
}

func startAgentMetricsSession(t *testing.T, clientConn, serverConn net.Conn, serve func(muxado.TypedStream)) RawSession {
	// Heartbeats run only on demand, so they do not write during a test.
	heartbeat := muxado.NewHeartbeatConfig()
	heartbeat.Interval = time.Hour
	raw := NewRawSession(slog.Default(), muxado.Client(clientConn, nil), heartbeat, nil)
	t.Cleanup(func() { raw.Close() })

	srv := muxado.NewHeartbeat(muxado.NewTypedStreamSession(muxado.Server(serverConn, nil)), nil, nil)
	t.Cleanup(func() { srv.Close() })
	go func() {
		for {
			stream, err := srv.AcceptTypedStream()
			if err != nil {
				return
			}
			go func() {
				defer stream.Close()
				serve(stream)
			}()
		}
	}()
	return raw
}

// synGateConn holds the header write of the first DATA frame with the SYN
// flag after armed is set. That frame opens the next stream. held is
// closed when that write waits, and the write continues when release is
// closed. The held slice is the frame's own header buffer, as it is for a
// transport write that stalls.
type synGateConn struct {
	net.Conn
	armed   atomic.Bool
	once    sync.Once
	held    chan struct{}
	release chan struct{}
}

func (c *synGateConn) Write(b []byte) (int, error) {
	if c.armed.Load() && len(b) == 8 &&
		frame.Type(b[3]>>4) == frame.TypeData && frame.Flags(b[3]&0x0F).IsSet(frame.FlagDataSyn) {
		c.once.Do(func() {
			close(c.held)
			<-c.release
		})
	}
	return c.Conn.Write(b)
}

// newStalledSession returns a RawSession whose peer never reads from the
// transport.
func newStalledSession(t *testing.T) RawSession {
	clientConn, serverConn := net.Pipe()
	raw := NewRawSession(slog.Default(), muxado.Client(clientConn, nil), nil, nil)
	t.Cleanup(func() {
		serverConn.Close()
		raw.Close()
	})
	return raw
}

// readFrame reads one varint length-delimited payload.
func readFrame(r *bufio.Reader) ([]byte, error) {
	n, err := binary.ReadUvarint(r)
	if err != nil {
		return nil, err
	}
	b := make([]byte, n)
	_, err = io.ReadFull(r, b)
	return b, err
}

// writeFrame writes one varint length-delimited payload.
func writeFrame(w io.Writer, b []byte) error {
	_, err := w.Write(append(binary.AppendUvarint(nil, uint64(len(b))), b...))
	return err
}

func metricsContext(t *testing.T, timeout time.Duration) context.Context {
	ctx, cancel := context.WithTimeout(testcontext.ForTB(t), timeout)
	t.Cleanup(cancel)
	return ctx
}

func TestSendAgentMetrics(t *testing.T) {
	t.Run("frames the batch and the result", func(t *testing.T) {
		// Both payloads are longer than 127 bytes, so their length prefixes
		// take two varint bytes.
		batch := bytes.Repeat([]byte("b"), 300)
		result := bytes.Repeat([]byte("r"), 200)

		type request struct {
			streamType muxado.StreamType
			body       []byte
			err        error
		}
		requests := make(chan request, 1)
		raw := newAgentMetricsSession(t, func(stream muxado.TypedStream) {
			r := bufio.NewReader(stream)
			req := request{streamType: stream.StreamType()}
			defer func() { requests <- req }()
			n, err := binary.ReadUvarint(r)
			if err != nil {
				req.err = err
				return
			}
			req.body = make([]byte, n)
			if _, req.err = io.ReadFull(r, req.body); req.err != nil {
				return
			}
			_, req.err = stream.Write(append([]byte{0xC8, 0x01}, result...)) // 200 as a uvarint
		})

		got, err := raw.SendAgentMetrics(metricsContext(t, time.Minute), batch)
		require.NoError(t, err)
		req := <-requests
		require.NoError(t, req.err)
		require.Equal(t, muxado.StreamType(proto.AgentMetricsReq), req.streamType)
		require.Equal(t, batch, req.body)
		require.Equal(t, result, got)
	})

	t.Run("rejects an oversized result", func(t *testing.T) {
		raw := newAgentMetricsSession(t, func(stream muxado.TypedStream) {
			_ = writeFrame(stream, make([]byte, maxAgentMetricsResultBytes+1))
		})

		_, err := raw.SendAgentMetrics(metricsContext(t, time.Minute), []byte{})
		require.Error(t, err)
	})

	t.Run("requires a deadline", func(t *testing.T) {
		raw := newStalledSession(t)

		_, err := raw.SendAgentMetrics(context.Background(), []byte{})
		require.Error(t, err)
	})

	t.Run("does not open a stream when the context has ended", func(t *testing.T) {
		raw := newStalledSession(t)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		cancel()

		_, err := raw.SendAgentMetrics(ctx, []byte{})
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("keeps a stalled write past the deadline and the session intact", func(t *testing.T) {
		type received struct {
			body []byte
			err  error
		}
		batches := make(chan received, 2)
		clientConn, serverConn := net.Pipe()
		gate := &synGateConn{Conn: clientConn, held: make(chan struct{}), release: make(chan struct{})}
		// The server echoes each batch as the result.
		raw := startAgentMetricsSession(t, gate, serverConn, func(stream muxado.TypedStream) {
			body, err := readFrame(bufio.NewReader(stream))
			batches <- received{body, err}
			if err != nil {
				return
			}
			_ = writeFrame(stream, body)
		})
		first := []byte("first batch")
		second := []byte("second batch")

		// After this beat, the heartbeat stream is open and idle, so the next
		// stream that opens is the metrics stream.
		_, err := raw.Heartbeat()
		require.NoError(t, err)
		gate.armed.Store(true)

		ctx := metricsContext(t, 200*time.Millisecond)
		errs := make(chan error, 1)
		go func() {
			_, err := raw.SendAgentMetrics(ctx, first)
			errs <- err
		}()
		select {
		case <-gate.held:
		case err := <-errs:
			t.Fatalf("SendAgentMetrics returned before its frame reached the transport: %v", err)
		}
		<-ctx.Done()

		// A call that gives up on the write at the deadline returns at once,
		// and its stream close reuses the frame that the transport holds. A
		// call that keeps the write stays blocked until the transport resumes.
		// The wait below only gives a wrong call time to return. A correct
		// call passes however long the wait is.
		select {
		case err := <-errs:
			close(gate.release)
			t.Fatalf("SendAgentMetrics returned while its write was stalled: %v", err)
		case <-time.After(100 * time.Millisecond):
		}
		close(gate.release)
		require.ErrorIs(t, <-errs, os.ErrDeadlineExceeded)

		wait := metricsContext(t, 10*time.Second)
		receive := func(want []byte) {
			select {
			case got := <-batches:
				require.NoError(t, got.err)
				require.Equal(t, want, got.body)
			case <-wait.Done():
				t.Fatalf("the server did not receive %q", want)
			}
		}
		receive(first)

		result, err := raw.SendAgentMetrics(metricsContext(t, time.Minute), second)
		require.NoError(t, err)
		require.Equal(t, second, result)
		receive(second)
	})

	t.Run("rejects a batch larger than the stream window", func(t *testing.T) {
		done := make(chan struct{})
		t.Cleanup(func() { close(done) })
		// The server accepts the stream but never reads it, so a write larger
		// than the window waits for a window update that never comes.
		raw := newAgentMetricsSession(t, func(muxado.TypedStream) { <-done })

		_, err := raw.SendAgentMetrics(metricsContext(t, time.Minute), make([]byte, maxAgentMetricsWriteBytes))
		require.Error(t, err)
	})

	t.Run("returns when the context ends", func(t *testing.T) {
		// The deadline is far away, so only the cancel can end the exchange.
		ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
		defer cancel()
		done := make(chan struct{})
		t.Cleanup(func() { close(done) })
		raw := newAgentMetricsSession(t, func(stream muxado.TypedStream) {
			// The server never answers. Cancel after the request arrives so
			// that the client is blocked on the read.
			_, _ = stream.Read(make([]byte, 1))
			cancel()
			<-done
		})

		_, err := raw.SendAgentMetrics(ctx, []byte{})
		require.ErrorIs(t, err, os.ErrDeadlineExceeded)
	})
}
