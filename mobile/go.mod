module myvpn

go 1.26.3

require (
	github.com/bridge-to-freedom/adapter v0.0.0-00010101000000-000000000000
	github.com/xjasonlyu/tun2socks/v2 v2.6.0
	github.com/xtls/xray-core v1.8.24
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/ajg/form v1.7.1 // indirect
	github.com/andybalholm/brotli v1.1.0 // indirect
	github.com/cloudflare/circl v1.4.0 // indirect
	github.com/dgryski/go-metro v0.0.0-20211217172704-adc40b04c140 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/francoispqt/gojay v1.2.13 // indirect
	github.com/ghodss/yaml v1.0.1-0.20220118164431-d8423dcdf344 // indirect
	github.com/go-chi/chi/v5 v5.3.0 // indirect
	github.com/go-chi/cors v1.2.2 // indirect
	github.com/go-chi/render v1.0.3 // indirect
	github.com/go-gost/relay v0.6.1 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/pprof v0.0.0-20240528025155-186aa0362fba // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/schema v1.4.1 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/klauspost/compress v1.17.8 // indirect
	github.com/klauspost/cpuid/v2 v2.2.7 // indirect
	github.com/onsi/ginkgo/v2 v2.19.0 // indirect
	github.com/pelletier/go-toml v1.9.5 // indirect
	github.com/pires/go-proxyproto v0.7.0 // indirect
	github.com/quic-go/qpack v0.4.0 // indirect
	github.com/quic-go/quic-go v0.46.0 // indirect
	github.com/refraction-networking/utls v1.6.7 // indirect
	github.com/riobard/go-bloom v0.0.0-20200614022211-cdc8013cb5b3 // indirect
	github.com/sagernet/sing v0.8.11 // indirect
	github.com/sagernet/sing-shadowsocks v0.2.7 // indirect
	github.com/seiflotfy/cuckoofilter v0.0.0-20240715131351-a2f2c23f1771 // indirect
	github.com/v2fly/ss-bloomring v0.0.0-20210312155135-28617310f63e // indirect
	github.com/xtls/reality v0.0.0-20240712055506-48f0b2d5ed6d // indirect
	github.com/yandex-cloud/go-genproto v0.62.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/mock v0.4.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.28.0 // indirect
	go4.org/netipx v0.0.0-20231129151722-fdeea329fbba // indirect
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/exp v0.0.0-20260611194520-c48552f49976 // indirect
	golang.org/x/mobile v0.0.0-20260709172247-6129f5bee9d5 // indirect
	golang.org/x/mod v0.38.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.org/x/tools v0.48.0 // indirect
	golang.zx2c4.com/wintun v0.0.0-20230126152724-0fa3db229ce2 // indirect
	golang.zx2c4.com/wireguard v0.0.0-20260522210424-ecfc5a8d5446 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20240903143218-8af14fe29dc1 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240903143218-8af14fe29dc1 // indirect
	google.golang.org/grpc v1.66.2 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
	gopkg.in/check.v1 v1.0.0-20180628173108-788fd7840127 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gvisor.dev/gvisor v0.0.0-20260701204157-69c2d17aea96 // indirect
	lukechampine.com/blake3 v1.3.0 // indirect
)

// FORCED, not just "required". xray-core's own go.mod requires a newer
// gvisor than tun2socks/v2 v2.6.0 does. Go's MVS always takes the highest
// version requested by ANYONE in the graph — a plain `require` for an older
// version would just get overridden by xray-core's higher one. `replace`
// is the only way to pin it down regardless of what other modules ask for.
// This is safe here specifically because we do NOT import xray-core's
// wireguard proxy (see submode.go) or anything else that needs the newer
// gvisor API — the only compiled consumer of gvisor.dev/gvisor in this
// build is tun2socks/v2/core, so tun2socks's own pinned version is correct.
replace gvisor.dev/gvisor => gvisor.dev/gvisor v0.0.0-20250523182742-eede7a881b20

// Points at wherever you put the adapter-and-helper folder (with the 3
// patched files from adapter-patches/ applied) relative to this go.mod.
replace github.com/bridge-to-freedom/adapter => ../third_party/adapter-and-helper

tool golang.org/x/mobile/cmd/gobind

replace github.com/xtls/xray-core => ./third_party/xray-core
