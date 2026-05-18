# mock-extsvc（外部业务服务仿真，测试专用）

> MEXT-001。**非产品服务**——仅为 E2E（`docs/design/testing.md §3.1`）提供"外部业务服务侧"测试替身，位于 `tests/mock/`（与 `tests/e2e/` 同级），遵 `pma-bun`。

## 职责（testing.md §3.1 三能力）

- **(a) 提交签名信封**：`POST /v1/requests`（api.md A2），自构 `metaHash` 与 `proposerSig`，与 Go `internal/contract` 规范化序列化**逐字节一致**。
- **(b) 申请组地址**：`GET /v1/groups/{groupId}/public`（api.md A1 / XA-001），取 `evm_address` / `tron_address` / `ecdsa_pubkey`。
- **(c) 接收并验签 {R,S,V}**：webhook（A4）或 longpoll（A3），用三链真实摘要做 `ecrecover`（ETH/BSC，EIP-55）与 TRON（Base58Check）地址比对，复核 coord 的组公钥门（callback.go `verifyRSV`）。

## 跨语言逐字节一致的保证

`testdata/golden.json` 由**真实 Go 包**（`internal/contract` + `internal/addr`）经 `testdata/gen/main.go` 生成，是权威锚。`test/golden.test.ts` 断言 TS 实现对规范化预映像、`metaHash`(RFC 8785 JCS)、`proposerSig`(secp256k1 DER, RFC6979)、`EmptyMetaHash`、EVM/TRON 地址派生、RSV 恢复**逐字节复现** Go 输出；任一侧漂移即 CI 失败。

重新生成（改动 contract 规范化或 addr 派生后，须在仓库根执行）：

```bash
go run ./mock-extsvc/testdata/gen > mock-extsvc/testdata/golden.json
```

## 质量门

```bash
bun install
bun run lint        # eslint @antfu
bun run typecheck   # tsc --noEmit (strict, noUncheckedIndexedAccess)
bun test            # bun:test —— 56 用例（48 契约 + 8 控制面）
bun test --coverage # >80 门
```

## 配置（启动时 Zod 校验，见 `src/config.ts`）

`COORD_BASE_URL`（必填）、`COORD_API_KEY` / `COORD_API_KEY_HEADER`、`RESULT_MODE`=`webhook|longpoll`、`WEBHOOK_*`、`LONGPOLL_WAIT_S`、`RESULT_DEADLINE_MS`、`PORT`（控制面端口）、`MOCKEXT_PROPOSER_PRIVKEY_HEX`（提议者身份私钥，测试专用，确定性默认 `'11'*32`，便于 harness/coord 预知 proposer 公钥）。mTLS 不在进程内 E2E 范围；api_key 为支持的 `coord.external.auth` 模式。

## HTTP 控制面（E2E-001 子进程拓扑，testing.md §3.1）

`bun run start`（= `bun src/server.ts`）拉起长驻控制面（`src/server.ts` `ControlServer`，**薄包装既有已验证库 API，零改 byte-exact 密码学/契约**），读 env `PORT`/`COORD_BASE_URL`/`COORD_API_KEY`，harness 拥有生命周期（SIGKILL）。端点（契约 `e2e/src/lib/mock-extsvc.ts:42-55`）：

| 方法 路径 | 响应 |
|---|---|
| `GET /healthz` | `200 {status:"ok"}`（harness 就绪轮询，<20s） |
| `POST /control/request-address {groupId}` | `200 {ecdsaPubkeyB64,evmAddress,tronAddress}`（驱 coord A1） |
| `POST /control/submit {groupId,chain,digest32Hex,unsignedTxHex,requestId,expiryMillis}` | `202 {requestId}`（驱 coord A2，复用 byte-exact proposerSig/metaHash） |
| `GET /control/result/{requestId}` | `200 {status,rsvB64?,recovered?}`（coord A4 + 验签；未知 id→404） |

## E2E-001 库用法（备选）

`MockExtSvc.run(input, proposerPriv)` 跑完整环：A1 取组 → 自构信封 A2 提交 → 等终态 → `RETURNED` 时独立验 {R,S,V} 对 A1 地址；`EXPIRED/REJECTED/FAILED` 不验签直接回传（§3.2「外部收 EXPIRED」）。
