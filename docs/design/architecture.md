# 系统架构设计(共享)

> 全系统全景:组件、部署拓扑、数据流与时序、信任模型汇总。
> 决策来源 `PLAN.md`;细节见 `server/server.md`、`server/database.md`、`contract/api.md`、`contract/protocol.md`、`mcp/sdk.md`、`security.md`。
> 性质:开发文档,不写代码。

## 1. 系统上下文

```
┌─ 外部业务服务(范围外)─┐   产生交易/签名需求 · 持链逻辑 · 负责广播
└───────────┬────────────┘
            │ REST(提交信封 / 接收 {R,S,V})  —— contract/api.md
┌───────────▼────────────────────────────────┐
│  node 服务端(本项目)                        │
│  · coord 角色:待签列表 / 编排 / 对接          │  —— server/server.md
│  · relay 角色:零信任 MPC 传输中转            │     server/database.md
└───────────┬────────────────────────────────┘
            │ 推送/拉取 + START(成员API) · libp2p Noise(MPC)
┌───────────▼────────────┐ ┌────────────┐ ┌────────────┐
│ 成员手机 A(mcp SDK)    │ │ 成员手机 B │ │ 成员手机 C │  —— mcp/sdk.md
│ MPC 分片 / keystore /   │ │  …         │ │  …         │
│ tx-decode / 审批 UI     │ └────────────┘ └────────────┘
└─────────────────────────┘
```

参与方:**外部业务服务**(不可信,范围外)、**node**(coord 信任最小化 + relay 零信任)、**成员设备/mcp SDK**(与持有者一起构成最终信任根)。

## 2. 组件与职责

| 组件 | 归属 | 职责 | 详见 |
|---|---|---|---|
| 外部业务服务 | 范围外 | 构造交易、组装信封、广播、消费 `{R,S,V}` | — |
| coord 角色 | server | 收信封→待签列表、连通性追踪、法定人数发起、对接回传、推送 | server/server.md C 部分 |
| relay 角色 | server | libp2p circuit-relay v2 + rendezvous,零信任哑管道 | server/server.md R 部分 |
| coord 持久化 | server | SQLite:待签列表/状态机/组公钥/审计/推送 token(relay 无状态) | server/database.md |
| 管理面 | server | 单一运维管理员:交易/历史会话查看、防滥用、审计;不签发准入 | server/admin.md |
| mcp SDK | mcp | MPC(keygen/sign/reshare)、keystore、tx-decode、transport、RN 桥 | mcp/sdk.md |
| walletcli | mcp | PC 钱包成员端(`cli` shell + `cli serve` HTTP + htmx UI):WYSIWYS 审批 / 备份导入 / 离线地址派生;非生产工具,本机运维与 E2E 调试用 | mcp/walletcli-ui.md |
| 契约 | contract | 信封、API、libp2p 协议、心跳、START | contract/api.md, contract/protocol.md |

## 3. 部署拓扑

- `node` 单二进制,`relay.enable`/`coord.enable` 配置开关。
- **默认**:`relay+coord` 合并单体,起步最简。
- **可扩展**:coord 单一逻辑权威(SQLite 单节点 + 文件备份/Litestream 容灾,非多写 HA);relay 多副本无状态,客户端持可配置 relay 列表自动故障转移;第三方可自建仅 relay 实例。
- 合并部署**不削弱 relay 密码学零信任**:Noise 两 party 端到端,coord 明文信封经 API 另一路径,不经 relay 转发(详见 server/server.md 首部)。

## 4. 关键时序

### 4.1 DKG(keygen,P1/P3)
```
组初始化 → 各成员设备端上后台生成 PreParams(进度UI,严禁后端)
→ 经 relay 跑 tss keygen → 各持一分片;主公钥公开(coord 仅存公钥)
```

### 4.2 签名(sign)
```
① 外部服务 POST 信封 → coord 入待签列表(PENDING)、推送通知成员
② 成员上线连 relay,签名心跳上报在线(coord 维护在线集)
③ 成员审批;coord 评估:在线∩审批∩未过期 ≥ t → 选 signers → DISPATCHED,下发 START
④ 各 signer 设备:tx-decode 解析 unsignedTx + 重算链摘要断言 ==digest32
   → A/B 分区展示 + 声明式核对 → 用户审核(WYSIWYS)→ 进 MPC 前再校验未过期
⑤ signers 经 relay 跑 tss 签名(coord 不参与)→ {R,S,V}
⑥ 指定一方上报 coord → coord 用组公钥验签 → 回传外部服务广播(RETURNED)
```

### 4.3 丢失成员 resharing(P5)
```
某成员设备/分片丢失 → 剩余 ≥ t 方发起 resharing → 重建缺失分片(或换新成员)
→ 主公钥不变,地址不变;期间窗口降冗余,完成即恢复 t/n
```

### 4.4 连通性心跳(A2)
```
成员连 relay 后周期发签名心跳 {memberId,groupId,relayPeerID,ts,sig} → coord 验签维护在线集(TTL 过期移除);relay 不参与上报
```

## 5. 数据流与平面划分

- **数据平面(MPC)**:成员↔成员,go-libp2p Noise,经 relay 中转;relay 读不到内容。
- **控制/编排平面**:成员↔coord(推送/拉取/审批/心跳/START/结果)、外部服务↔coord(信封/回传);明文对 coord 可见(信任最小化)。
- 两平面物理路径分离 → relay 零信任与 coord 信任最小化互不污染。

## 6. 信任模型汇总(详见 security.md)

| 主体 | 信任级别 | 能做 | 不能做(被什么挡) |
|---|---|---|---|
| 外部业务服务 | 不可信 | 提交任意信封/businessInfo | 让成员误签 —— 设备侧 tx-decode 摘要断言 + WYSIWYS 人审 |
| relay 角色 | 零信任 | 丢弃/延迟/审查流量 | 读/改 MPC 内容、伪造发件人(Noise 端到端 + peerID=公钥) |
| coord 角色 | 信任最小化(资金)/隐私可信 | DoS/审查、知交易隐私 | 偷钱/伪造审批/令各方签不同内容/篡改 businessInfo(TSS 门限 + WYSIWYS + 同摘要绑定 + proposerSig) |
| 运维管理员(单一) | 隐私/运维可信,资金不可信 | 查交易+历史会话、控配额/封禁 | 偷钱/伪造审批/签发准入/看分片(门限+WYSIWYS+自主式不变+审计) |
| 成员设备+持有者 | 信任根 | 持分片、审批 | 单方动用资金(需 ≥ t 方) |

## 7. 横切不变量

1. **禁止盲签 / WYSIWYS**:进 MPC 前必经 tx-decode 重算 `==digest32` + A/B 展示 + 人审。
2. **PreParams 端上生成**:含 Paillier 私钥,严禁后端预生成下发。
3. **TTL 一等公民**:coord 与设备双侧校验过期,requestId 不复用。
4. **同摘要绑定**:TSS 要求各方签同一 digest,天然抵御分裂攻击。
5. **结果免信任校验**:coord 回传前用组公钥验 `{R,S,V}`。
6. **链无关内核**:库只产 `{R,S,V}`;构造/广播在外部服务。
