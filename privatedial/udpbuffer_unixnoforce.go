//go:build darwin || freebsd

package privatedial

import "syscall"

// On non-Linux unix platforms there is no privileged "force" option to exceed
// the kernel's buffer-size limit, matching quic-go's non-Linux
// forceSetReceiveBuffer / forceSetSendBuffer no-ops.

func forceSetReceiveUDPBuffer(syscall.RawConn, int) error { return nil }
func forceSetSendUDPBuffer(syscall.RawConn, int) error    { return nil }
