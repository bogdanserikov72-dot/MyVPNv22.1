package intouristcore

import (
	"bufio"
	"fmt"
	"io"
	"net"
)

// socks5Handler implements RFC 1928 SOCKS5 protocol wrapper.
// It accepts a SOCKS5-speaking client, validates the handshake,
// and then hands the authenticated socket to the stream handler.
func handleSOCKS5Conn(conn net.Conn, handleStream func(net.Conn) error) error {
	defer conn.Close()
	br := bufio.NewReader(conn)

	// ============ SOCKS5 Greeting ============
	// Client sends: [VER | NMETHODS | METHODS...]
	// VER = 5 (SOCKS5), NMETHODS = number of methods, METHODS = auth method IDs
	ver := make([]byte, 2)
	if _, err := io.ReadFull(br, ver); err != nil {
		return fmt.Errorf("read SOCKS5 greeting: %w", err)
	}
	if ver[0] != 5 {
		return fmt.Errorf("invalid SOCKS version: %d (expected 5)", ver[0])
	}

	nmethods := int(ver[1])
	if nmethods < 1 || nmethods > 255 {
		return fmt.Errorf("invalid number of methods: %d", nmethods)
	}

	methods := make([]byte, nmethods)
	if _, err := io.ReadFull(br, methods); err != nil {
		return fmt.Errorf("read SOCKS5 methods: %w", err)
	}

	// Server response: [VER | METHOD]
	// METHOD = 0 (no authentication required)
	if _, err := conn.Write([]byte{5, 0}); err != nil {
		return fmt.Errorf("write SOCKS5 greeting response: %w", err)
	}

	// ============ SOCKS5 Request ============
	// Client sends: [VER | CMD | RSV | ATYP | DST.ADDR | DST.PORT]
	// VER = 5, CMD = 1 (CONNECT), RSV = 0, ATYP = address type
	req := make([]byte, 4)
	if _, err := io.ReadFull(br, req); err != nil {
		return fmt.Errorf("read SOCKS5 request header: %w", err)
	}
	if req[0] != 5 {
		return fmt.Errorf("invalid SOCKS5 version in request: %d", req[0])
	}

	cmd := req[1]
	if cmd != 1 { // 1 = CONNECT (only command we support)
		// Send command not supported error
		conn.Write([]byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0})
		return fmt.Errorf("unsupported SOCKS5 command: %d (only CONNECT=1 is supported)", cmd)
	}

	// req[2] is reserved (must be 0)
	atyp := req[3]

	// Parse destination address based on type
	var addr string
	var port uint16

	switch atyp {
	case 1: // IPv4 address: 4 bytes
		ipv4 := make([]byte, 4)
		if _, err := io.ReadFull(br, ipv4); err != nil {
			return fmt.Errorf("read IPv4 address: %w", err)
		}
		portBytes := make([]byte, 2)
		if _, err := io.ReadFull(br, portBytes); err != nil {
			return fmt.Errorf("read port for IPv4: %w", err)
		}
		addr = net.IPv4(ipv4[0], ipv4[1], ipv4[2], ipv4[3]).String()
		port = uint16(portBytes[0])<<8 | uint16(portBytes[1])

	case 3: // Domain name: 1-byte length + domain bytes
		lenByte := make([]byte, 1)
		if _, err := io.ReadFull(br, lenByte); err != nil {
			return fmt.Errorf("read domain name length: %w", err)
		}
		domainLen := int(lenByte[0])
		if domainLen == 0 {
			conn.Write([]byte{5, 8, 0, 1, 0, 0, 0, 0, 0, 0})
			return fmt.Errorf("empty domain name")
		}
		domainBytes := make([]byte, domainLen)
		if _, err := io.ReadFull(br, domainBytes); err != nil {
			return fmt.Errorf("read domain name: %w", err)
		}
		portBytes := make([]byte, 2)
		if _, err := io.ReadFull(br, portBytes); err != nil {
			return fmt.Errorf("read port for domain: %w", err)
		}
		addr = string(domainBytes)
		port = uint16(portBytes[0])<<8 | uint16(portBytes[1])

	case 4: // IPv6 address: 16 bytes
		ipv6 := make([]byte, 16)
		if _, err := io.ReadFull(br, ipv6); err != nil {
			return fmt.Errorf("read IPv6 address: %w", err)
		}
		portBytes := make([]byte, 2)
		if _, err := io.ReadFull(br, portBytes); err != nil {
			return fmt.Errorf("read port for IPv6: %w", err)
		}
		addr = net.IP(ipv6).String()
		port = uint16(portBytes[0])<<8 | uint16(portBytes[1])

	default:
		// Send address type not supported error
		conn.Write([]byte{5, 8, 0, 1, 0, 0, 0, 0, 0, 0})
		return fmt.Errorf("unsupported SOCKS5 address type: %d", atyp)
	}

	// ============ SOCKS5 Response ============
	// Server sends: [VER | REP | RSV | ATYP | BND.ADDR | BND.PORT]
	// REP = 0 (success), ATYP = 1 (IPv4), BND.ADDR/PORT = 0
	// We're accepting any address; send generic success
	if _, err := conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return fmt.Errorf("write SOCKS5 response: %w", err)
	}

	// ============ Hand off to stream handler ============
	// The socket is now authenticated and established.
	// The remaining data on the socket (any buffered in br) plus
	// the underlying conn goes to the stream handler for bridge protocol.
	//
	// NOTE: If there was data buffered in br beyond the SOCKS5
	// handshake, it would be lost here. In practice, clients don't
	// send payload before receiving the success response, so this
	// is safe. If needed, we could wrap conn with br to preserve buffering.
	return handleStream(conn)
}
