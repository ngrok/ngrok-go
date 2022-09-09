package muxado

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"testing"
	"time"

	"github.com/ngrok/libngrok-go/internal/muxado/frame"
)

func newFakeStream(sess sessionPrivate, id frame.StreamId, windowSize uint32, fin bool, init bool) streamPrivate {
	return &fakeStream{sess, id}
}

type fakeStream struct {
	sess     sessionPrivate
	streamId frame.StreamId
}

func (s *fakeStream) Write([]byte) (int, error)              { return 0, nil }
func (s *fakeStream) Read([]byte) (int, error)               { return 0, nil }
func (s *fakeStream) Close() error                           { return nil }
func (s *fakeStream) SetDeadline(time.Time) error            { return nil }
func (s *fakeStream) SetReadDeadline(time.Time) error        { return nil }
func (s *fakeStream) SetWriteDeadline(time.Time) error       { return nil }
func (s *fakeStream) CloseWrite() error                      { return nil }
func (s *fakeStream) Id() uint32                             { return uint32(s.streamId) }
func (s *fakeStream) Session() Session                       { return s.sess }
func (s *fakeStream) RemoteAddr() net.Addr                   { return nil }
func (s *fakeStream) LocalAddr() net.Addr                    { return nil }
func (s *fakeStream) handleStreamData(*frame.Data) error     { return nil }
func (s *fakeStream) handleStreamWndInc(*frame.WndInc) error { return nil }
func (s *fakeStream) handleStreamRst(*frame.Rst) error       { return nil }
func (s *fakeStream) closeWith(error)                        {}

type fakeConn struct {
	in     *io.PipeReader
	out    *io.PipeWriter
	closed bool
}

func (c *fakeConn) SetDeadline(time.Time) error      { return nil }
func (c *fakeConn) SetReadDeadline(time.Time) error  { return nil }
func (c *fakeConn) SetWriteDeadline(time.Time) error { return nil }
func (c *fakeConn) LocalAddr() net.Addr              { return nil }
func (c *fakeConn) RemoteAddr() net.Addr             { return nil }
func (c *fakeConn) Close() error                     { c.closed = true; c.in.Close(); return c.out.Close() }
func (c *fakeConn) Read(p []byte) (int, error)       { return c.in.Read(p) }
func (c *fakeConn) Write(p []byte) (int, error)      { return c.out.Write(p) }
func (c *fakeConn) Discard()                         { go io.Copy(ioutil.Discard, c.in) }

func newFakeConnPair() (local *fakeConn, remote *fakeConn) {
	local, remote = new(fakeConn), new(fakeConn)
	local.in, remote.out = io.Pipe()
	remote.in, local.out = io.Pipe()
	return
}

var debugFramer = func(name string) func(io.Reader, io.Writer) frame.Framer {
	return func(rd io.Reader, wr io.Writer) frame.Framer {
		return frame.NewNamedDebugFramer(name, os.Stdout, frame.NewFramer(rd, wr))
	}
}

func TestWrongClientParity(t *testing.T) {
	t.Parallel()
	local, remote := newFakeConnPair()
	// don't need the remote output
	remote.Discard()
	s := Server(local, &Config{newStream: newFakeStream})

	// 300 is even, and only servers send even stream ids
	f := new(frame.Data)
	f.Pack(300, []byte{}, false, true)

	// send the frame into the session
	fr := frame.NewFramer(remote, remote)
	fr.WriteFrame(f)

	// wait for failure
	err, _, _ := s.Wait()

	if code, _ := GetError(err); code != ProtocolError {
		t.Errorf("Session not terminated with protocol error. Got %d, expected %d. Session error: %v", code, ProtocolError, err)
	}

	if !local.closed {
		t.Errorf("Session transport not closed after protocol failure.")
	}
}

func TestWrongServerParity(t *testing.T) {
	t.Parallel()

	local, remote := newFakeConnPair()
	s := Client(local, &Config{newStream: newFakeStream})

	// don't need the remote output
	remote.Discard()

	// 301 is odd, and only clients send even stream ids
	f := new(frame.Data)
	f.Pack(301, []byte{}, false, true)

	// send the frame into the session
	fr := frame.NewFramer(remote, remote)
	fr.WriteFrame(f)

	// wait for failure
	err, _, _ := s.Wait()

	if code, _ := GetError(err); code != ProtocolError {
		t.Errorf("Session not terminated with protocol error. Got %d, expected %d. Session error: %v", code, ProtocolError, err)
	}

	if !local.closed {
		t.Errorf("Session transport not closed after protocol failure.")
	}
}

func TestAcceptStream(t *testing.T) {
	t.Parallel()

	local, remote := newFakeConnPair()

	// don't need the remote output
	remote.Discard()

	s := Client(local, &Config{newStream: newFakeStream})
	defer s.Close()

	f := new(frame.Data)
	f.Pack(300, []byte{}, false, true)

	// send the frame into the session
	fr := frame.NewFramer(remote, remote)
	fr.WriteFrame(f)

	done := make(chan int)
	go func() {
		defer func() { done <- 1 }()

		// wait for accept
		str, err := s.AcceptStream()

		if err != nil {
			t.Errorf("Error accepting stream: %v", err)
			return
		}

		if str.Id() != 300 {
			t.Errorf("Stream has wrong id. Expected %d, got %d", str.Id(), 300)
		}
	}()

	select {
	case <-time.After(time.Second):
		t.Fatalf("Timed out!")
	case <-done:
	}
}

// validate that a session fulfills the net.Listener interface
// compile-only check
func TestNetListener(t *testing.T) {
	if false {
		var _ net.Listener = Server(nil, nil)
	}
}

// Test for the Close() behavior
// Close() issues a data frame with the fin flag
// if any further data is received from the remote side, then RST is sent
func TestWriteAfterClose(t *testing.T) {
	t.Parallel()
	local, remote := newFakeConnPair()
	sLocal := Server(local, &Config{NewFramer: debugFramer("SERVER")})
	sRemote := Client(remote, &Config{NewFramer: debugFramer("CLIENT")})

	closed := make(chan int)
	go func() {
		stream, err := sRemote.Open()
		if err != nil {
			t.Errorf("Failed to open stream: %v", err)
			return
		}
		stream.Write([]byte("hello local"))
		defer sRemote.Close()

		<-closed
		// this write should succeed
		if _, err = stream.Write([]byte("test!")); err != nil {
			t.Errorf("Failed to write test data: %v", err)
			return
		}

		// give the remote end some time to send us an RST
		time.Sleep(200 * time.Millisecond)

		// this write should fail
		if _, err = stream.Write([]byte("test!")); err == nil {
			fmt.Println("WROTE FRAME WITHOUT ERROR")
			t.Errorf("expected error, but not did not receive one")
			return
		}
	}()

	stream, err := sLocal.Accept()
	if err != nil {
		t.Fatalf("Failed to accept stream!")
	}

	// tell the other side that we closed so they can write late
	stream.Close()
	closed <- 1

	err, remoteErr, debug := sLocal.Wait()
	if code, _ := GetError(err); code != PeerEOF {
		t.Fatalf("session closed with error: %v, expected PeerEOF", err)
	}
	remoteCode, _ := GetError(remoteErr)
	if remoteCode != NoError {
		t.Fatalf("remote session closed with error code: %v, expected NoError (debug: %s)", remoteCode, debug)
	}
}
