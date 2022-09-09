package muxado

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ngrok/libngrok-go/internal/muxado/frame"
)

var (
	zeroTime         time.Time
	resetRemoveDelay = 5 * time.Second
	closeError       = newErr(StreamClosed, errors.New("stream closed"))
)

const (
	halfClosedInbound  = 0x1
	halfClosedOutbound = 0x2
	fullyClosed        = 0x3
)

type stream struct {
	synOnce    uint32    // == 0 only if we should send a syn on the next data frame
	recvWindow uint32    // remaining space in the recv buffer
	resetOnce  sync.Once // == 1 only if we sent a reset to close this connection

	// just for embedding purposes to avoid heap alloc, use 'window' and 'buf'
	windowImpl condWindow
	bufImpl    inboundBuffer

	id             frame.StreamId // stream id (const)
	session        sessionPrivate // the parent session (const)
	buf            buffer         // buffer for data coming in from the remote side
	window         windowManager  // manages the outbound window
	writer         sync.Mutex     // only one writer at a time
	writeDeadline  time.Time      // deadline for writes (protected by writer mutex)
	windowSize     uint32         // max window size
	frData         frame.Data     // data frame used in writes
	halfCloseMutex sync.Mutex     // synchornizes access to half-close tracking state
	closedState    uint8          // used for determining when both in/out streams are closed
}

// private interface for Streams to call Sessions
type sessionPrivate interface {
	Session
	writeFrame(frame.Frame, time.Time) error
	writeFrameAsync(frame.Frame) error
	die(error) error
	removeStream(frame.StreamId)
}

////////////////////////////////
// public interface
////////////////////////////////
func newStream(sess sessionPrivate, id frame.StreamId, windowSize uint32, fin bool, init bool) streamPrivate {
	str := &stream{
		id:         id,
		session:    sess,
		windowSize: windowSize,
		recvWindow: windowSize,
	}
	if !init {
		str.synOnce = 1
	}
	str.windowImpl.Init(int(windowSize))
	str.window = &str.windowImpl
	str.bufImpl.Init(int(windowSize))
	str.buf = &str.bufImpl

	if fin {
		str.window.SetError(streamClosed)
	}
	return str
}

func (s *stream) Write(buf []byte) (n int, err error) {
	return s.write(buf, false)
}

func (s *stream) Read(buf []byte) (int, error) {
	// read from the buffer
	n, err := s.buf.Read(buf)
	if n > 0 {
		/*
			maxWinSize := s.windowSize
			recvWindow := atomic.AddUint32(&s.recvWindow, ^uint32(n-1))
			if recvWindow < maxWinSize/2 {
				if atomic.CompareAndSwapUint32(&s.recvWindow, recvWindow, maxWinSize) {
					s.sendWindowUpdate(maxWinSize - recvWindow)
				}
			}
		*/
		s.sendWindowUpdate(uint32(n))
	}
	return n, err
}

// Close closes the stream in a manner that attempts to emulate a net.Conn's Close():
// - It calls CloseWrite() to half-close the stream on the remote side
// - It calls closeWith() so that all future Read/Write operations will fail
// - If the stream receives another STREAM_DATA frame (except an empty one with a FIN)
//   from the remote side, it will send a STREAM_RST with a CANCELED error code
func (s *stream) Close() error {
	s.CloseWrite()
	s.closeWith(closeError)
	return nil
}

func (s *stream) SetDeadline(deadline time.Time) (err error) {
	if err = s.SetReadDeadline(deadline); err != nil {
		return
	}
	if err = s.SetWriteDeadline(deadline); err != nil {
		return
	}
	return
}

func (s *stream) SetReadDeadline(dl time.Time) error {
	s.buf.SetDeadline(dl)
	return nil
}

func (s *stream) SetWriteDeadline(dl time.Time) error {
	s.writer.Lock()
	s.writeDeadline = dl
	s.writer.Unlock()
	return nil
}

func (s *stream) CloseWrite() error {
	_, err := s.write([]byte{}, true)
	return err
}

func (s *stream) Id() uint32 {
	return uint32(s.id)
}

func (s *stream) Session() Session {
	return s.session
}

func (s *stream) LocalAddr() net.Addr {
	return s.session.LocalAddr()
}

func (s *stream) RemoteAddr() net.Addr {
	return s.session.RemoteAddr()
}

/////////////////////////////////////
// session's stream interface
/////////////////////////////////////
func (s *stream) handleStreamData(f *frame.Data) error {
	// skip writing for zero-length frames (typically for sending FIN)
	if f.Length() > 0 {
		// write the data into the buffer
		if _, err := s.buf.ReadFrom(f.Reader()); err != nil {
			if err == bufferFull {
				s.resetWith(FlowControlError, flowControlViolated)
			} else if err == closeError {
				// We're trying to emulate net.Conn's Close() behavior where we close our side of the connection,
				// and if we get any more frames from the other side, we RST it.
				s.resetWith(StreamClosed, streamClosed)
			} else if err == bufferClosed {
				// there was already an error set
				s.resetWith(StreamClosed, streamClosed)
			} else {
				// the transport returned some sort of IO error
				return err
			}
			return nil
		}
	}
	if f.Fin() {
		s.buf.SetError(io.EOF)
		s.maybeRemove(halfClosedInbound)
	}
	return nil
}

func (s *stream) handleStreamRst(f *frame.Rst) error {
	s.closeWith(newErr(ErrorCode(f.ErrorCode()), fmt.Errorf("Stream reset by peer with remote error code: %d", f.ErrorCode())))
	return nil
}

func (s *stream) handleStreamWndInc(f *frame.WndInc) error {
	s.window.Increment(int(f.WindowIncrement()))
	return nil
}

func (s *stream) closeWith(err error) {
	s.window.SetError(err)
	s.buf.SetError(err)
	s.removeFromSession()
}

////////////////////////////////
// internal methods
////////////////////////////////

func (s *stream) removeFromSession() {
	s.session.removeStream(s.id)
}

func (s *stream) closeWithAndRemoveLater(err error) {
	s.window.SetError(err)
	s.buf.SetError(err)
	time.AfterFunc(resetRemoveDelay, s.removeFromSession)
}

func (s *stream) maybeRemove(closeFlag uint8) {
	s.halfCloseMutex.Lock()
	s.closedState |= closeFlag
	remove := s.closedState == fullyClosed
	s.halfCloseMutex.Unlock()

	if remove {
		s.removeFromSession()
	}
}

func (s *stream) resetWith(errorCode ErrorCode, resetErr error) {
	// only ever send one reset
	s.resetOnce.Do(func() {
		// close the stream
		s.closeWithAndRemoveLater(resetErr)

		// make the reset frame
		rst := new(frame.Rst)
		if err := rst.Pack(s.id, frame.ErrorCode(errorCode)); err != nil {
			s.session.die(newErr(InternalError, fmt.Errorf("failed to pack RST frame: %v", err)))
			return
		}

		// need write lock to make sure no data frames get sent after we send the reset
		s.writer.Lock()
		defer s.writer.Unlock()

		// send it
		s.session.writeFrame(rst, zeroTime)
	})
}

func (s *stream) write(buf []byte, fin bool) (n int, err error) {
	var synFlag bool
	if atomic.CompareAndSwapUint32(&s.synOnce, 0, 1) {
		synFlag = true
	}

	// a write call can pass a buffer larger that we can send in a single frame
	// only allow one writer at a time to prevent interleaving frames from concurrent writes
	s.writer.Lock()

	bufSize := len(buf)
	bytesRemaining := bufSize
	for bytesRemaining > 0 || fin {
		// figure out the most we can write in a single frame
		writeReqSize := min(0x00FFFFFF, bytesRemaining)

		// and then reduce that to however much is available in the window
		// this blocks until window is available and may not return all that we asked for
		var writeSize int
		if writeSize, err = s.window.Decrement(writeReqSize); err != nil {
			s.writer.Unlock()
			return
		}

		// calculate the slice of the buffer we'll write
		start, end := n, n+writeSize

		// only send fin for the last frame
		finFlag := fin && end == bufSize

		// make the frame
		if err = s.frData.Pack(s.id, buf[start:end], finFlag, synFlag); err != nil {
			err = newErr(InternalError, fmt.Errorf("failed to pack DATA frame: %v", err))
			s.writer.Unlock()
			return
		}

		// write the frame
		if err = s.session.writeFrame(&s.frData, s.writeDeadline); err != nil {
			s.writer.Unlock()
			return
		}

		// update our counts
		n += writeSize
		bytesRemaining -= writeSize

		if finFlag {
			s.window.SetError(streamClosed)
			s.maybeRemove(halfClosedOutbound)

			// handles the empty buffer with fin case
			fin = false
		}
		synFlag = false
	}

	s.writer.Unlock()
	return
}

// sendWindowUpdate sends a window increment frame
// with the given increment
func (s *stream) sendWindowUpdate(inc uint32) {
	// send a window update
	var wndinc frame.WndInc
	if err := wndinc.Pack(s.id, inc); err != nil {
		s.session.die(newErr(InternalError, fmt.Errorf("failed to pack WNDINC frame: %v", err)))
		return
	}
	s.session.writeFrameAsync(&wndinc)
}

func min(n1, n2 int) int {
	if n1 > n2 {
		return n2
	} else {
		return n1
	}
}
