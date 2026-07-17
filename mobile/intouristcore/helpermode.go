package intouristcore

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	tun2socks "github.com/xjasonlyu/tun2socks/v2/engine"
	"gopkg.in/yaml.v3"

	"github.com/bridge-to-freedom/adapter/pkg/config"
	"github.com/bridge-to-freedom/adapter/pkg/protocol"
	"github.com/bridge-to-freedom/adapter/pkg/streams"
	"github.com/bridge-to-freedom/adapter/pkg/upstream"
	"github.com/bridge-to-freedom/adapter/pkg/wsapi"
)

// This file requires the small additions described in
// adapter-patches/PATCH_NOTES.md applied to your internal/upstream/upstream.go
// and internal/wsapi/{wsapi,grpc}.go: Upstream.SetNetDialContext and
// wsapi.NewClientWithDialer. Both are purely additive (new methods/funcs),
// nothing existing changes shape, so cmd/helper and cmd/adapter keep
// building unmodified.

var (
	helperMu      sync.Mutex
	helperLn      net.Listener
	helperTunFile *os.File
	helperT2SStop func()
	helperUps     *upstream.Upstream
)

// runHelperMode is the Android port of cmd/helper/main.go — same protocol,
// same streams/upstream/wsapi packages, running as goroutines in this
// process instead of as a standalone binary. tun2socks replaces the
// TCP-listener-facing side of tun2socks.exe: instead of a real network
// socket, it routes the VpnService TUN fd straight into a local raw-TCP
// listener that speaks the exact same "opaque byte stream over OPEN/DATA"
// protocol helper.exe's accept loop always spoke.
func runHelperMode(ctx context.Context, configYAML string, tunFd int64, s LogSink, protector SocketProtector) error {
	helperMu.Lock()
	defer helperMu.Unlock()

	var cfg config.Config
	if err := yaml.Unmarshal([]byte(configYAML), &cfg); err != nil {
		return fmt.Errorf("parse helper.config.yaml: %w", err)
	}
	if cfg.Bridge.URL == "" {
		return fmt.Errorf("bridge.url not found in config; expected helper.config.yaml bridge.url or equivalent")
	}
	listenAddr := cfg.Listen.Address
	if listenAddr == "" {
		listenAddr = "127.0.0.1:1080"
	}

	// Forward this package's log.Printf output (used throughout the ported
	// streams/upstream/wsapi packages, and below) to the Kotlin-side LogSink
	// instead of stderr, which isn't visible in logcat from a gomobile .so.
	installLogForwarder(s)

	protectedDial := newProtectedDialFunc(protector)

	// 1) Local multiplexer listener — identical role to helper.exe's
	// net.Listen in cmd/helper/main.go.
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", listenAddr, err)
	}
	helperLn = ln
	s.OnLog(fmt.Sprintf("[INFO] helper: listening on %s", listenAddr))

	wsClient := wsapi.NewClientWithDialer(func(dctx context.Context, addr string) (net.Conn, error) {
		return protectedDial(dctx, "tcp", addr)
	})
	relay := cfg.WsAPI.Relay

	var ups *upstream.Upstream

	var pendingMu sync.Mutex
	pendingOpens := make(map[uint32]chan protocol.Frame)

	var probeMu sync.Mutex
	probeChans := make(map[uint32]chan protocol.Frame)

	cancelPendingOpens := func(reason string) {
		pendingMu.Lock()
		count := len(pendingOpens)
		for sid, ch := range pendingOpens {
			close(ch)
			delete(pendingOpens, sid)
		}
		pendingMu.Unlock()
		if count > 0 {
			s.OnLog(fmt.Sprintf("[INFO] cancelled %d pending opens: %s", count, reason))
		}
	}

	sm := streams.NewManager(func(data []byte) error {
		if relay {
			return ups.Send(data)
		}
		peerID := ups.PeerConnID()
		token := ups.IAMToken()
		if peerID == "" || token == "" {
			return fmt.Errorf("no peer connected")
		}
		err := wsClient.Send(peerID, data, "BINARY", token)
		if err != nil {
			ups.MarkPeerStale()
		}
		return err
	})
	sm.CoalesceDelay = cfg.CoalesceDelay()

	ups = upstream.New(&cfg, func(f protocol.Frame) {
		switch f.Type {
		case protocol.MsgPeerConn:
			peerID, iamToken, _, err := protocol.DecodePeerConn(f.Payload)
			if err != nil {
				s.OnLog(fmt.Sprintf("[WARN] bad PEER_CONN: %v", err))
				return
			}
			if ups.IsStaleConnID(peerID) {
				s.OnLog(fmt.Sprintf("[WARN] PEER_CONN with stale ID %s, ignoring", peerID))
				return
			}
			ups.ClearStaleConnID()
			cancelPendingOpens("new peer connected")
			ups.SetPeerConnID(peerID)
			if iamToken != "" {
				ups.SetIAMToken(iamToken)
			}
		case protocol.MsgPeerGone:
			s.OnLog(fmt.Sprintf("[INFO] PEER_GONE received, closing %d streams", sm.Count()))
			ups.SetPeerConnID("")
			cancelPendingOpens("peer gone")
			sm.CloseAll()
		case protocol.MsgPong:
			iamToken, err := protocol.DecodePong(f.Payload)
			if err != nil {
				s.OnLog(fmt.Sprintf("[WARN] bad PONG: %v", err))
				return
			}
			ups.SetIAMToken(iamToken)
		case protocol.MsgOpenOK, protocol.MsgOpenFail:
			pendingMu.Lock()
			ch, ok := pendingOpens[f.StreamID]
			pendingMu.Unlock()
			if ok {
				ch <- f
			}
		case protocol.MsgData:
			if protocol.IsProbe(f.StreamID) {
				deliverProbeFrame(&probeMu, probeChans, f)
				return
			}
			sm.HandleData(f.StreamID, f.Payload)
		case protocol.MsgFin:
			if protocol.IsProbe(f.StreamID) {
				deliverProbeFrame(&probeMu, probeChans, f)
				return
			}
			sm.HandleFin(f.StreamID)
		case protocol.MsgRst:
			if protocol.IsProbe(f.StreamID) {
				deliverProbeFrame(&probeMu, probeChans, f)
				return
			}
			sm.HandleRst(f.StreamID)
		default:
			s.OnLog(fmt.Sprintf("[WARN] unknown frame type=0x%02x stream=%d", f.Type, f.StreamID))
		}
	})
	ups.SetNetDialContext(protectedDial)
	helperUps = ups

	go ups.Run(ctx)
	go runProbe(ctx, s, ups, sm, &pendingMu, pendingOpens, &probeMu, probeChans, relay)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				s.OnLog(fmt.Sprintf("[WARN] helper: accept error: %v", err))
				continue
			}
			go handleConn(ctx, s, conn, ups, sm, &pendingMu, pendingOpens, relay)
		}
	}()

	// 2) Route the VpnService TUN fd into the local listener above, exactly
	// like sub-mode routes it into xray's SOCKS inbound (see submode.go).
	f := os.NewFile(uintptr(tunFd), "tun")
	if f == nil {
		stopHelperModeLocked()
		return fmt.Errorf("os.NewFile failed for tunFd=%d", tunFd)
	}
	helperTunFile = f

	tun2socks.Insert(&tun2socks.Key{
		MTU:      1500,
		Proxy:    fmt.Sprintf("socks5://%s", listenAddr),
		LogLevel: "info",
		Device:   fmt.Sprintf("fd://%d", tunFd),
	})
	tun2socks.Start()
	helperT2SStop = tun2socks.Stop
	s.OnLog(fmt.Sprintf("[INFO] helper: tun2socks routing TUN -> %s", listenAddr))

	go func() {
		<-ctx.Done()
		stopHelperMode()
	}()

	return nil
}

// handleConn processes an inbound SOCKS5 connection from tun2socks.
// This is the critical piece: tun2socks speaks RFC 1928 SOCKS5, and we must
// parse that to extract the destination address before creating an upstream
// stream to the bridge.
//
// Flow:
// 1. Parse SOCKS5 greeting + CONNECT request from inbound conn
// 2. Extract destination host:port
// 3. Send SOCKS5 success response to client
// 4. Send upstream OPEN frame with destination as payload
// 5. Wait for OPEN_OK/OPEN_FAIL response
// 6. Hand the now-established stream to sm.ReadLoop() for data forwarding
func handleConn(ctx context.Context, s LogSink, conn net.Conn, ups *upstream.Upstream, sm *streams.Manager, pendingMu *sync.Mutex, pendingOpens map[uint32]chan protocol.Frame, relay bool) {
	if tc, ok := conn.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
	}

	if !waitForPeer(ctx, ups, relay, 10*time.Second) {
		s.OnLog(fmt.Sprintf("[WARN] no peer available, closing inbound from %s", conn.RemoteAddr()))
		conn.Close()
		return
	}

	// Parse SOCKS5 handshake (greeting + CONNECT request)
	socksReq, err := parseSocks5Connect(conn)
	if err != nil {
		s.OnLog(fmt.Sprintf("[WARN] SOCKS5 parse error from %s: %v", conn.RemoteAddr(), err))
		sendSocks5Error(conn, 1) // general SOCKS server failure
		conn.Close()
		return
	}

	// Send SOCKS5 success response. Format: [VER=5 | REP=0 | RSV=0 | ATYP | ADDR | PORT]
	// We're proxying (not binding), so we return the original destination as BND.ADDR/BND.PORT.
	resp := []byte{0x05, 0x00, 0x00} // VER=5, REP=success, RSV=0
	resp = append(resp, socksReq.DestAddrBytes...)
	if _, err := conn.Write(resp); err != nil {
		s.OnLog(fmt.Sprintf("[WARN] SOCKS5 response write failed: %v", err))
		conn.Close()
		return
	}

	// Now create an upstream stream for this connection.
	shortID := ups.HelperShortID()
	localID := sm.NextID() & protocol.StreamLocalIDMask
	sid := (uint32(shortID) << protocol.StreamHelperShortIDShift) | localID

	str := &streams.Stream{ID: sid, Conn: conn}

	ch := make(chan protocol.Frame, 1)
	pendingMu.Lock()
	pendingOpens[sid] = ch
	pendingMu.Unlock()
	defer func() {
		pendingMu.Lock()
		delete(pendingOpens, sid)
		pendingMu.Unlock()
	}()

	// Send OPEN frame with destination host:port as payload.
	// The upstream (adapter/helper) will parse this and initiate the actual
	// connection to the target server.
	target := fmt.Sprintf("%s:%d", socksReq.DestHost, socksReq.DestPort)
	openPayload := []byte(target)
	if err := sm.SendFrame(protocol.Frame{Type: protocol.MsgOpen, StreamID: sid, Payload: openPayload}); err != nil {
		conn.Close()
		return
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			conn.Close()
			return
		}
		if resp.Type == protocol.MsgOpenFail {
			s.OnLog(fmt.Sprintf("[WARN] upstream rejected OPEN for %s: %v", target, string(resp.Payload)))
			conn.Close()
			return
		}
	case <-time.After(30 * time.Second):
		s.OnLog(fmt.Sprintf("[WARN] OPEN_OK timeout for %s", target))
		conn.Close()
		return
	case <-ctx.Done():
		conn.Close()
		return
	}

	sm.Register(str)
	sm.ReadLoop(str)
}

// socks5Request holds parsed SOCKS5 CONNECT request information
type socks5Request struct {
	DestHost      string // resolved hostname or IP
	DestPort      uint16 // destination port
	DestAddrBytes []byte // raw SOCKS5 response address bytes (ATYP + ADDR + PORT)
}

// parseSocks5Connect parses an RFC 1928 SOCKS5 handshake from a connection.
// It reads:
// 1. Greeting: [VER=5 | NMETHODS | METHODS...]
// 2. Request: [VER=5 | CMD=1 | RSV=0 | ATYP | DST.ADDR | DST.PORT]
//
// Returns a socks5Request with the destination or an error if the handshake
// is invalid or doesn't request CONNECT (CMD=1).
func parseSocks5Connect(conn net.Conn) (*socks5Request, error) {
	br := bufio.NewReader(conn)

	// ============ SOCKS5 Greeting ============
	// Client sends: [VER | NMETHODS | METHODS...]
	// VER = 5 (SOCKS5), NMETHODS = number of methods, METHODS = auth method IDs
	ver := make([]byte, 2)
	if _, err := io.ReadFull(br, ver); err != nil {
		return nil, fmt.Errorf("read SOCKS5 greeting: %w", err)
	}
	if ver[0] != 0x05 {
		return nil, fmt.Errorf("invalid SOCKS version: %d (expected 5)", ver[0])
	}

	nmethods := int(ver[1])
	if nmethods < 1 || nmethods > 255 {
		return nil, fmt.Errorf("invalid number of methods: %d", nmethods)
	}

	methods := make([]byte, nmethods)
	if _, err := io.ReadFull(br, methods); err != nil {
		return nil, fmt.Errorf("read SOCKS5 methods: %w", err)
	}

	// Server response: [VER=5 | METHOD=0 (no auth)]
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return nil, fmt.Errorf("write SOCKS5 greeting response: %w", err)
	}

	// ============ SOCKS5 Request ============
	// Client sends: [VER | CMD | RSV | ATYP | DST.ADDR | DST.PORT]
	// VER = 5, CMD = 1 (CONNECT), RSV = 0, ATYP = address type
	req := make([]byte, 4)
	if _, err := io.ReadFull(br, req); err != nil {
		return nil, fmt.Errorf("read SOCKS5 request header: %w", err)
	}
	if req[0] != 0x05 {
		return nil, fmt.Errorf("invalid SOCKS5 version in request: %d", req[0])
	}

	cmd := req[1]
	if cmd != 0x01 { // 1 = CONNECT (only command we support)
		// Send command not supported error (REP=7)
		conn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return nil, fmt.Errorf("unsupported SOCKS5 command: %d (only CONNECT=1 is supported)", cmd)
	}

	// req[2] is reserved (must be 0), req[3] is ATYP
	atyp := req[3]

	// Parse destination address based on type and build response address bytes
	var host string
	var port uint16
	var addrResp []byte

	switch atyp {
	case 0x01: // IPv4 address: 4 bytes
		ipv4 := make([]byte, 4)
		if _, err := io.ReadFull(br, ipv4); err != nil {
			return nil, fmt.Errorf("read IPv4 address: %w", err)
		}
		portBytes := make([]byte, 2)
		if _, err := io.ReadFull(br, portBytes); err != nil {
			return nil, fmt.Errorf("read port for IPv4: %w", err)
		}
		host = net.IPv4(ipv4[0], ipv4[1], ipv4[2], ipv4[3]).String()
		port = uint16(portBytes[0])<<8 | uint16(portBytes[1])
		addrResp = append([]byte{0x01}, ipv4...)
		addrResp = append(addrResp, portBytes...)

	case 0x03: // Domain name: 1-byte length + domain bytes
		lenByte := make([]byte, 1)
		if _, err := io.ReadFull(br, lenByte); err != nil {
			return nil, fmt.Errorf("read domain name length: %w", err)
		}
		domainLen := int(lenByte[0])
		if domainLen == 0 {
			// Send address type not supported error (REP=8)
			conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
			return nil, fmt.Errorf("empty domain name")
		}
		domainBytes := make([]byte, domainLen)
		if _, err := io.ReadFull(br, domainBytes); err != nil {
			return nil, fmt.Errorf("read domain name: %w", err)
		}
		portBytes := make([]byte, 2)
		if _, err := io.ReadFull(br, portBytes); err != nil {
			return nil, fmt.Errorf("read port for domain: %w", err)
		}
		host = string(domainBytes)
		port = uint16(portBytes[0])<<8 | uint16(portBytes[1])
		addrResp = append([]byte{0x03, byte(domainLen)}, domainBytes...)
		addrResp = append(addrResp, portBytes...)

	case 0x04: // IPv6 address: 16 bytes
		ipv6 := make([]byte, 16)
		if _, err := io.ReadFull(br, ipv6); err != nil {
			return nil, fmt.Errorf("read IPv6 address: %w", err)
		}
		portBytes := make([]byte, 2)
		if _, err := io.ReadFull(br, portBytes); err != nil {
			return nil, fmt.Errorf("read port for IPv6: %w", err)
		}
		host = net.IP(ipv6).String()
		port = uint16(portBytes[0])<<8 | uint16(portBytes[1])
		addrResp = append([]byte{0x04}, ipv6...)
		addrResp = append(addrResp, portBytes...)

	default:
		// Send address type not supported error (REP=8)
		conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return nil, fmt.Errorf("unsupported SOCKS5 address type: %d", atyp)
	}

	return &socks5Request{
		DestHost:      host,
		DestPort:      port,
		DestAddrBytes: addrResp,
	}, nil
}

// sendSocks5Error sends a SOCKS5 error response.
// errCode should be one of: 1=general failure, 2=conn not allowed, 3=network unreachable,
// 4=host unreachable, 5=connection refused, 6=TTL expired, 7=command not supported, 8=address not supported
func sendSocks5Error(conn net.Conn, errCode byte) {
	// SOCKS5 error response: [VER=5 | REP | RSV=0 | ATYP=1 | ADDR=0.0.0.0 | PORT=0]
	resp := []byte{0x05, errCode, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
	conn.Write(resp)
}

// handleConn, waitForPeer, deliverProbeFrame, runProbe, tryProbeOnce below
// are a near-verbatim port of cmd/helper/main.go, with log.Printf swapped
// for s.OnLog/s.OnError so messages reach the Kotlin UI's log tab.

func waitForPeer(ctx context.Context, ups *upstream.Upstream, relay bool, timeout time.Duration) bool {
	ready := func() bool {
		if relay {
			return ups.OwnConnID() != ""
		}
		return ups.PeerConnID() != ""
	}
	if ready() {
		return true
	}
	if !relay {
		ups.SendSync()
	}

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	pollTicker := time.NewTicker(200 * time.Millisecond)
	defer pollTicker.Stop()
	syncTicker := time.NewTicker(2 * time.Second)
	defer syncTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-syncTicker.C:
			if !relay && ups.PeerConnID() == "" {
				ups.SendSync()
			}
		case <-pollTicker.C:
			if ready() {
				return true
			}
		}
	}
}

func deliverProbeFrame(mu *sync.Mutex, probeChans map[uint32]chan protocol.Frame, f protocol.Frame) {
	mu.Lock()
	ch, ok := probeChans[f.StreamID]
	mu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- f:
	default:
	}
}

const (
	probeMaxAttempts = 3
	probeRetryDelay  = 3 * time.Second
)

func runProbe(ctx context.Context, s LogSink, ups *upstream.Upstream, sm *streams.Manager, pendingMu *sync.Mutex, pendingOpens map[uint32]chan protocol.Frame, probeMu *sync.Mutex, probeChans map[uint32]chan protocol.Frame, relay bool) {
	if !waitForPeer(ctx, ups, relay, 30*time.Second) {
		s.OnLog("[WARN] probe: upstream not ready within 30s, skipping")
		return
	}

	var lastErr string
	for attempt := 1; attempt <= probeMaxAttempts; attempt++ {
		if ctx.Err() != nil {
			return
		}
		if attempt > 1 {
			select {
			case <-time.After(probeRetryDelay):
			case <-ctx.Done():
				return
			}
		}
		ok, detail := tryProbeOnce(ctx, ups, sm, pendingMu, pendingOpens, probeMu, probeChans)
		if ctx.Err() != nil {
			return
		}
		if ok {
			s.OnLog(fmt.Sprintf("[INFO] probe: OK after %d attempt(s) — %s", attempt, detail))
			return
		}
		lastErr = detail
	}
	s.OnLog(fmt.Sprintf("[WARN] probe: FAILED after %d attempts: %s", probeMaxAttempts, lastErr))
}

func tryProbeOnce(ctx context.Context, ups *upstream.Upstream, sm *streams.Manager, pendingMu *sync.Mutex, pendingOpens map[uint32]chan protocol.Frame, probeMu *sync.Mutex, probeChans map[uint32]chan protocol.Frame) (bool, string) {
	shortID := ups.HelperShortID()
	localID := sm.NextID() & protocol.StreamLocalIDMask
	sid := (uint32(shortID) << protocol.StreamHelperShortIDShift) | protocol.StreamProbeFlag | localID

	openCh := make(chan protocol.Frame, 1)
	pendingMu.Lock()
	pendingOpens[sid] = openCh
	pendingMu.Unlock()
	defer func() {
		pendingMu.Lock()
		delete(pendingOpens, sid)
		pendingMu.Unlock()
	}()

	dataCh := make(chan protocol.Frame, 16)
	probeMu.Lock()
	probeChans[sid] = dataCh
	probeMu.Unlock()
	defer func() {
		probeMu.Lock()
		delete(probeChans, sid)
		probeMu.Unlock()
	}()

	start := time.Now()
	if err := sm.SendFrame(protocol.Frame{Type: protocol.MsgOpen, StreamID: sid}); err != nil {
		return false, "send failed: " + err.Error()
	}

	select {
	case resp, ok := <-openCh:
		if !ok {
			return false, "peer reset"
		}
		if resp.Type == protocol.MsgOpenFail {
			return false, "OPEN_FAIL: " + string(resp.Payload)
		}
	case <-time.After(10 * time.Second):
		return false, "OPEN_OK timeout"
	case <-ctx.Done():
		return false, "cancelled"
	}

	getReq := "GET / HTTP/1.0\r\nHost: probe.bridge-to-freedom\r\nUser-Agent: btf-helper-probe\r\n\r\n"
	if err := sm.SendFrame(protocol.Frame{Type: protocol.MsgData, StreamID: sid, Payload: []byte(getReq)}); err != nil {
		return false, "GET send failed: " + err.Error()
	}

	var body []byte
	deadline := time.After(10 * time.Second)
	finSeen := false
loop:
	for !finSeen {
		select {
		case f := <-dataCh:
			switch f.Type {
			case protocol.MsgData:
				body = append(body, f.Payload...)
			case protocol.MsgFin:
				finSeen = true
				break loop
			case protocol.MsgRst:
				return false, "RST received"
			}
		case <-deadline:
			return false, "response timeout"
		case <-ctx.Done():
			return false, "cancelled"
		}
	}

	rtt := time.Since(start)
	if len(body) == 0 {
		return false, "empty response"
	}
	const expected = "HTTP/1.1 200 OK"
	if len(body) >= len(expected) && string(body[:len(expected)]) == expected {
		return true, fmt.Sprintf("rtt=%v bytes=%d", rtt, len(body))
	}
	return false, "unexpected response"
}

func stopHelperMode() {
	helperMu.Lock()
	defer helperMu.Unlock()
	stopHelperModeLocked()
}

// waitHelperReady blocks until the WSS upstream has an authenticated peer,
// or until timeout/ctx cancellation. This is the Android equivalent of
// myvpn.exe's [STEP 5] WaitForSOCKS5: on Windows, myvpn.exe doesn't declare
// the connection "up" — and doesn't add the default route that sends real
// traffic onto the TUN adapter — until it has confirmed helper.exe's SOCKS5
// port actually accepts connections. Here, runHelperMode's local SOCKS5-ish
// listener is a bound net.Listener the instant it starts (so a Windows-style
// TCP dial-probe against it is a no-op, always instantly "ready"); the part
// that can actually still be unready is the WSS session to the bridge, so
// this checks that instead — same intent, adapted to what can really fail
// on this side.
func waitHelperReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		helperMu.Lock()
		ups := helperUps
		helperMu.Unlock()
		if ups != nil && ups.HasAnyPeer() {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %v waiting for bridge to authenticate a peer", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func stopHelperModeLocked() {
	if helperT2SStop != nil {
		helperT2SStop()
		helperT2SStop = nil
	}
	if helperUps != nil {
		helperUps = nil // Upstream.Run() exits on ctx cancellation; nothing else to close explicitly
	}
	if helperLn != nil {
		helperLn.Close()
		helperLn = nil
	}
	if helperTunFile != nil {
		helperTunFile.Close()
		helperTunFile = nil
	}
}
