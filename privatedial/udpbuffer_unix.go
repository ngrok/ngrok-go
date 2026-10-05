//go:build linux || darwin || freebsd

// The UDP-buffer probe in this file is adapted from quic-go's
// setReceiveBuffer / setSendBuffer (sys_conn_buffers.go,
// sys_conn_buffers_write.go) and inspectReadBuffer / inspectWriteBuffer
// (sys_conn_oob.go), as of github.com/quic-go/quic-go v0.59.1. We can't call
// those functions because they're unexported, so the inspect/grow/force
// algorithm and the error-message strings are reproduced here. quic-go is
// MIT-licensed:
//
//	Copyright (c) 2016 the quic-go authors & Google, Inc.
//
// See the full license at https://github.com/quic-go/quic-go/blob/master/LICENSE.md.

package privatedial

import (
	"fmt"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

// desiredUDPBufferSize mirrors quic-go's protocol.DesiredReceiveBufferSize and
// protocol.DesiredSendBufferSize (7 MiB). quic-go tries to grow a QUIC socket's
// kernel receive and send buffers to this size and logs a warning if it can't;
// a connection with smaller buffers is degraded. We probe for the same
// condition before racing so we can quietly fall back to HTTP/2 instead.
const desiredUDPBufferSize = (1 << 20) * 7

// probeQUICBuffers opens a throwaway UDP socket and replicates quic-go's
// setReceiveBuffer/setSendBuffer logic on it, returning the first error either
// would report. A nil result means QUIC would get the buffer sizes it wants;
// any error means a QUIC connection would be degraded (and quic-go would log a
// warning), so the caller should force HTTP/2.
func probeQUICBuffers() error {
	pc, err := net.ListenUDP("udp", nil)
	if err != nil {
		return fmt.Errorf("probe udp socket: %w", err)
	}
	defer pc.Close()

	sc, err := pc.SyscallConn()
	if err != nil {
		return fmt.Errorf("probe udp syscall conn: %w", err)
	}

	if err := checkUDPBuffer(sc, pc.SetReadBuffer, unix.SO_RCVBUF, forceSetReceiveUDPBuffer, "receive"); err != nil {
		return err
	}
	return checkUDPBuffer(sc, pc.SetWriteBuffer, unix.SO_SNDBUF, forceSetSendUDPBuffer, "send")
}

// checkUDPBuffer mirrors quic-go's setReceiveBuffer/setSendBuffer for a single
// buffer: inspect the current size, try to grow it to the desired size (with a
// privileged force attempt as a fallback, on platforms that support it), and
// report whether the desired size was reached.
func checkUDPBuffer(
	sc syscall.RawConn,
	set func(int) error,
	inspectOpt int,
	force func(syscall.RawConn, int) error,
	kind string,
) error {
	size, err := inspectUDPBuffer(sc, inspectOpt)
	if err != nil {
		return fmt.Errorf("failed to determine %s buffer size: %w", kind, err)
	}
	if size >= desiredUDPBufferSize {
		return nil
	}
	// Ignore the error. We check if we succeeded by querying the buffer size afterward.
	_ = set(desiredUDPBufferSize)
	newSize, err := inspectUDPBuffer(sc, inspectOpt)
	if newSize < desiredUDPBufferSize {
		// Try again with the privileged force option (a no-op where unsupported).
		_ = force(sc, desiredUDPBufferSize)
		newSize, err = inspectUDPBuffer(sc, inspectOpt)
		if err != nil {
			return fmt.Errorf("failed to determine %s buffer size: %w", kind, err)
		}
	}
	if err != nil {
		return fmt.Errorf("failed to determine %s buffer size: %w", kind, err)
	}
	if newSize == size {
		return fmt.Errorf("failed to increase %s buffer size (wanted: %d kiB, got %d kiB)", kind, desiredUDPBufferSize/1024, newSize/1024)
	}
	if newSize < desiredUDPBufferSize {
		return fmt.Errorf("failed to sufficiently increase %s buffer size (was: %d kiB, wanted: %d kiB, got: %d kiB)", kind, size/1024, desiredUDPBufferSize/1024, newSize/1024)
	}
	return nil
}

// inspectUDPBuffer reads the current size of the socket buffer named by opt
// (unix.SO_RCVBUF or unix.SO_SNDBUF), mirroring quic-go's inspectReadBuffer /
// inspectWriteBuffer.
func inspectUDPBuffer(sc syscall.RawConn, opt int) (int, error) {
	var size int
	var serr error
	if err := sc.Control(func(fd uintptr) {
		size, serr = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, opt)
	}); err != nil {
		return 0, err
	}
	return size, serr
}
