package intouristcore

import (
	"context"
	"fmt"
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

// handleConn, waitForPeer, deliverProbeFrame, runProbe, tryProbeOnce below
// are a near-verbatim port of cmd/helper/main.go, with log.Printf swapped
// for s.OnLog/s.OnError so messages reach the Kotlin UI's log tab.

func handleConn(ctx context.Context, s LogSink, conn net.Conn, ups *upstream.Upstream, sm *streams.Manager, pendingMu *sync.Mutex, pendingOpens map[uint32]chan protocol.Frame, relay bool) {
	if tc, ok := conn.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
	}

	if !waitForPeer(ctx, ups, relay, 10*time.Second) {
		s.OnLog(fmt.Sprintf("[WARN] no peer available, closing inbound from %s", conn.RemoteAddr()))
		conn.Close()
		return
	}

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

	if err := sm.SendFrame(protocol.Frame{Type: protocol.MsgOpen, StreamID: sid}); err != nil {
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
			conn.Close()
			return
		}
	case <-time.After(30 * time.Second):
		conn.Close()
		return
	case <-ctx.Done():
		conn.Close()
		return
	}

	sm.Register(str)
	sm.ReadLoop(str)
}

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
