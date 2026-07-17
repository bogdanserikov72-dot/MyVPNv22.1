// Package intouristcore is the gomobile-bound Go core for Intourist VPN on
// Android. It is compiled with:
//
//	gomobile bind -target=android -androidapi=21 -javapkg=com.intourist.gomobile ./mobile/intouristcore
//
// The exported function/interface surface here must match exactly what
// app/libs/gomobile-intouristcore-sources.jar was generated from (see the
// method signatures in com/intourist/gomobile/intouristcore/*.java), so the
// existing Kotlin code (IntouristVpnService.kt, VpnBridgeApi.kt) keeps
// working unchanged once this package is rebuilt into a new AAR.
package intouristcore

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// LogSink is implemented on the Kotlin side (IntouristVpnService) and lets
// this package stream log lines / state changes back to the UI, mirroring
// the Windows GUI's bridge.appendLog(...).
type LogSink interface {
	OnLog(message string)
	OnError(message string)
	OnStateChanged(connected bool, mode string)
}

// SocketProtector wraps Android's VpnService.protect(fd). Every outbound
// socket this package opens (the WSS bridge connection in helper mode, the
// proxy server connection in sub mode) MUST be protected, or it will be
// captured by our own TUN and loop forever. See dialer.go.
type SocketProtector interface {
	Protect(fd int64) bool
}

const (
	ModeHelper = "helper"
	ModeSub    = "sub"
)

var (
	mu        sync.Mutex
	starting  bool
	running   bool
	cancelRun context.CancelFunc
	sink      LogSink
)

// IsRunning reports whether the core session is active.
func IsRunning() bool {
	mu.Lock()
	defer mu.Unlock()
	return running
}

// IsStarting reports whether a start sequence has been accepted but not yet
// promoted to running or failed. Kotlin uses this to suppress duplicate
// starts (see VpnBridgeApi.connect()'s startInFlight guard, which is a
// belt-and-suspenders check on top of this one).
func IsStarting() bool {
	mu.Lock()
	defer mu.Unlock()
	return starting
}

// AttachTunFd can be called after a mode runner has already started, e.g.
// after a hot reconnect where Kotlin re-establishes the VpnService TUN
// without tearing down the Go-side session. Not required for a normal
// connect flow (startHelperMode/startSubMode take the fd directly).
func AttachTunFd(tunFd int64) error {
	mu.Lock()
	defer mu.Unlock()
	if !running {
		return fmt.Errorf("attachTunFd: no active session")
	}
	// Hot re-attachment of a new TUN fd into an already-running tun2socks
	// session isn't implemented in submode.go/helpermode.go (both only wire
	// the fd once, at start). Kotlin's normal connect flow doesn't need
	// this — it passes the fd directly to StartHelperMode/StartSubMode.
	// Wire this up for real if you add a reconnect-without-teardown path.
	return fmt.Errorf("attachTunFd: hot fd re-attachment is not implemented; stop and start the session again with the new fd")
}

// Stop tears down the active helper/xray/tun2socks session. Idempotent —
// safe to call even if nothing is running, mirroring processmgr.Cleanup()'s
// "always safe to call" contract from the Windows core.
func Stop() {
	mu.Lock()
	cancel := cancelRun
	wasRunning := running || starting
	cancelRun = nil
	starting = false
	running = false
	s := sink
	mu.Unlock()

	if cancel != nil {
		cancel()
	}
	stopSubMode()
	stopHelperMode()

	if wasRunning && s != nil {
		s.OnStateChanged(false, "")
	}
}

// StartHelperMode starts the Android equivalent of helper.exe: a WSS bridge
// to the cloud endpoint plus an in-process tun2socks that reads the
// VpnService TUN fd directly. The bridge config used is always the static
// helper.config.yaml embedded into this package at build time (see
// configgen.go's staticHelperConfig) — configYAML is only honored if it is
// itself already a real helper.config.yaml document with a real bridge.url,
// which lets it be overridden for local testing.
func StartHelperMode(configYAML string, tunFd int64, s LogSink, protector SocketProtector) error {
	// The helper bridge is our own fixed relay, not per-subscription data —
	// VpnBridgeApi's "server" JSON for helper mode (kind:"helper",
	// host:"bridge") is just a UI placeholder with no real connection info.
	// So: only trust configYAML as-is if it's already a real
	// helper.config.yaml with a real bridge.url in it (e.g. someone
	// deliberately overriding it for testing). Anything else — the
	// placeholder JSON, an empty string, garbage — falls back to the
	// static config embedded in this binary at build time.
	cfg := configYAML
	if !hasBridgeURL(cfg) {
		cfg = staticHelperConfig
	}
	return start(ModeHelper, cfg, tunFd, s, protector)
}

// hasBridgeURL reports whether s is a helper.config.yaml document that
// already has a non-empty bridge.url. Note this is not "is this valid
// YAML" — a JSON blob like {"host":"bridge",...} is technically valid
// YAML too (JSON is a YAML subset) and would unmarshal without error, just
// leaving Bridge.URL at its zero value, which is exactly what we want to
// detect and reject here.
func hasBridgeURL(s string) bool {
	var cfg struct {
		Bridge struct {
			URL string `yaml:"url"`
		} `yaml:"bridge"`
	}
	if err := yaml.Unmarshal([]byte(s), &cfg); err != nil {
		return false
	}
	return cfg.Bridge.URL != ""
}

// StartSubMode starts the Android equivalent of xray.exe with a generated
// Xray JSON config (same shape VpnBridgeApi.kt's makeXrayConfig() already
// produces on the Kotlin side — inbound SOCKS on 127.0.0.1:1080, outbound
// vless/vmess/trojan/shadowsocks). tun2socks routes the TUN fd into that
// in-process SOCKS inbound.
func StartSubMode(xrayJSON string, tunFd int64, s LogSink, protector SocketProtector) error {
	return start(ModeSub, xrayJSON, tunFd, s, protector)
}

func start(mode, config string, tunFd int64, s LogSink, protector SocketProtector) error {
	mu.Lock()
	if starting || running {
		mu.Unlock()
		return fmt.Errorf("core already starting or running")
	}
	if s == nil || protector == nil {
		mu.Unlock()
		return fmt.Errorf("sink and protector must not be nil")
	}
	starting = true
	sink = s
	ctx, cancel := context.WithCancel(context.Background())
	cancelRun = cancel
	mu.Unlock()

	s.OnLog(fmt.Sprintf("[INFO] [STEP 1] starting %s mode core", mode))

	var err error
	switch mode {
	case ModeSub:
		// xray-core's inst.Start() (inside runSubMode) is synchronous — by
		// the time it returns, the SOCKS/HTTP inbounds are already bound.
		// That's the same guarantee Windows gets by running `xray.exe run`
		// directly, so there's no separate readiness wait needed here,
		// mirroring how the desktop GUI reports the sub connection as up
		// immediately after xray.exe starts.
		err = runSubMode(ctx, config, tunFd, s, protector)
		if err == nil {
			s.OnLog("[INFO] [STEP 2] xray-core started, SOCKS5/HTTP inbounds bound, tun2socks routing TUN")
		}
	default:
		err = runHelperMode(ctx, config, tunFd, s, protector)
		if err == nil {
			s.OnLog("[INFO] [STEP 2] helper listener + tun2socks started")
			// Mirrors myvpn.exe's [STEP 5] WaitForSOCKS5, which blocks up to
			// 30s and fails the whole connection (log.Fatalf) rather than
			// declaring success while helper.exe might not actually be
			// reachable yet. Same idea here: don't tell Kotlin/the UI
			// "connected" — and don't leave a half-up session running —
			// until the bridge has actually authenticated.
			s.OnLog("[INFO] [STEP 3] waiting for bridge to authenticate...")
			if werr := waitHelperReady(ctx, 20*time.Second); werr != nil {
				stopHelperMode()
				cancel()
				err = fmt.Errorf("bridge did not become ready: %w", werr)
			} else {
				s.OnLog("[INFO] [STEP 4] bridge authenticated, tunnel is live")
			}
		}
	}

	mu.Lock()
	starting = false
	if err != nil {
		running = false
		cancelRun = nil
	} else {
		running = true
	}
	mu.Unlock()

	if err != nil {
		s.OnError(fmt.Sprintf("%s mode failed: %v", mode, err))
		return err
	}

	s.OnLog(fmt.Sprintf("[INFO] %s mode running", mode))
	s.OnStateChanged(true, mode)
	return nil
}
