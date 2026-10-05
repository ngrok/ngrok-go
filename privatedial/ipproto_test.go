package privatedial

import "testing"

func TestIPProtocolNetworks(t *testing.T) {
	for _, tc := range []struct {
		p      IPProtocol
		str    string
		tcpNet string
		udpNet string
	}{
		{IPProtocolAny, "any", "tcp", "udp"},
		{IPProtocolV4, "ipv4", "tcp4", "udp4"},
		{IPProtocolV6, "ipv6", "tcp6", "udp6"},
	} {
		if got := tc.p.String(); got != tc.str {
			t.Errorf("%d.String() = %q, want %q", int(tc.p), got, tc.str)
		}
		if got := tc.p.tcpNetwork(); got != tc.tcpNet {
			t.Errorf("%v.tcpNetwork() = %q, want %q", tc.p, got, tc.tcpNet)
		}
		if got := tc.p.udpNetwork(); got != tc.udpNet {
			t.Errorf("%v.udpNetwork() = %q, want %q", tc.p, got, tc.udpNet)
		}
	}
}
