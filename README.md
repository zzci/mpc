# mcp-wallet

TSS MPC 自托管共管钱包的**签名内核**：基于 [tss-lib v3](external/tss-lib) 的纯端上门限签名，支持 ETH / BSC / TRON（三链同用 secp256k1，一次 DKG 单主公钥三链通用）。任何分支不得退化为远端签名，coord-server 仅做信任最小化编排、不参与 MPC。

> 本仓库只负责签名内核与传输/编排骨架；交易构造、广播、质押 calldata 等链上业务逻辑不在范围内。

## 文档入口

- **[架构总览](docs/ARCHITECTURE.md)** — 系统上下文 / 组件 / 部署拓扑 / 时序 / 信任模型 / 安全边界
- **[交付状态](docs/DELIVERY.md)** — P0-P6 + DM-* + AD-* + cli-ui 实际进度 + 验证方式
- **[使用指南](docs/USAGE.md)** — 快速开始 / 部署 / A面+B面 API / SDK / wallet-cli / 测试 / 备份
- **[设计深度](docs/design/)** — 实现者细节(17 篇组件 source-of-truth)

## 目录结构

| 路径 | 说明 |
|---|---|
| `cmd/server` | 单一可执行：`server` 的 relay 角色（零信任哑管道）/ coord 角色（信任最小化协调），由配置开关启用，可单开或双开 |
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

## 配置

完整带注释的示例配置见 [`server.example.yaml`](server.example.yaml)（覆盖 log / metrics / relay / coord 全部字段、默认值与可选枚举）。

- 复制为 `./server.yaml`（默认路径），或用 `SERVER_CONFIG` / CLI `--config <path>` 指定路径。
- 优先级（Traefik 式三源，统一键空间）：内置默认 < 配置文件 < 环境变量 < CLI 参数。
  - 环境变量：`MPC_` + 大写点分键，嵌套与键内 `_` 一律单 `_`（如 `MPC_COORD_HTTP_LISTEN`、`MPC_COORD_TTL_SKEW_TOLERANCE`）；由 schema 生成名后精确匹配，不解析 env 名。
  - CLI：`--<点分键>=<值>`（如 `--coord.http.listen=:8080`、`--relay.enable=true`）。
- 任一值可为字面量或 `env:VAR` / `file:/path` 引用（运维自择，不再强制 secret 必为引用）；双角色均关闭、已启用角色必填项缺失 → 启动 fail-fast。
- `coord.db.encryption.enable` 生产必须为 `true`；置 `false` 仅限 dev/test 且须额外设 `ALLOW_INSECURE_DB=1`，否则拒绝启动。

## 质量门（一行执行）

```bash
gofmt -l . && go vet ./... && golangci-lint run && go test ./...
```

## 依赖说明

`github.com/bnb-chain/tss-lib/v3` 经 `go.mod` 的 `replace` 指向本地 vendored 副本 `external/tss-lib`（tag v3.0.0，纯 Go 无 cgo），锁定经审计源码。
