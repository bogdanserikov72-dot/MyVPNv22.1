package intouristcore

import (
	"context"
	"net"

	"github.com/xtls/xray-core/transport/internet"
)

// registerProtectedDialer wires our VpnService.protect()-aware dial function
// into xray-core's outbound socket creation path.
//
// Uses internet.WithAdapter(), which wraps a minimal
// Dial(network, address string) (net.Conn, error) adapter into the full
// internet.SystemDialer xray-core expects internally (which itself needs
// DestIpAddress() and xray-core's own net.Address/net.Destination types —
// not worth hand-implementing when the adapter form covers exactly what we
// need: routing every outbound socket through a Control hook that calls
// protector.Protect() before connecting).
func registerProtectedDialer(dial func(ctx context.Context, network, address string) (net.Conn, error)) {
	internet.UseAlternativeSystemDialer(internet.WithAdapter(&protectedDialerAdapter{dial: dial}))
}

type protectedDialerAdapter struct {
	dial func(ctx context.Context, network, address string) (net.Conn, error)
}

func (p *protectedDialerAdapter) Dial(network, address string) (net.Conn, error) {
	return p.dial(context.Background(), network, address)
}
