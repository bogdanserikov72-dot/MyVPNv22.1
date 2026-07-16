package intouristcore

import (
	"context"
	"fmt"
	"os"
	"sync"

	xraycore "github.com/xtls/xray-core/core"

	// Deliberately NOT importing "github.com/xtls/xray-core/main/distro/all".
	// That blanket import registers every xray-core feature, including the
	// "wireguard" outbound/inbound — which pulls in its own copy of gvisor
	// (for wireguard's userspace TUN, gvisortun) independently of
	// tun2socks's gvisor. Two different gvisor consumers in the same build
	// graph is exactly what caused the "cannot use func(...) as
	// udp.ForwarderHandler" / "pkt.IsNil undefined" errors — Go's MVS picks
	// one gvisor version for both, and it's never simultaneously right for
	// both callers.
	//
	// Since makeXrayConfig() in VpnBridgeApi.kt only ever emits
	// vless/vmess/trojan/shadowsocks outbounds behind socks/http inbounds,
	// we only need to register those. Add more lines here if you extend the
	// Kotlin config generator to emit another protocol/transport later —
	// just never add the wireguard proxy package.
	_ "github.com/xtls/xray-core/app/dispatcher"
	_ "github.com/xtls/xray-core/app/dns"
	_ "github.com/xtls/xray-core/app/log"
	_ "github.com/xtls/xray-core/app/proxyman/inbound"
	_ "github.com/xtls/xray-core/app/proxyman/outbound"
	_ "github.com/xtls/xray-core/app/router"

	_ "github.com/xtls/xray-core/proxy/blackhole"
	_ "github.com/xtls/xray-core/proxy/freedom"
	_ "github.com/xtls/xray-core/proxy/http"
	_ "github.com/xtls/xray-core/proxy/shadowsocks"
	_ "github.com/xtls/xray-core/proxy/socks"
	_ "github.com/xtls/xray-core/proxy/trojan"
	_ "github.com/xtls/xray-core/proxy/vless/inbound"
	_ "github.com/xtls/xray-core/proxy/vless/outbound"
	_ "github.com/xtls/xray-core/proxy/vmess/inbound"
	_ "github.com/xtls/xray-core/proxy/vmess/outbound"

	_ "github.com/xtls/xray-core/transport/internet/grpc"
	_ "github.com/xtls/xray-core/transport/internet/reality"
	_ "github.com/xtls/xray-core/transport/internet/tcp"
	_ "github.com/xtls/xray-core/transport/internet/tls"
	_ "github.com/xtls/xray-core/transport/internet/websocket"

	tun2socks "github.com/xjasonlyu/tun2socks/v2/engine"
)

var (
	subMu       sync.Mutex
	xrayInst    *xraycore.Instance
	tunFile     *os.File
	t2sStopFunc func()
)

// runSubMode mirrors internal/xraymgr.Manager.Start() + tun2socksmgr.Start()
// from the Windows core, but in-process instead of spawning xray.exe /
// tun2socks.exe as child processes (Android doesn't allow arbitrary
// subprocess execution the way Windows does, and it would be wasteful to
// ship two more native binaries when both are available as Go libraries).
//
// xrayJSON is exactly the JSON string VpnBridgeApi.kt's makeXrayConfig()
// already builds: inbound SOCKS on 127.0.0.1:1080 + inbound HTTP on
// 127.0.0.1:1081, one "proxy" outbound (vless/vmess/trojan/shadowsocks),
// one "direct" freedom outbound, routing rules identical to the desktop
// config_gen.py output.
func runSubMode(ctx context.Context, xrayJSON string, tunFd int64, s LogSink, protector SocketProtector) error {
	subMu.Lock()
	defer subMu.Unlock()

	s.OnLog("[INFO] sub: parsing xray config")
	config, err := xraycore.LoadConfig("json", []byte(xrayJSON))
	if err != nil {
		return fmt.Errorf("parse xray config: %w", err)
	}

	inst, err := xraycore.New(config)
	if err != nil {
		return fmt.Errorf("build xray instance: %w", err)
	}

	// Route every outbound TCP/UDP dial xray-core makes through the
	// protected dialer so its own connections to the remote VLESS/VMess/
	// Trojan/Shadowsocks server never re-enter our TUN. xray-core supports
	// swapping its dial behaviour via internet.UseAlternativeSystemDialer;
	// wire the protected dialer in before Start().
	//
	// NOTE: the exact hook name/package (internet.UseAlternativeSystemDialer
	// vs. a custom SockoptEnhancer) has moved around across xray-core
	// releases. Pin an xray-core version in go.mod and adjust this call to
	// match that version's internet package — this is the one spot in
	// sub-mode most likely to need a small edit after `go get`.
	registerProtectedDialer(newProtectedDialFunc(protector))

	if err := inst.Start(); err != nil {
		return fmt.Errorf("start xray instance: %w", err)
	}
	xrayInst = inst
	s.OnLog("[INFO] sub: xray-core started, inbound SOCKS on 127.0.0.1:1080")

	// Take ownership of the TUN fd and hand it to tun2socks, pointed at the
	// SOCKS inbound xray just brought up. This replaces
	// tun2socksmgr.Start()'s "tun2socks.exe -device ... -proxy socks5://127.0.0.1:1080".
	f := os.NewFile(uintptr(tunFd), "tun")
	if f == nil {
		xrayInst.Close()
		xrayInst = nil
		return fmt.Errorf("os.NewFile failed for tunFd=%d", tunFd)
	}
	tunFile = f

	key := tun2socks.Key{
		MTU:      1500,
		Proxy:    "socks5://127.0.0.1:1080",
		RestAPI:  "",
		LogLevel: "info",
		Device:   fmt.Sprintf("fd://%d", tunFd),
	}
	tun2socks.Insert(&key)
	tun2socks.Start()
	t2sStopFunc = tun2socks.Stop
	s.OnLog("[INFO] sub: tun2socks routing TUN -> 127.0.0.1:1080")

	go func() {
		<-ctx.Done()
		stopSubMode()
	}()

	return nil
}

func stopSubMode() {
	subMu.Lock()
	defer subMu.Unlock()

	if t2sStopFunc != nil {
		t2sStopFunc()
		t2sStopFunc = nil
	}
	if xrayInst != nil {
		xrayInst.Close()
		xrayInst = nil
	}
	if tunFile != nil {
		tunFile.Close()
		tunFile = nil
	}
}
