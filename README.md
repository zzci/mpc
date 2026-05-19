# mcp-wallet

TSS MPC 自托管共管钱包的**签名内核**：基于 [tss-lib v3](external/tss-lib) 的纯端上门限签名，支持 ETH / BSC / TRON（三链同用 secp256k1，一次 DKG 单主公钥三链通用）。任何分支不得退化为远端签名，coord-server 仅做信任最小化编排、不参与 MPC。

> 本仓库只负责签名内核与传输/编排骨架；交易构造、广播、质押 calldata 等链上业务逻辑不在范围内。

## 目录结构

| 路径 | 说明 |
|---|---|
| `cmd/server` | 单一可执行：`node relay`（零信任哑管道）/ `node coord`（信任最小化协调），运行时独立部署、独立进程、不共享状态 |
| `cmd/cli` | 端到端测试主载体（多进程模拟 2-of-3） |
| `internal/` | mpc / envelope / transport / relay / coord / keystore / mobileapi / addr / cli |
| `mobile/` | RN 原生桥 + 示例 App 骨架（gomobile 绑定目标） |
| `tests/` | `e2e/`（Bun/TS 全环套件）、`docker/`（隔离拓扑）、`mock/`（外部业务服务替身） |
| `external/tss-lib` | vendored `github.com/bnb-chain/tss-lib/v3`（tag v3.0.0，纯 Go 无 cgo），经 `go.mod` `replace` 锁定经审计源码 |
| `docs/design/` | 设计规范（[docs/design/PLAN.md](docs/design/PLAN.md)、architecture/security/testing、contract、mcp、server） |
| `docs/` | 发布就绪审计、安全评审、gomobile 构建报告 |

## 快速开始

```bash
go build ./...
```

## 质量门（一行执行）

```bash
gofmt -l . && go vet ./... && golangci-lint run && go test ./...
```

## 依赖说明

`github.com/bnb-chain/tss-lib/v3` 经 `go.mod` 的 `replace` 指向本地 vendored 副本 `external/tss-lib`（tag v3.0.0，纯 Go 无 cgo），锁定经审计源码。
