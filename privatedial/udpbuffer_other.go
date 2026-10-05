//go:build !(linux || darwin || freebsd)

package privatedial

// probeQUICBuffers is a no-op on platforms where we don't inspect UDP socket
// buffer sizes. It reports success so protocol selection behaves as it did
// before the probe was added (QUIC is raced normally).
func probeQUICBuffers() error { return nil }
