# 测试策略(共享)

> mcp(SDK)与 server(node)分章;覆盖单元/集成/E2E + 安全专项。
> 关联 `mcp/sdk.md`、`server/server.md`、`server/database.md`、`contract/*`、`security.md`、`P0-tasks.md`。
> 全局基线:**覆盖率 ≥ 80%**,TDD(先红后绿),修实现不改对(除非对错)。性质:开发文档,不写代码。

## 1. mcp(SDK)测试

### 单元
- `tx-decode`:**安全攸关,最高强度** —— 三链(ETH/BSC legacy+EIP-1559+ERC20+合约调用、TRON 原生+TRC20+Stake2.0)**真实交易语料** + **模糊测试**;断言「解析→重算摘要==digest32」;篡改/错误填充 → 必须拒签;未识别调用 → 「原始 + 告警」不臆造。
- `keystore`:加密/解密/口令错误/导入导出;绝不明文落盘。
- `mobile-api`:扁平接口编解码;callback 时序;错误码分类(安全类硬拒)。
- `addr`:公钥→ETH/TRON 地址向量对照。

### 集成
- 进程内多 party keygen/sign/reshare 端到端(参考 `tss-lib/ecdsa/*/local_party_test.go`)。
- WYSIWYS 流程:信封校验→tx-decode→A/B→审批→MPC→未过期再校验(任一失败中止)。
- 丢失成员 resharing:主公钥/地址不变。

### 设备/打包(P0/P4)
- 路径 A:gomobile .aar/.xcframework 真机 keygen(P0-tasks.md T3/T4)。
- PreParams 后台不卡 UI、达阈值;**禁后端预生成**回归。
- RN 桥冒烟:扁平 API 跨 JS 不丢类型(T5)。

## 2. server(node)测试

### 单元
- coord 状态机:全迁移合法/非法路径;`request_events` 同事务;并发 `FOR UPDATE` 防双发 START。
- 法定人数算法:在线∩审批∩未过期≥t 的边界;signer 掉线回退重选。
- TTL:过期 (a)~(e) 五规则;时钟 skew 容差;requestId 禁复用。
- 回传前组公钥验签:伪 RSV → FAILED 不外泄。
- 配置:文件+env 覆盖优先级;双角色 false 报错;secret 缺失 fail-fast。

### 集成
- coord + relay 合并/拆分两种部署。
- `contract/api.md` A/B 全端点契约测试(鉴权、幂等、错误码、过期 410)。
- DB:迁移、过期局部索引命中(EXPLAIN)、加密列泄库不泄明文、重启恢复未过期请求。

## 3. E2E(P3)

### 3.1 完整端到端拓扑(用户裁定 2026-05-18)

E2E **必须**做完整端到端,由两个测试替身配对组成(均非产品,仅测试):
- **成员侧** = CLI-001 harness(Go,多进程模拟 2-of-3 经 libp2p Noise+circuit-relay 跑 keygen/sign/reshare)。
- **外部业务服务侧** = **`mock-extsvc`(Bun/TypeScript,新增,与 cli/、rn/ 同级,遵 pma-bun 规范)**:模拟 PLAN「外部业务服务」,**支持 (a) 向 coord 提交交易签名信封**(`POST /v1/requests`,自构 `proposerSig`/`metaHash`)、**(b) 从 coord 申请地址**(`GET /v1/groups/{groupId}/public` 取 evm/tron 派生地址,见 api.md A1)、(c) 经 webhook 或 longpoll 接收 `{R,S,V}` 并验签。
  > **进程模型(L1 裁定 2026-05-18,消 §3.1 规约含糊——跨交付物启动契约不匹配根因)**:`mock-extsvc` **= 独立子进程长驻 HTTP server**(非进程内库)。必须提供 `package.json` `start` 脚本 + `GET /healthz`(就绪探测,E2E harness 轮询)+ coord webhook 回调接收端点 + 其驱动 coord 所需客户端逻辑。理由:§3.1(c) 要求经 **webhook 接收 `{R,S,V}`**——webhook 回调必须 mock-extsvc 为监听 server(进程内库收不到 coord 的 webhook POST);且贴合本 §3.1 子进程拓扑(Go 二进制亦子进程)与真实「外部业务服务」即独立服务。已 finalized 的 `MockExtSvc` 库 API(字节一致 proposerSig/metaHash + 契约,48 测试绿)**作为内核被该 HTTP 入口包裹复用,密码学/契约核零改动**。

**E2E 测试用例与编排语言 = Bun + TypeScript(用户裁定 2026-05-18,非 Go)**:E2E-001 交付物 = 一个 **Bun/TS 测试套件**(遵 pma-bun;可置于 `mock-extsvc/` 内或与之同级的 `e2e/` Bun 项目)。它**以外部进程方式驱动** Go 侧成员二进制(CLI-001 harness,Go 编译产物)与 `node`(coord+relay),编排完整环路并在 **TS 中编写断言**。语言切分:成员 MPC 进程=Go 二进制(被 TS 作子进程拉起);E2E 编排+测试用例+断言+mock-extsvc=Bun/TS。

完整环路(Bun/TS E2E 测试编排):启动 node(coord+relay)→ ProvisionGroup → `mock-extsvc` 申请地址(A1 `GET /v1/groups/{groupId}/public`,断言 evm/tron 正确)→ 多成员 CLI(Go 子进程)上线/心跳 → `mock-extsvc` 提交信封(A2)→ 推送/拉取 → 法定人数 → START → 各成员 tx-decode+人审 → MPC 签名 → coord 组公钥验签 → `{R,S,V}` 回传 `mock-extsvc` → TS 用例用三链真实摘要 `ecrecover`/TRON 验签断言;过期路径断言 EXPIRED。

**并行性(用户裁定 2026-05-18)**:`mock-extsvc` 与 Bun/TS E2E 用例**不依赖未合并的 Go 实现**——仅依赖已存在的契约(`api.md` A1/A2 已提交、`internal/contract` 已合并的 proposerSig/metaHash 规范、CLI/node 进程 CLI 接口)。故其**实现/编写可与 FIX-001/XA-001 等 Go 工作并行**(各自 worktree,零文件重叠);仅最终**活集成跑通**(对完整已合并系统执行并全绿)属串行最后一步(待 FIX-001+XA-001+MEXT-001 merged)。即「实现并行,finalize/活跑串行」。

### 3.2 既有 E2E 断言(保留)

- 三机经 relay+coord:外部服务提交信封 → 推送/拉取 → 心跳 → 法定人数 → START → tx-decode+人审 → MPC → {R,S,V} 验签 → 回传外部服务。
- 过期请求两侧均拒,外部收 EXPIRED。
- V/low-S 用三链真实摘要交叉验证(`ecrecover` / TRON 验签)。

### 3.3 Docker 隔离模拟机 E2E 拓扑(用户裁定 2026-05-18)

§3.1 单机多进程 E2E(E2E-001,已 finalized GREEN)**保留不动**(快速门、零回归)。**新增** Docker 隔离 E2E(本环境已验 Docker 27.5.1/daemon UP/Compose v2.33.0 可真跑,非范围外):

- **拓扑**:各模拟机各自独立容器,经真实 Docker 网络通信(**无 127.0.0.1 localhost 捷径**,真实网络边界):① `node` 容器(relay+coord 双角色并发,FIX-002;dev 加密禁用 + `ALLOW_INSECURE_DB=1`,FIX-003)② N 个成员设备容器(各跑 CLI-001 成员 harness,**各自独立 keystore/文件系统隔离**,模拟 2-of-3 不同手机)③ `mock-extsvc` 容器(HTTP 控制面 server,coord 经 Docker 网络回调其 webhook)。
- **复用**:复用既有 `e2e/` Bun/TS harness + node/CLI/mock-extsvc 既有二进制与镜像内构建;**密码学/契约/MPC/已 finalized 产物零改动**。新增交付 = Dockerfile(node/member-cli/mock-extsvc)+ `docker compose` 编排 + Bun/TS harness 的容器编排适配器(compose up→healthcheck→跑环→断言→compose down)。
- **断言**:同 §3.1 完整环(provision→A1→A2→法定人数→MPC keygen/sign/reshare 经真实 libp2p Noise+circuit-relay **跨容器**→{R,S,V}→webhook→三链 ETH/BSC/TRON 恢复验签)+ EXPIRED;额外验真实网络隔离下 relay 中转必经(无 localhost 直连)、各设备 keystore 物理隔离。
- **定位(用户裁定)**:Docker E2E 为**更强隔离的补充验证**,与 §3.1 localhost E2E 并存(后者为快速门);Docker E2E 真 GREEN 纳入最终验收完成条件(故项目由「待验收」转为「核心已交付 finalized + Docker-E2E 硬化在制,最终验收增此门」)。**不得 un-finalize/回归任何已交付件**(零回归红线)。

### 3.4 分布式 MPC E2E(DM-6 §G 验收,`e2e-distributed-mpc`)

DM-6 closure-gate(commit `e70bd75`)产出独立 Bun/TS 套件
`tests/e2e/test/e2e-distributed-mpc/`,验证真分布式 MPC(n 方各自独立进程 +
真实 libp2p 路径,**非** CLI-001 单机多进程仿真)。

- **门控(`gate.ts`)**:
  - `attestationOnlyGate()` —— 需 Go toolchain + `coorddb.CommitAttestationQuorum`
    存在(DM-6 主提交;已在 main)。**默认可跑**(`attestation-quorum.test.ts`)。
  - `realMpcGate()` —— `attestationOnlyGate` + `internal/cli/host_transport.go`
    存在(DM-5 主提交;已在 main)+ **显式开关 `E2E_DMPC=1`**(默认 skip,
    防 CI 误触发长跑)。
- **用例(4 件)**:
  - `attestation-quorum.test.ts`:同事务 B11 commit 路径(幂等、INCONSISTENT、R7 violation)
  - `keygen-3of3.test.ts` / `sign-2of3.test.ts` / `reshare.test.ts`:真 n 方
    keygen / sign / reshare 经 DM-5 host_transport 跑通。
- **运行**:`cd tests/e2e && E2E_DMPC=1 bun test test/e2e-distributed-mpc`;
  默认 CI 跑 attestation-quorum,real-MPC 三件作可选门(operator 主动开启)。
- **定位**:E2E-001(§3.1)+ E2E-002(§3.3)是回归门(单进程/Docker 仿真);
  e2e-distributed-mpc 是**真分布式实证门**,DM-1..DM-6 闭环硬判据。

## 4. 安全专项(对应 security.md §5)

每条攻击对策须有对应用例:relay 抓包仅密文且无法伪造 `from`;跨 sessionId 注入被丢弃;无 PSK/能力令牌无法预约 relay/注册 rendezvous;分裂攻击(不同成员不同信封)产不出有效签名;重放被拒;`tx-decode` 模糊不产生「误签」(只产「拒签」)。

## 5. 阶段门禁

| 阶段 | 必过 |
|---|---|
| P0 | 真机 keygen + RN 冒烟 + PreParams 阈值(P0-tasks.md 退出标准) |
| P1 | 进程内 keygen/sign/reshare 集成绿 |
| P2 | libp2p+relay 替换内存通道;零信任抓包用例 |
| P3 | coord 状态机/TTL/API 契约 + tx-decode 模糊语料 + E2E 三机 |
| P4 | 设备打包 + RN 桥 sign 真机 |
| P6 | 安全专项全绿 + 第三方审计 + 覆盖率 ≥ 80% |

## 6. 工具

- Go:`go test`(`-race`)、`testing`/`fuzz`、`go vet`;覆盖率门槛 CI 强制。
- 契约:OpenAPI 校验 + 合同测试(api.md)。
- 设备:真机矩阵(iOS/Android 多档位)P0/P4。
- 不引入对加密原语的自实现测试替身;以 tss-lib 既有测试模式为基。
- **tss-lib no-proof 测试模式边界(L1 裁定 2026-05-18,RA-001 P1-1,修订原 §6 措辞)**:「以 tss-lib 既有测试模式为基」**仅指 dev/test 跑测时**为适配 N-002 relay 时长上限可用 no-proof 加速;**不授权生产联网 keygen/reshare 关闭 Paillier 证明**。生产联网路径必带证明(security.md 不变量 #10);no-proof 须显式门控 + 生产 fail-closed 护栏。relay 时长上限须对 keygen 放宽以容纳带证明耗时,而非靠关证明迁就。
