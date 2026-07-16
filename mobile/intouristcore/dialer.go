package intouristcore

import (
	"context"
	"fmt"
	"net"
	"syscall"
)

// newProtectedDialFunc returns a dial function that calls
// SocketProtector.Protect(fd) (== Android VpnService.protect()) on every
// socket right after it's created and before it connects. This is the
// direct equivalent of what the Windows bypass-route logic in
// internal/routemgr does at the OS routing-table level: keep the tunnel's
// own upstream connection(s) outside the tunnel.
//
// Without this, the bridge/proxy connection would get captured by our own
// VpnService TUN and deadlock (the tunnel trying to tunnel itself).
func newProtectedDialFunc(protector SocketProtector) func(ctx context.Context, network, address string) (net.Conn, error) {
	d := net.Dialer{
		Control: func(_, _ string, c syscall.RawConn) error {
			var protectErr error
			ctrlErr := c.Control(func(fd uintptr) {
				if !protector.Protect(int64(fd)) {
					protectErr = fmt.Errorf("VpnService.protect() rejected fd=%d", fd)
				}
			})
			if ctrlErr != nil {
				return ctrlErr
			}
			return protectErr
		},
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		return d.DialContext(ctx, network, address)
	}
}
