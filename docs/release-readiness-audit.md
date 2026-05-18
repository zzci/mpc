# 发布就绪审计报告（RA-001）

> 性质：**独立评估**（非自证 / 非橡皮图章 / 证据化 file:line）。审计员未参与产品实现，对 `docs/design/P0-report.md`、`docs/security-review.md`、`docs/changelog.md` 的结论**不予默认采信**，全部从代码重新推导核验。
> 范围：纯只读审计。本工作流仅**新增本文档一件**，零改任何已 finalized 产品件，零回归。
> 基线：仓库 HEAD `aa56c98`，分支 `bkd/o5jxth03`，工作树净。审计时真实墙钟 2026-05-18T18:59Z。
> 方法：6 路并行独立审计代理（功能/密码学/密钥+鉴权/零信任/构建+供应链+文档/测试），各产出 file:line 实证；测试维度因授权约束以静态盘点 + 定向真跑 + 已记录证据复核为主（见 §6 透明性声明）。

---

## 0. 结论（go / no-go）

**判定：NO-GO（作为终端用户自托管钱包产品发布）；CONDITIONAL-GO（作为「签名内核组件里程碑」，须先闭合下述 P1 清单）。**

核心依据（独立证据化）：

- **内核侧坚实**：密码学正确性、密钥/机密管理、传输与编排零信任模型、服务端鉴权与防滥用、构建/供应链、文档与运维就绪 —— 六维**零 P0**。两项历史 P0 级风险（nonce 重放时间炸弹、依赖闭包缺口）经独立复核**已确证退役**（见 §4、§7）。
- **交付面薄弱（go/no-go 的支点）**：4 项 P1 集中在「可交付终端面」——移动 SDK 非分布式、RN 桥为惰性桩、唯一联网 keygen 路径关闭了抗恶意参与方的 ZK 证明、测试夹具进入可编译产品路径。对一个**自托管签名产品**，「唯一存在的联网门限 keygen 实现关闭了 modulus/factor ZK 证明」意味着**生产联网协议的密钥生成安全姿态从未被验证过**——这是发布前必须闭合的硬条件，不论该载体是否被标注为「测试载体」（因为没有第二个联网 keygen 实现作替代证据）。
- 团队已**充分披露**移动/真机子门为范围外（`docs/design/P0-report.md` §4，披露质量评为典范）；本审计确认无其它未披露重大缺口。

→ 内核组件层面可作里程碑收口；任何承载真实资产托管的部署或终端产品发布，**须先完成 §9 的 P1 remediation 清单**。

---

## 1. 功能完整性与契约一致性 — **CONCERN**

| 子域 | 判定 | 关键实证 |
|---|---|---|
| C-001 信封规范化编码 | PASS | `internal/contract/canonical.go:53-96`（域分隔/长度前缀/NFC/值派生）；`internal/contract/metahash.go:12-49`（RFC8785 JCS） |
| proposerSig / senderAuth 绑定域 | PASS | `internal/contract/proposer.go:96-109`（精确绑定 SessionID,Round,Payload） |
| MpcMessage 线编码 | PASS | `internal/transport/wire.go:39-48`（4B BE 长度前缀 + JSON，符 protocol.md 用户裁定例外） |
| X-001 coord 状态机/法定人数/TTL | PASS | `internal/server/coord/statemachine.go:34-39`；原子事务 `:74-131` |
| api.md A/B 端点覆盖 | PASS | `internal/server/coord/api.go:40-61`（A1-A4/B2-B7/provisioning 全有 handler） |
| XA-001 外部 `/v1/groups/{groupId}/public` | PASS | `internal/server/coord/api.go:441-459`（外部鉴权链，仅地址视图，无成员泄漏） |
| T-001 三链摘要不变量 | PASS | `internal/txdecode/txdecode.go:95-96`（常时比较 recomputed==digest32，解码偏差即拒签） |
| B-001 移动 SDK 扁平面 | **CONCERN** | 扁平面合规（仅 string/[]byte/callback），但 `internal/mobileapi/sign.go:165-178` 经 `mpc.Sign` 对 `snapshotShares()` **进程内**签名，非逐设备单分片联网门限签名 |
| RN 原生桥 | **CONCERN（设计性）** | `rn/bridge/ios/McpWallet.swift:41-108`、`rn/bridge/android/.../McpWalletModule.kt:39-88` 全部 `unimplemented`/`reject` 桩 |
| E2E 真活绿 | CONCERN | `e2e/test/e2e/full-ring.test.ts:33-37` 等 `it.skipIf(gate)` —— 真活断言仅在 gate 开启时执行（L1 裁定「实现并行、活跑串行」的 sanctioned 模式，非失败，但静态审计无法复现 gate 开态） |

**发现**

- **[P1] 移动 SDK 为进程内签名，非分布式门限** — `internal/mobileapi/sign.go:165-178`。`sdk.md` §3 的终端用户面在 `mpc.Sign(s.snapshotShares())` 下假设单进程持全部门限分片；真实逐设备单分片联网签名仅存在于 CLI 载体（`internal/cli/mpcnet.go`）。系统级自托管契约经 CLI E2E 满足，但**已交付的移动 SDK 不执行联网 t-of-n**。属已记录的分解，但无任务认领该联网装配。
- **[P1] RN 原生桥为惰性桩** — iOS/Android 全方法返回 `unimplemented`/`reject`，无 gomobile `.aar`/`.xcframework` 绑定，RN→native→Go 路径全惰。B-004 范围为 P2「骨架不可运行」（合法分解），但意味着发布时无可用移动 App 路径。
- **[P2] E2E 绿为 gate 条件态，静态审计不可独立复现** — `e2e/test/e2e/gate.ts:1-60`。测试逻辑本身契约忠实（ecrecover/low-S/地址匹配/proposerSig/digest 绑定/RSV verify/RETURNED 断言齐全），但「E2E GREEN」依赖运行时 gate 开启。最近记录证据：终态门 `E2E_LIVE=1` = 2 pass/2 skip/0 fail（localhost）、E2E-002 docker 1 pass/1 skip/0 fail、E2E-001 回归 3 pass/3 skip/0 fail（`docs/changelog.md` 尾部）。go/no-go 前应要求**捕获到 gate 开态非占位测试通过的活跑日志**而非仅采信 changelog 断言。
- **[P3] coord api.go 工具链注释陈旧** — `internal/server/coord/api.go:18-20` 称 baseline go1.23/protobuf v1.31.0，实际 `go.mod:3` go1.25.7 / `go.mod:27` protobuf v1.36.6。仅文档漂移。

---

## 2. 密码学正确性与签名安全 — **CONCERN（条件通过，0 P0 / 2 P1）**

| 子域 | 判定 | 关键实证 |
|---|---|---|
| tss-lib v3 party/门限接线 | PASS | 各入口门限校验正确，无密钥材料泄漏 |
| 生产路径 PreParams 设备内不变量（移动 SDK） | PASS | `internal/mobileapi/sdk.go` `preParams` 仅未导出测试 seam，生产为 nil 由 mpc 端内生成；红线（含 Paillier 私钥禁后端预生成下发）守住 |
| low-S 归一化 & {R,S,V} 恢复 | PASS | tss-lib 终化轮强制 low-S；`Compact()` 编码正确；测试 ecrecover 验证 |
| 三链摘要交叉验证 | PASS | `internal/txdecode/` 常时比较；ETH/BSC 按解析字段重算，TRON sha256(raw_data) |
| 反盲签门 | PASS | 移动 SDK 路径强制且不可绕过（`internal/mobileapi/sign.go:93-163` proposerSig+metaHash+三重过期+解码==digest32 硬拒） |
| 地址派生 ETH/TRON | PASS | keccak256+EIP-55 / base58check；btcec 在曲线校验 |
| 规范化编码确定性/无可塑性 | PASS | `internal/contract/canonical.go`+`metahash.go` 域分隔+长度前缀 |
| 随机性/k 复用/计时泄漏/域分隔 | PASS | 全路径 `crypto/rand`；无 `math/rand` 触密；secret 比较用 `subtle.ConstantTimeCompare`；4 个独立域前缀 |
| 攻击者可控输入触 panic（签名路径） | CONCERN | RLP 递归无深度上限（见 P2 发现） |

**发现**

- **[P1] 测试夹具进入可编译产品路径（托管不变量边界侵蚀）** — `internal/cli/device.go:72-81`。`preParamsFor` 调 `keygen.LoadKeygenTestFixtures(n)`，被导出非测试入口 `RunDeviceInProc`/`RunDevice` 调用，位于非 `_test.go` 文件 → 编译进任何 import `internal/cli` 的二进制。部署的 E2E 环每设备使用**公开已知**的安全素数集，知夹具者可对 Paillier 密钥做预计算攻击。修复直接：`device.go` 加 `//go:build integration` 构建标签隔离，或将 `preParamsFor` 移入 `_test.go`/测试专用包。
- **[P1] 联网 keygen/reshare 路径关闭 modulus/factor ZK 证明** — `internal/cli/mpcnet.go:171-172`（keygen）、`:259-260`（reshare）。`SetNoProofMod()`/`SetNoProofFac()` 无条件禁用 Paillier modulus/分解 ZK 证明（**不受 `fast` 开关门控**，与进程内仿真的 `fast=true` 跳过不同）。这是 GG18/GG20 对抗恶意方注入合数 modulus、偏置分布式密钥生成的核心防御。联网环中被攻陷参与方可借此偏置分片并最终在后续签名会话恢复他方密钥材料。**关键放大因素**：这是**唯一**存在的联网 keygen/reshare 实现 —— 故生产联网协议的密钥生成安全姿态从未在带 ZK 证明下被验证。即使该二进制被标注「测试载体」，对自托管产品仍属发布前必修。修复：从 `runKeygen`/`runReshare` 移除 `SetNoProofMod()/SetNoProofFac()`，相应放宽 E2E 超时，并以带证明的联网环重跑取证。
- **[P2] EVM RLP 递归解码无深度上限** — `internal/txdecode/evm.go:232`（`rebuildValue`，`:287-289` kids 递归）。深嵌套 accessList 可致栈溢出；digest 绑定限制为 DoS（摘要不匹配即拒签不产签名），但签名决策 UI 线程 OOM/栈溢出仍值得关注。修复：加深度上限（如 64）+ 显式处理叶子 `v.Bytes()` 错误。
- **[P2] TRON 不可解析 payload 透传至人工审批** — `internal/txdecode/tron.go:48-57` + `mobileapi/sign.go:118-122`。`TxTronUnparsed` 无错误透传，sha256 恒匹配 → 完全不透明 TRON 载荷到达 `OnDecoded` 等待人工。设计有意（人为兜底/WYSIWYS），但社工风险存在。建议：可选策略钩子自动拒 `TxTronUnparsed`，或 UI 暴露原始 sha256 hex 便于带外核验。
- **[P2] keystore AES-GCM 随机 nonce 无计数器** — `internal/keystore/crypto.go:144-147`。12B 随机 nonce，本地少次封装碰撞概率可忽略；高频 reshare/recover 场景应记封装次数上界或转 AES-GCM-SIV。
- **[P3]** 签名结果仅取 `results[0]` 不交叉校验（`internal/mpc/signing.go:116-123`）；pre-EIP-155 6 元 legacy 仅 caution 不硬拒（`internal/txdecode/evm.go:109-112`，跨链重放风险）；`memberAuthDomain` 为 `var` 非 `const` 可被未来误改（`internal/server/coord/auth.go:36`）。

---

## 3. 密钥与机密管理 — **PASS**

| 子域 | 判定 | 关键实证 |
|---|---|---|
| keystore 静态加密 | PASS | `internal/keystore/crypto.go`：Argon2id(t=2,m=19MiB,p=1)+AES-256-GCM，独立 salt/nonce 均 crypto/rand，HKDF 域分隔混设备因子，参数边界强制（含不可信导入路径），`wipe()` defer，版本先校验，`ErrDecrypt` 非预言式 |
| 备份/恢复 | PASS | `internal/keystore/backup.go`：`ExportShare` 用 `PassphraseOnly{}` 可移植，`ImportShare` 开封不施口令策略（正确，避免预言） |
| 口令加固 | PASS | `internal/keystore/passphrase.go`：12 字符/8 distinct rune/2 类或 20 长/常见基线黑名单，仅写入时强制不预言 |
| 设备安全区 | PASS | `internal/keystore/securearea.go`：`DeviceSecureArea` 拒空 provider/空密钥无软回退，`SoftSecureArea` 明标测试限定 |
| 机密 fail-fast | PASS | `internal/node/config.go` `resolveSecret` 仅受理 `env:`/`file:` 前缀，明文 → `errSecretPlaintext` 启动硬失败；`.env.example` 仅含 bind 地址/日志级别无真机密；无硬编码生产密钥 |

**发现**：无 P0/P1。P2/P3 见 §4 合并（coorddb KDF sidecar 参数边界）。

---

## 4. 服务端鉴权与防滥用加固 — **PASS**

### FIX-001 nonce 重放时间炸弹 —— 独立核验（专段）

历史缺陷：`nonceCache.use()` 曾内部调 `time.Now()`，与注入测试时钟分歧，致 `TestMemberAuthReplay` 在真墙钟 2026-05-18T12:05:00Z 后确定性失败。

独立代码级核验：

- `internal/server/coord/auth.go:79-88`：`verifyMemberAuth` 取 `c.clock.Now()` 为 `tnow`，将 `tnow` 与 `tnow.Add(memberAuthWindow)` 直接传入 `c.nonces.use(...)`。
- `internal/server/coord/auth.go:142-156`：`nonceCache.use()` 以 `now, expiry time.Time` 为入参，**内部无任何 `time.Now()`**。
- `grep time.Now()` 于 `auth.go`/`abuse.go` **零命中**；coord 包内 `time.Now()` 仅 `clock.go:15`（systemClock 生产实现）与 `callback.go:46,51`（webhook 重试，非鉴权路径）。
- 测试 `coord_test.go:55` 注入 `testClock{2026-05-18T12:00:00Z}`，`security_test.go:204` 经 `newHarness` 绑定。
- **定向真跑（本审计，非全 -race 门）**：真墙钟 2026-05-18T18:59Z（已**越过** 12:05Z 触发点）下，`rtk err 'go test ./internal/server/coord/ -run TestMemberAuthReplay -count=1 -v'` rc=0。

**FIX-001 判定：已修复，独立确证。** 注入时钟在鉴权/重放全路径一致使用；时间炸弹在越过原触发墙钟后实测仍通过，已消除。

| 子域 | 判定 | 关键实证 |
|---|---|---|
| coord 成员鉴权 & nonce/重放 | PASS | 见上专段；`abuse.go:28` 限流按 IP 非 claimed memberId（防伪造放大），EC 验签前先限流 |
| admin 加固 | PASS（1 P2） | `auth.go` 常时比较+读控分离；`netguard.go` 不信 XFF 仅 RemoteAddr；`audit.go` 追加式非密参数；`unlock.go` 指数退避(500ms→30s,5 次告警)+TryLock 串行+口令清零；`strongauth.go` 先于令牌比较 |
| 全外部端点限流 | PASS（1 P3） | coord 外部 `extGate`/成员 `memberGate`/provisioning `rateGate` 均 `c.clock.Now()` 驱动；admin `/admin/unlock` 经 `unlockGuard` |

**发现**

- **[P2] admin-UI 登录端点无限流** — `internal/server/admin/ui.go:163,245`（`POST /admin/ui/session`）。JSON unlock 路径有指数退避 `unlockGuard`，HTML 登录表单仅 `h.strong` 包裹无等效防护；IP 允许列表为空的部署场景下可对令牌做快速枚举。修复：对 `hSession` 套用同 `unlockGuard` 或每 IP 固定窗口限流。
- **[P3]** coorddb `.kdf` sidecar 仅校 Salt base64，不校 TimeCost/MemoryKiB/Threads/KeyLen，被篡改可强制退化 Argon2 参数（`internal/server/coorddb/kdf.go:42-53`，应仿 keystore 边界）；coord/admin HTTP 仅设 `ReadHeaderTimeout` 无 `ReadTimeout`，慢速 body 攻击可长持连接（`coord.go:145-149`,`admin/server.go:172-176`）；非 loopback 且无 CIDR/TLS 时应启动失败而非仅 Warn（`admin/server.go:200-213`）。

---

## 5. 传输与编排零信任模型 — **PASS**

| 子域 | 判定 | 关键实证 |
|---|---|---|
| relay = 零信任哑管道 | PASS | `relay.go:80-89` 仅 Noise+pnet；`:75` 临时身份；circuit-relay v2 仅 HOP 不解密；`relay_integration_test.go:360` tee-proxy 证线上仅密文 |
| relay 访问控制（威胁模型 A） | PASS | 三层：pnet PSK + CapToken groupSig 验证（`captoken.go:127-154`）+ 每 token/group 配额（`authz.go:97-121`）；ACL 绑 libp2p 认证 peer.ID；rendezvous 反 DoS 上界（`rendezvous.go:168-197`） |
| coord 不持分片/不参与 MPC | PASS | coord 生产码无 `tss.`/`ecdsa.Sign`/分片处理；仅验证原语（VerifyProposerSig/VerifyMetaHash/verifyRSV recover-only）；`engine.go:17` 仅发 START 不入 tss |
| coord 不可重建密钥/伪造结果 | PASS | `callback.go:101-120` `verifyRSV` 用 `ecdsa.RecoverCompact`（恢复非签名）须等于 group pubkey；伪造 RSV→FAILED 无泄漏（`result.go:55-60`，`security_test.go:75` 断言） |
| 信封端到端完整性 | PASS | `engine.go:181-198` 逐字转发含 ProposerSig；`coord.go:241` `rebuildEnvelope` 比特一致重建，成员独立复验，coord 替换可检（`security_p6_test.go:204/240`） |
| 传输安全（Noise/peer 钉/无 MITM） | PASS | `transport.go:81-90` 仅 Noise+pnet PSK+yamux+TCP，peer ID=pubkey；`session.go:219-234` 每入站 AcceptInbound（版本+sessionId 隔离）+ VerifySenderAuth |
| cmd/node 双角色隔离（FIX-002） | PASS | `main.go:63-89` errgroup 并发，各角色仅读自身配置子树，relay↛coord/coord↛relay/transport↛both **零跨导入**；`main_test.go:34/76/106` 覆盖并发/任一错/单角色 |

**发现**：无 P0/P1/P2。P3（信息）：relay 身份每进程临时（设计有意，无信任问题）；callback URL 经本地 env 覆盖非 server.md 一等配置（部署管线缺口，无安全影响）。

---

## 6. 测试与质量保证 — **CONCERN**

| 子域 | 判定 | 关键实证 |
|---|---|---|
| 单元/集成测试覆盖 | PASS | 58 个产品 `_test.go`（不含 docs/design/）；16 个产品包仅 `cmd/cli` 无测试（薄 main 包装，`internal/cli` 重测） |
| 关键包覆盖 | PASS | internal/mpc、keystore、contract、server/coord、txdecode、transport 均有专测 |
| Go skip 情况 | PASS | 仅 `internal/cli/coordflow_test.go:194`、`e2e_test.go:37` 在 `-short` 跳过重 E2E 载体测试（全模式运行，正常） |
| E2E 真活完整性 | CONCERN | `full-ring.test.ts`/`expired.test.ts`/`docker-isolated.test.ts` 用 `it.skipIf(!gate.ok)` —— L1 裁定「实现并行、活跑串行」sanctioned 模式（`gate.ts:1-60`）；记录终态门 GREEN 见 §1-P2 |
| 权威 -race 校准门 | CONCERN（透明性） | 见下透明性声明 |

**透明性声明（非鸦片图章的诚实边界）**：本审计**未独立重跑**权威 -race 校准门 `CGO_ENABLED=1 rtk test go test -race -count=1 -timeout=1200s -p 1 ./...`（该重型 20 分钟级运行在本工作流被授权方拒绝执行）。可援引的独立/记录证据：(a) 构建/供应链审计代理本轮独立 `rtk err 'go test ./...'` rc=0、`go mod verify` rc=0；(b) 本审计定向 `rtk err` 跑 `TestMemberAuthReplay` 于越过时间炸弹触发墙钟后 rc=0；(c) 已记录 GREEN：`docs/task/index.md` reconcile 表 HEAD `fdf738a` 15 包全 ok rc=0、MEXT-001 post-merge @`0ed7f33` 15 包全 ok、终态门活跑 2 pass/2 skip/0 fail。结论：权威 -race 门有**实质但非完全**的独立佐证；建议 L1/L2 在终签前安排一次新鲜独立 -race 全树门复跑作终态背书。

**发现**

- **[P2] 权威 -race 校准门本审计未独立复跑** — 见上透明性声明。非门红，属审计完备性缺口；以记录 GREEN + 本轮独立非 -race 真跑佐证。
- **[P3] `cmd/cli` 包无 `_test.go`** — 薄 main 包装（`internal/cli` 已重测），影响低。
- **[P2] E2E 真活绿不可由静态审计独立复现** — 与 §1-P2 同源；终签前应捕获 gate 开态非占位活跑日志。

---

## 7. 构建发布工程与依赖供应链 — **PASS**

| 子域 | 判定 | 关键实证 |
|---|---|---|
| A1 go.mod 基线钉死 + replace | PASS | `go.mod:3` `go 1.25.7`（唯一权威，无 go1.23 回退）；`go.mod:135` `replace tss-lib/v3 => ./docs/design/tss-lib`；`go mod verify` rc=0；vendored tss-lib 纯 Go 无 `import "C"`，MIT LICENSE 在 |
| A2 依赖闭包缺口（历史风险） | **PASS（风险退役）** | `rtk err 'go build ./...'` rc=0、`'go vet ./...'` rc=0 —— bootstrap require 集已闭合真实导入，历史缺口**已解决**，0 P0/P1 |
| A3 CI/lint/Taskfile | PASS | `.github/workflows/ci.yml` gofmt+vet+golangci-lint v2.11.4+build+test+`go mod tidy` diff-gate，actions SHA 钉死；`.golangci.yml` standard+revive/errorlint/bodyclose/sloglint/noctx，tss-lib 排除；`Taskfile.yml` `ci` 镜像 CI |
| A4 gomobile bind 脚本 | PASS | `scripts/build-{android,ios}.sh` 参数固化、`-ldflags "-s -w"`、可复现；「静态/未执行/范围外」在脚本头与 P0-report §3-§4 明示 |
| A5 Docker E2E 打包 | PASS（1 P3） | `e2e/docker/docker-compose.yml` 隔离 bridge、无 host bind-mount、named volume 仅公开数据、secrets 经 `${VAR:?}`；基础镜像 tag 钉版但未 digest 钉死（P3） |
| A6 机密/产物/LICENSE | PASS | 无提交机密（命中均为弱口令黑名单/env 引用）；`.gitignore` 排除 `.env*`/`*.key`/`*.pem`/`*.keystore`/`*.shard`；`.dockerignore` 正确；根 MIT LICENSE 在 |

**发现**：**[P3]** E2E Docker 基础镜像未 digest 钉死（`Dockerfile.node:10` `golang:1.25` 等，仅测试镜像无生产产物影响）；**[P3 信息]** vendored `docs/design/tss-lib/go.mod:3` 声明 `go 1.23` —— 系上游 tss-lib v3.0.0 自身 directive，**非**主模块回退（主 `go.mod:3` 正确钉 go1.25.7 且构建/测试在其下通过），记此以预防误报。

---

## 8. 文档完整性与运维就绪 — **PASS**

| 子域 | 判定 | 关键实证 |
|---|---|---|
| B1 README 准确性 | PASS | quick-start `go build ./...` 实测 rc=0；结构表/依赖说明与 go.mod replace 一致 |
| B2 docs/design/+docs/ 一致性 | PASS | `docs/design/PLAN.md` §1 基线==go.mod（go1.25/protobuf v1.36.6/libp2p v0.43.0）；contract 目录完整；`docs/task/index.md` RA-001 正确 `[-]`；changelog/security-review 自洽 |
| B3 已知限制披露 | PASS | `docs/design/P0-report.md` §1/§3/§4(7 项枚举)/§5/页脚**广泛无歧义**披露移动/真机子门范围外/未执行；无其它未披露重大缺口 |
| B4 运维就绪 | PASS | `log/slog` 结构化日志；coord `/healthz`(`api.go:32`)、admin `/healthz`(`server.go:97`)、`/admin/relay/metrics`；配置 fail-fast(`internal/node/config.go:186-191`)；无日志泄密；部署拓扑见 `architecture.md` §3（relay/coord 独立部署） |

**发现**：无 P0/P1/P2/P3。披露质量评为典范。

---

## 9. P0 / P1 Remediation 清单（go-gate）

**P0（发布阻断）**：无。两项历史 P0 级风险（nonce 时间炸弹 / 依赖闭包缺口）经独立复核已确证退役。

**P1（发布前必修，go/no-go 支点）**：

| # | 发现 | 位置 | 必修动作 | 验收 |
|---|---|---|---|---|
| P1-1 | 联网 keygen/reshare 关闭 modulus/factor ZK 证明 | `internal/cli/mpcnet.go:171-172,259-260` | 移除 `SetNoProofMod()/SetNoProofFac()`；放宽 E2E 超时 | 带证明联网环 keygen+reshare+sign 全绿活跑日志 |
| P1-2 | 测试夹具进可编译产品路径 | `internal/cli/device.go:72-81` | `device.go` 加 `//go:build integration`，或 `preParamsFor` 移入测试专用包 | 生产构建不含 `LoadKeygenTestFixtures` 符号 |
| P1-3 | 移动 SDK 进程内签名非分布式 | `internal/mobileapi/sign.go:165-178` | 二选一：(a) go/no-go 显式将移动 SDK 分布式签名**移出本次发布范围**并文档化 CLI 为唯一功能载体；(b) 立项装配 `internal/transport`→`mobileapi.Sign`（单分片+OnWireMessage 泵） | 范围决策落 docs/design/ 或新任务交付并 E2E 验证 |
| P1-4 | RN 原生桥惰性桩 | `rn/bridge/ios/*`,`android/*` | go/no-go 显式确认本次发布**排除移动 App 交付物**；若移动在范围则为阻断，须 gomobile bind+桥装配立项 | 范围决策落 docs/design/ |

**P2（应修）**：admin-UI 登录无限流（`admin/ui.go:163`）；EVM RLP 递归无深度上限（`txdecode/evm.go:232`）；TRON 不可解析透传审批（`txdecode/tron.go:48`）；keystore GCM nonce 无计数器（`keystore/crypto.go:144`）；权威 -race 门未独立复跑（§6 透明性）；E2E 绿静态不可复现（§1/§6）；coorddb KDF sidecar 参数无边界（`coorddb/kdf.go:42`）。

**P3（注记）**：见各维度发现尾段（注释漂移、Docker digest 未钉、presence 用墙钟、域常量为 var、pre-EIP-155 仅 caution 等）。

---

## 10. 维度判定汇总

| # | 维度 | 判定 | P0 | P1 |
|---|---|---|---|---|
| 1 | 功能完整性与契约一致性 | CONCERN | 0 | 2（P1-3,P1-4） |
| 2 | 密码学正确性与签名安全 | CONCERN | 0 | 2（P1-1,P1-2） |
| 3 | 密钥与机密管理 | PASS | 0 | 0 |
| 4 | 服务端鉴权与防滥用加固 | PASS | 0 | 0 |
| 5 | 传输与编排零信任模型 | PASS | 0 | 0 |
| 6 | 测试与质量保证 | CONCERN | 0 | 0 |
| 7 | 构建发布工程与依赖供应链 | PASS | 0 | 0 |
| 8 | 文档完整性与运维就绪 | PASS | 0 | 0 |

**总判定：NO-GO（终端用户自托管钱包产品）/ CONDITIONAL-GO（签名内核组件里程碑，须先闭合 P1-1/P1-2，并就 P1-3/P1-4 作明确范围决策）。** 零信任模型、密钥管理、鉴权加固、供应链、文档运维内核坚实且零 P0；阻碍发布的是交付面的 4 项 P1，其中 **P1-1（唯一联网 keygen 关闭抗恶意方 ZK 证明）对自托管产品风险最高**，必修且须带证明重取证后方可对承载真实资产的部署放行。

---

_审计员：RA-001（L3，独立评估）。本文档为唯一新增件，零改已 finalized 产品件。证据为 HEAD `aa56c98` 静态分析 + 定向 `rtk` 真跑；权威 -race 全树门未在本工作流独立复跑（§6 透明性声明）。_
