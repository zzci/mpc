module github.com/royqta/mcp-wallet

go 1.25.7

// tss-lib v3 is vendored locally via a replace directive: external/tss-lib is
// github.com/bnb-chain/tss-lib/v3 at tag v3.0.0 (pure Go, no cgo), pinned to an
// audited source copy so the MPC kernel never pulls an upstream fork.
require github.com/bnb-chain/tss-lib/v3 v3.0.0

require (
	github.com/btcsuite/btcd/btcec/v2 v2.3.2
	github.com/btcsuite/btcutil v1.0.2
	github.com/gowebpki/jcs v1.0.1
	// libp2p unified baseline per L1 final ruling B (PLAN.md §1): go-libp2p
	// v0.43.0 + go-multiaddr v0.16.1, go1.25 line. registry-verified stable.
	github.com/libp2p/go-libp2p v0.43.0
	github.com/multiformats/go-multiaddr v0.16.1
	github.com/mutecomm/go-sqlcipher/v4 v4.4.2
	github.com/pressly/goose/v3 v3.15.1
	github.com/umbracle/fastrlp v0.1.0
	go.yaml.in/yaml/v3 v3.0.4
	golang.org/x/crypto v0.51.0
	golang.org/x/text v0.37.0
	// protobuf v1.31.0 recorded-reason pin lifted by L1 final ruling B: B2
	// verified tss-lib v3.0.0 `go test` ok with protobuf v1.36.6 (no
	// regression); v1.36.6 is required by the go-libp2p v0.43.0 baseline.
	google.golang.org/protobuf v1.36.6
)

require (
	github.com/libp2p/go-libp2p-pubsub v0.16.0
	golang.org/x/sync v0.20.0
)

require (
	filippo.io/bigmod v0.1.0 // indirect
	github.com/agl/ed25519 v0.0.0-20200225211852-fd4d107ace12 // indirect
	github.com/benbjohnson/clock v1.3.5 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/btcsuite/btcd v0.23.4 // indirect
	github.com/btcsuite/btcd/chaincfg/chainhash v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davidlazar/go-crypto v0.0.0-20200604182044-b73af7476f6c // indirect
	github.com/decred/dcrd/dcrec/edwards/v2 v2.0.3 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.0 // indirect
	github.com/flynn/noise v1.1.0 // indirect
	github.com/francoispqt/gojay v1.2.13 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/gopacket v1.1.19 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/hashicorp/errwrap v1.0.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/huin/goupnp v1.3.0 // indirect
	github.com/ipfs/go-cid v0.5.0 // indirect
	github.com/ipfs/go-log v1.0.5 // indirect
	github.com/ipfs/go-log/v2 v2.6.0 // indirect
	github.com/jackpal/go-nat-pmp v1.0.2 // indirect
	github.com/jbenet/go-temp-err-catcher v0.1.0 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/klauspost/cpuid/v2 v2.2.10 // indirect
	github.com/koron/go-ssdp v0.0.6 // indirect
	github.com/libp2p/go-buffer-pool v0.1.0 // indirect
	github.com/libp2p/go-flow-metrics v0.2.0 // indirect
	github.com/libp2p/go-libp2p-asn-util v0.4.1 // indirect
	github.com/libp2p/go-msgio v0.3.0 // indirect
	github.com/libp2p/go-netroute v0.2.2 // indirect
	github.com/libp2p/go-reuseport v0.4.0 // indirect
	github.com/libp2p/go-yamux/v5 v5.0.1 // indirect
	github.com/marten-seemann/tcp v0.0.0-20210406111302-dfbc87cc63fd // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/miekg/dns v1.1.66 // indirect
	github.com/mikioh/tcpinfo v0.0.0-20190314235526-30a79bb1804b // indirect
	github.com/mikioh/tcpopt v0.0.0-20190314235656-172688c1accc // indirect
	github.com/minio/sha256-simd v1.0.1 // indirect
	github.com/mr-tron/base58 v1.2.0 // indirect
	github.com/multiformats/go-base32 v0.1.0 // indirect
	github.com/multiformats/go-base36 v0.2.0 // indirect
	github.com/multiformats/go-multiaddr-dns v0.4.1 // indirect
	github.com/multiformats/go-multiaddr-fmt v0.1.0 // indirect
	github.com/multiformats/go-multibase v0.2.0 // indirect
	github.com/multiformats/go-multicodec v0.9.1 // indirect
	github.com/multiformats/go-multihash v0.2.3 // indirect
	github.com/multiformats/go-multistream v0.6.1 // indirect
	github.com/multiformats/go-varint v0.0.7 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/opentracing/opentracing-go v1.2.0 // indirect
	github.com/otiai10/primes v0.0.0-20210501021515-f1b2be525a11 // indirect
	github.com/pbnjay/memory v0.0.0-20210728143218-7b4eea64cf58 // indirect
	github.com/pion/datachannel v1.5.10 // indirect
	github.com/pion/dtls/v2 v2.2.12 // indirect
	github.com/pion/dtls/v3 v3.0.6 // indirect
	github.com/pion/ice/v4 v4.0.10 // indirect
	github.com/pion/interceptor v0.1.40 // indirect
	github.com/pion/logging v0.2.3 // indirect
	github.com/pion/mdns/v2 v2.0.7 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/rtcp v1.2.15 // indirect
	github.com/pion/rtp v1.8.19 // indirect
	github.com/pion/sctp v1.8.39 // indirect
	github.com/pion/sdp/v3 v3.0.13 // indirect
	github.com/pion/srtp/v3 v3.0.6 // indirect
	github.com/pion/stun v0.6.1 // indirect
	github.com/pion/stun/v3 v3.0.0 // indirect
	github.com/pion/transport/v2 v2.2.10 // indirect
	github.com/pion/transport/v3 v3.0.7 // indirect
	github.com/pion/turn/v4 v4.0.2 // indirect
	github.com/pion/webrtc/v4 v4.1.2 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/client_golang v1.22.0 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.64.0 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/quic-go/qpack v0.5.1 // indirect
	github.com/quic-go/quic-go v0.54.0 // indirect
	github.com/quic-go/webtransport-go v0.9.0 // indirect
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	github.com/wlynxg/anet v0.0.5 // indirect
	go.uber.org/dig v1.19.0 // indirect
	go.uber.org/fx v1.24.0 // indirect
	go.uber.org/mock v0.5.2 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/exp v0.0.0-20250606033433-dcc06ee1d476 // indirect
	golang.org/x/mobile v0.0.0-20260514233045-7de0a8fa7f4d // indirect
	golang.org/x/mod v0.36.0 // indirect
	golang.org/x/net v0.54.0 // indirect
	golang.org/x/sys v0.44.0 // indirect
	golang.org/x/telemetry v0.0.0-20260508192327-42602be52be6 // indirect
	golang.org/x/time v0.12.0 // indirect
	golang.org/x/tools v0.45.0 // indirect
	lukechampine.com/blake3 v1.4.1 // indirect
)

replace github.com/bnb-chain/tss-lib/v3 => ./external/tss-lib

// Mirror the replace from external/tss-lib/go.mod: when tss-lib is pulled in
// via a local replace, the replaced module's own replace does not take effect,
// so the main module must restate it — otherwise ed25519 resolves to the
// upstream package (missing the edwards25519 subpackage) and the build fails.
// The fork is binance-chain/edwards25519.
replace github.com/agl/ed25519 => github.com/binance-chain/edwards25519 v0.0.0-20200305024217-f36fc4b53d43

// golang.org/x/mobile recorded-reason pin（L1 裁定 2026-05-18，RA-001/GM-001
// YELLOW；docs/design/PLAN.md §1，commit d05f0f6）：gomobile 生成的 Android/iOS 桥接
// 胶水代码 import golang.org/x/mobile/bind，模块须可解析该依赖，否则 gobind 报
// 「no Go package in golang.org/x/mobile/bind」。pin 锚定 GM-001 已实证成功 .aar
// 构建所解析的确切伪版本（docs/gomobile-build-report.md:15）；tool 指令使其不被
// go mod tidy 裁剪并令 gomobile 工具可由本 go.mod 复现，否决 CI 内浮动
// go install x/mobile@latest（非可复现/供应链风险/违 §1 pin 纪律）。连带 x/* 抬升
// 由该确切伪版本 x/mobile 自身 go.mod 经 MVS 决定（x/crypto v0.39.0→v0.51.0 满足
// §1「x/crypto ≥v0.39.0」下限；x/sys go1.25 下不再 pin；余为 indirect），校准门
// GREEN 零回归实证（x/mobile 不被 wallet 运行码 import，仅 bind 工具用）。
tool golang.org/x/mobile/cmd/gomobile
