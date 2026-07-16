package wsapi

import (
	"context"
	"net"
)

// Client sends data to WebSocket connections via the YC management API.
type Client interface {
	Send(connectionID string, data []byte, dataType string, iamToken string) error
	Disconnect(connectionID string, iamToken string) error
}

// NewClient creates a gRPC client (only gRPC supported in v4).
func NewClient() Client {
	return &grpcClient{}
}

// NewClientWithDialer creates a gRPC client whose underlying connection is
// established via dial instead of gRPC's default dialer — e.g. one that
// calls Android's VpnService.protect() on the socket so it isn't captured
// by our own TUN. Added for the Android/gomobile port.
func NewClientWithDialer(dial func(ctx context.Context, addr string) (net.Conn, error)) Client {
	return &grpcClient{dialContext: dial}
}
