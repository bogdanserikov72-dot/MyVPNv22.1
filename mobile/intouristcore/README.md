# mobile/intouristcore — Android Go core

This replaces the stub `Intouristcore` Go library whose compiled AAR was
throwing:

```
helper core is not linked: wire the WSS helper runner here and dial upstream with protectedDialer
xray core is not linked: start xray-core here with a protected outbound dialer
```

## What's done

- `intouristcore.go` — the exact exported surface the existing AAR/Kotlin
  code expects (`StartHelperMode`, `StartSubMode`, `Stop`, `IsRunning`,
  `IsStarting`, `AttachTunFd`, `LogSink`, `SocketProtector`). Drop this
  package in at `mobile/intouristcore/` (sibling of `android/`, matching
  what `app/build.gradle`'s `bindGomobile` task already points at) and
  nothing on the Kotlin side needs to change.
- `dialer.go` — protected dial function shared by both modes, wired to
  `VpnService.protect()` via the `SocketProtector` callback.
- `submode.go` + `xray_dialer.go` — **fully wired**, real implementation.
  Runs xray-core in-process (`github.com/xtls/xray-core`) against the exact
  JSON `VpnBridgeApi.kt`'s `makeXrayConfig()` already generates, and routes
  the TUN fd into its SOCKS inbound with an in-process tun2socks
  (`github.com/xjasonlyu/tun2socks/v2`). This mode should build and run as
  soon as the two dependencies below are added to `go.mod`.
- `helpermode.go` — the local listener + tun2socks wiring is real and
  complete (same shape as sub-mode). The multiplexed WSS session itself
  delegates to a package `myvpn/mobile/intouristcore/adapter` that **is not
  included** — see below.

## What I still need from you

`bin/main.go` (your Windows `helper.exe`) imports:

```
github.com/bridge-to-freedom/adapter/internal/config
github.com/bridge-to-freedom/adapter/internal/protocol
github.com/bridge-to-freedom/adapter/internal/streams
github.com/bridge-to-freedom/adapter/internal/upstream
github.com/bridge-to-freedom/adapter/internal/wsapi
```

`bin.zip` only had the single `bin/main.go` file that calls into these —
the actual protocol implementation (frame encoding, the HELLO/PEER_CONN/PING
handshake, reconnect+backoff, stream-ID allocation) lives in that separate
module and wasn't part of either upload. I didn't try to reconstruct it from
guesswork: `bin/main.go`'s call sites tell me the *shape* of the API
(`config.Load`, `upstream.New(cfg, frameHandler)`, `ups.Run(ctx)`,
`streams.NewManager(sendFunc)`, `protocol.Frame{Type, StreamID, Payload}`,
etc.) but not the actual bytes that go over the wire — getting that wrong
wouldn't crash, it would just silently fail to talk to your real cloud
bridge endpoint, which is worse than the current explicit stub error.

If you can upload that module (or point me at it — it's a normal Go import
path, so if it's a public/private Git repo I can also just add it as a
`go.mod` requirement instead of vendoring it), I'll finish wiring
`helpermode.go`'s accept loop and frame-handler switch — it's a fairly
mechanical port of `bin/main.go`'s `handleConn`/`waitForPeer`/frame-switch
logic, which is already plain portable Go with no Windows-specific calls.

## go.mod additions needed

```
require (
    github.com/xtls/xray-core v1.8.x        // pin whatever version you build/test against
    github.com/xjasonlyu/tun2socks/v2 latest
    // + github.com/bridge-to-freedom/adapter, once available
)
```

Run `go mod tidy` from the project root after adding these (needs network —
I don't have it in this sandbox, so I haven't attempted an actual build
here, only written and structurally reviewed the source).

## Known rough edge

`xray_dialer.go`'s `registerProtectedDialer` hooks into
`internet.UseAlternativeSystemDialer`. That symbol's exact shape has moved
between xray-core releases — it's the one spot most likely to need a small
signature tweak after you `go get` a specific version. Everything else
(`dialer.go`, `submode.go`'s xray/tun2socks wiring, `helpermode.go`'s
listener/tun2socks wiring) is stable, version-independent Go.

## Build

```powershell
gomobile bind -target=android -androidapi=21 -javapkg=com.intourist.gomobile -o android/app/libs/gomobile-intouristcore.aar ./mobile/intouristcore
```
(this is already what `app/build.gradle`'s `bindGomobile` task runs for you
on `./gradlew assembleRelease`, once `mobile/intouristcore` exists next to
`android/`).
