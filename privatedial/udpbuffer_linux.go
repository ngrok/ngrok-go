//go:build linux

package privatedial

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// forceSetReceiveUDPBuffer / forceSetSendUDPBuffer are ported from quic-go's
// forceSetReceiveBuffer / forceSetSendBuffer (sys_conn_helper_linux.go, as of
// github.com/quic-go/quic-go v0.59.1, MIT-licensed — see udpbuffer_unix.go for
// the attribution): on Linux a process with CAP_NET_ADMIN can exceed
// net.core.{r,w}mem_max via SO_{RCV,SND}BUFFORCE.

func forceSetReceiveUDPBuffer(sc syscall.RawConn, bytes int) error {
	var serr error
	if err := sc.Control(func(fd uintptr) {
		serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_RCVBUFFORCE, bytes)
	}); err != nil {
		return err
	}
	return serr
}

func forceSetSendUDPBuffer(sc syscall.RawConn, bytes int) error {
	var serr error
	if err := sc.Control(func(fd uintptr) {
		serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_SNDBUFFORCE, bytes)
	}); err != nil {
		return err
	}
	return serr
}
