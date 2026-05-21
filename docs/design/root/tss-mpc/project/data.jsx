// data.jsx — B-side data model: groups, members, envelopes, audit

const MEMBER = {
  memberId: 'm0',
  identityPub: '0x04a2c9f1...8e3b',
  partyIndex: 0,
  device: 'iPhone 15 Pro',
};

const COORD = {
  endpoint: 'https://coord.zzci.io',
  tls: 'mTLS',
  status: 'connected',         // connected | locked | offline
  latencyMs: 38,
  lastHeartbeat: 'just now',
  relayPeerID: '12D3Koo…7Q2x',
};

const WALLETS = [
  {
    groupId: 'grp_5a8e7b3c',
    moniker: '财库主钱包',
    threshold: 2,
    parties: 3,
    members: [
      { id:'m0', label:'本机 · iPhone',     self:true,  status:'online', last:'刚刚' },
      { id:'m1', label:'同事 · Pixel 9',     self:false, status:'online', last:'4 秒前' },
      { id:'m2', label:'冷端 · 旧手机',      self:false, status:'standby', last:'2 天前' },
    ],
    ecdsaPubkey: '02f8a3...c4e1',
    chaincode:   '8f1a02bc...4d29',
    addresses: [
      { id:'a0', label:'主账户 · EVM', chain:'eip155:1',     chainLabel:'Ethereum',  path:"m/0",   address:'0xA4f3CBd2...23E89bF1', isDefault:true },
      { id:'a1', label:'主账户 · TRON', chain:'tron',         chainLabel:'TRON',      path:"m/0",   address:'TQrZ8jX2pVdK...P2hKw',     isDefault:true },
      { id:'a2', label:'热包 · 热提现',     chain:'eip155:1',     chainLabel:'Ethereum',  path:"m/0/1", address:'0x7c2EAa19...9f04B12C' },
      { id:'a3', label:'L2 结算',         chain:'eip155:42161', chainLabel:'Arbitrum',  path:"m/0/2", address:'0x9a01F4dB...12cAe7Ef' },
      { id:'a4', label:'场外 OTC',        chain:'tron',         chainLabel:'TRON',      path:"m/0/1", address:'TN8jKQp9wEZ...4VrPmL'      },
    ],
    xpubReady: true,
  },
  {
    groupId: 'grp_2c4d9f1a',
    moniker: '运营备用钱包',
    threshold: 2,
    parties: 4,
    members: [
      { id:'m0', label:'本机 · iPhone',  self:true,  status:'online', last:'刚刚' },
      { id:'m1', label:'CTO · iPad',     self:false, status:'online', last:'12s' },
      { id:'m2', label:'CFO · Mac',      self:false, status:'offline', last:'1h' },
      { id:'m3', label:'冷端 · Yubikey', self:false, status:'standby', last:'3 天前' },
    ],
    ecdsaPubkey: '03b7d2...91fa',
    chaincode:   '2c4d9f1a...8e07',
    addresses: [
      { id:'a0', label:'主账户 · EVM', chain:'eip155:1',     chainLabel:'Ethereum',  path:"m/0",   address:'0x71c8E9aF...AaB304E2', isDefault:true },
      { id:'a1', label:'主账户 · TRON', chain:'tron',         chainLabel:'TRON',      path:"m/0",   address:'TN9xRpQ7vZJ...XmZ4d',      isDefault:true },
      { id:'a2', label:'营销活动',         chain:'eip155:1',     chainLabel:'Ethereum',  path:"m/0/1", address:'0x3bD9c81E...7f201A0B' },
    ],
    xpubReady: true,
  },
];
// keep GROUPS alias for back-compat
const GROUPS = WALLETS;

// Pending signing envelopes (received via B6 dispatch)
const ENVELOPES = [
  {
    requestId: 'req_9c3a82e1',
    groupId: 'grp_5a8e7b3c',
    chain: 'eip155:1',
    chainLabel: 'Ethereum',
    proposer: 'biz-svc-1',
    proposerLabel: 'Treasury Service',
    expiryISO: '2026-05-21T14:23:00Z',
    expiresIn: 412,                 // seconds remaining
    receivedAt: '14:18:24',
    status: 'PENDING',
    digest32: '0x8a4f2c39e1b7d04f...c2a9',
    unsignedTxSummary: {
      to: '0xA4f3...B23E',
      toLabel: 'Binance 热钱包 (白名单)',
      value: '0.5 ETH',
      valueFiat: '≈ ¥10,656.42',
      nonce: 142,
      gasLimit: 21000,
      gasPrice: '24 gwei',
      data: '0x',
    },
    businessInfo: {
      title: '财库提现 · 周度结算',
      orderId: 'WTH-2026-0521-018',
      operator: 'alice@company.io',
      memo: '5 月 19 日审批通过 · 财务凭证 W-518',
    },
    metaHashOk: true,
    crossCheckOk: true,             // A facts vs B info
    decisions: [
      { id:'m0', state:'pending', self:true },
      { id:'m1', state:'pending', self:false },
      { id:'m2', state:'pending', self:false },
    ],
  },
  {
    requestId: 'req_2f8b14a7',
    groupId: 'grp_5a8e7b3c',
    chain: 'eip155:42161',
    chainLabel: 'Arbitrum One',
    proposer: 'biz-svc-2',
    proposerLabel: 'DEX Bot',
    expiryISO: '2026-05-21T15:02:00Z',
    expiresIn: 2310,
    receivedAt: '14:11:02',
    status: 'PENDING',
    digest32: '0x42b8a09c7e1d5f33...9210',
    unsignedTxSummary: {
      to: '0x1c8E...4421',
      toLabel: '未识别合约',
      value: '0 ETH',
      valueFiat: 'approve(spender, 0)',
      nonce: 89,
      gasLimit: 56200,
      gasPrice: '0.04 gwei',
      data: '0x095ea7b3...',
    },
    businessInfo: {
      title: 'Uniswap v4 路由授权撤销',
      orderId: 'OPS-2026-0521-009',
      operator: 'ops@company.io',
      memo: '撤销旧路由器无限授权',
    },
    metaHashOk: true,
    crossCheckOk: true,
    decisions: [
      { id:'m0', state:'pending', self:true },
      { id:'m1', state:'approved', self:false },
      { id:'m2', state:'pending', self:false },
    ],
  },
  {
    requestId: 'req_71e9c0d4',
    groupId: 'grp_2c4d9f1a',
    chain: 'tron',
    chainLabel: 'TRON',
    proposer: 'biz-svc-1',
    proposerLabel: 'Treasury Service',
    expiryISO: '2026-05-21T14:35:00Z',
    expiresIn: 1140,
    receivedAt: '14:09:51',
    status: 'PENDING',
    digest32: '0xf3092ab48d1c7e60...c8e1',
    unsignedTxSummary: {
      to: 'TYwJ...9pKz',
      toLabel: '⚠ 不在白名单',
      value: '120,000 USDT',
      valueFiat: '≈ ¥867,000',
      nonce: 0,
      gasLimit: 0,
      gasPrice: 'feeLimit 80 TRX',
      data: 'TRC20.transfer',
    },
    businessInfo: {
      title: '客户付款 · #C-2491',
      orderId: 'WTH-2026-0521-021',
      operator: 'cs@company.io',
      memo: '客户 #C-2491 · 退款 12 万 USDT',
    },
    metaHashOk: false,              // ← MISMATCH
    crossCheckOk: false,
    mismatchHint: 'businessInfo metaHash 与提议者签名不匹配',
    decisions: [
      { id:'m0', state:'pending', self:true },
      { id:'m1', state:'pending', self:false },
      { id:'m2', state:'pending', self:false },
      { id:'m3', state:'pending', self:false },
    ],
  },
];

// Audit history — references wallet by moniker for display
const AUDIT = [
  { t:'14:08', d:'今天', requestId:'req_4b2c91',  group:'财库主钱包',   chain:'Ethereum', op:'signed',   value:'0.25 ETH',     to:'0xA4f3...B23E',  rsv:'0x8a4f...32 (65B)' },
  { t:'11:47', d:'今天', requestId:'req_88a01e',  group:'运营备用钱包',   chain:'Arbitrum', op:'rejected', value:'1,200 USDC',   to:'0x9c1e...44ad',  reason:'白名单不匹配' },
  { t:'09:22', d:'昨天', requestId:'req_3c12d9',  group:'财库主钱包',   chain:'Ethereum', op:'signed',   value:'0 ETH',        to:'Uniswap v4',     rsv:'0x71f8...1b (65B)' },
  { t:'15:03', d:'2 天前', requestId:'req_99ef02', group:'财库主钱包', chain:'TRON',     op:'signed',   value:'8,500 USDT',   to:'TPx...4Q9',      rsv:'0xc02d...4a (65B)' },
  { t:'10:51', d:'2 天前', requestId:'req_71b3aa', group:'运营备用钱包', chain:'Ethereum', op:'expired',  value:'0.04 ETH',     to:'0x1c8E...4421',  reason:'未在 10 分钟内决策' },
  { t:'09:00', d:'3 天前', requestId:'req_2e8f01', group:'财库主钱包', chain:'Ethereum', op:'signed',   value:'2 ETH',        to:'0xA4f3...B23E',  rsv:'0x4d2a...90 (65B)' },
];

Object.assign(window, { MEMBER, COORD, WALLETS, GROUPS, ENVELOPES, AUDIT });
