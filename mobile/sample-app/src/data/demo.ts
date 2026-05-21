// Static demo data for the Trine Signer sample app. Adapted from the
// read-only design handoff (design/tss-mpc/project/data.jsx) with all
// visible copy translated to English to match the sample-app convention.
// Later L3s replace pieces of this with live SDK state.

import type {
  AuditEvent,
  CoordStatus,
  Member,
  SettingsSection,
  SigningEnvelope,
  Wallet,
} from './types';

export const MEMBER: Member = {
  memberId: 'm0',
  identityPub: '0x04a2c9f1...8e3b',
  partyIndex: 0,
  device: 'iPhone 15 Pro',
};

export const COORD: CoordStatus = {
  endpoint: 'https://coord.zzci.io',
  tls: 'mTLS',
  status: 'connected',
  latencyMs: 38,
  lastHeartbeat: 'just now',
  relayPeerID: '12D3Koo…7Q2x',
};

export const WALLETS: ReadonlyArray<Wallet> = [
  {
    groupId: 'grp_5a8e7b3c',
    moniker: 'Treasury main',
    threshold: 2,
    parties: 3,
    members: [
      { id: 'm0', label: 'This device · iPhone', self: true, status: 'online', last: 'just now' },
      { id: 'm1', label: 'Coworker · Pixel 9', self: false, status: 'online', last: '4s ago' },
      { id: 'm2', label: 'Cold · old phone', self: false, status: 'standby', last: '2 days ago' },
    ],
    ecdsaPubkey: '02f8a3...c4e1',
    chaincode: '8f1a02bc...4d29',
    addresses: [
      { id: 'a0', label: 'Main · EVM', chain: 'eip155:1', chainLabel: 'Ethereum', path: 'm/0', address: '0xA4f3CBd2...23E89bF1', isDefault: true },
      { id: 'a1', label: 'Main · TRON', chain: 'tron', chainLabel: 'TRON', path: 'm/0', address: 'TQrZ8jX2pVdK...P2hKw', isDefault: true },
      { id: 'a2', label: 'Hot withdrawals', chain: 'eip155:1', chainLabel: 'Ethereum', path: 'm/0/1', address: '0x7c2EAa19...9f04B12C' },
      { id: 'a3', label: 'L2 settlement', chain: 'eip155:42161', chainLabel: 'Arbitrum', path: 'm/0/2', address: '0x9a01F4dB...12cAe7Ef' },
      { id: 'a4', label: 'OTC desk', chain: 'tron', chainLabel: 'TRON', path: 'm/0/1', address: 'TN8jKQp9wEZ...4VrPmL' },
    ],
    xpubReady: true,
  },
  {
    groupId: 'grp_2c4d9f1a',
    moniker: 'Operations backup',
    threshold: 2,
    parties: 4,
    members: [
      { id: 'm0', label: 'This device · iPhone', self: true, status: 'online', last: 'just now' },
      { id: 'm1', label: 'CTO · iPad', self: false, status: 'online', last: '12s ago' },
      { id: 'm2', label: 'CFO · Mac', self: false, status: 'offline', last: '1h ago' },
      { id: 'm3', label: 'Cold · YubiKey', self: false, status: 'standby', last: '3 days ago' },
    ],
    ecdsaPubkey: '03b7d2...91fa',
    chaincode: '2c4d9f1a...8e07',
    addresses: [
      { id: 'a0', label: 'Main · EVM', chain: 'eip155:1', chainLabel: 'Ethereum', path: 'm/0', address: '0x71c8E9aF...AaB304E2', isDefault: true },
      { id: 'a1', label: 'Main · TRON', chain: 'tron', chainLabel: 'TRON', path: 'm/0', address: 'TN9xRpQ7vZJ...XmZ4d', isDefault: true },
      { id: 'a2', label: 'Marketing campaign', chain: 'eip155:1', chainLabel: 'Ethereum', path: 'm/0/1', address: '0x3bD9c81E...7f201A0B' },
    ],
    xpubReady: true,
  },
];

export const ENVELOPES: ReadonlyArray<SigningEnvelope> = [
  {
    requestId: 'req_9c3a82e1',
    groupId: 'grp_5a8e7b3c',
    chain: 'eip155:1',
    chainLabel: 'Ethereum',
    proposer: 'biz-svc-1',
    proposerLabel: 'Treasury Service',
    expiryISO: '2026-05-21T14:23:00Z',
    expiresIn: 412,
    receivedAt: '14:18:24',
    status: 'PENDING',
    digest32: '0x8a4f2c39e1b7d04f...c2a9',
    unsignedTxSummary: {
      to: '0xA4f3...B23E',
      toLabel: 'Binance hot wallet (allow-listed)',
      value: '0.5 ETH',
      valueFiat: '≈ $1,481.20',
      nonce: 142,
      gasLimit: 21000,
      gasPrice: '24 gwei',
      data: '0x',
    },
    businessInfo: {
      title: 'Treasury withdrawal · weekly settlement',
      orderId: 'WTH-2026-0521-018',
      operator: 'alice@company.io',
      memo: 'Approved 2026-05-19 · finance voucher W-518',
    },
    metaHashOk: true,
    crossCheckOk: true,
    decisions: [
      { id: 'm0', state: 'pending', self: true },
      { id: 'm1', state: 'pending', self: false },
      { id: 'm2', state: 'pending', self: false },
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
      toLabel: 'Unverified contract',
      value: '0 ETH',
      valueFiat: 'approve(spender, 0)',
      nonce: 89,
      gasLimit: 56200,
      gasPrice: '0.04 gwei',
      data: '0x095ea7b3...',
    },
    businessInfo: {
      title: 'Uniswap v4 router allowance revoke',
      orderId: 'OPS-2026-0521-009',
      operator: 'ops@company.io',
      memo: 'Revoke infinite allowance on legacy router',
    },
    metaHashOk: true,
    crossCheckOk: true,
    decisions: [
      { id: 'm0', state: 'pending', self: true },
      { id: 'm1', state: 'approved', self: false },
      { id: 'm2', state: 'pending', self: false },
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
      toLabel: 'Not on allow-list',
      value: '120,000 USDT',
      valueFiat: '≈ $120,000',
      nonce: 0,
      gasLimit: 0,
      gasPrice: 'feeLimit 80 TRX',
      data: 'TRC20.transfer',
    },
    businessInfo: {
      title: 'Customer payment · #C-2491',
      orderId: 'WTH-2026-0521-021',
      operator: 'cs@company.io',
      memo: 'Refund customer #C-2491 · 120k USDT',
    },
    metaHashOk: false,
    crossCheckOk: false,
    mismatchHint: 'businessInfo metaHash does not match proposer signature',
    decisions: [
      { id: 'm0', state: 'pending', self: true },
      { id: 'm1', state: 'pending', self: false },
      { id: 'm2', state: 'pending', self: false },
      { id: 'm3', state: 'pending', self: false },
    ],
  },
];

export const AUDIT: ReadonlyArray<AuditEvent> = [
  { t: '14:08', d: 'Today', requestId: 'req_4b2c91', group: 'Treasury main', chain: 'Ethereum', op: 'signed', value: '0.25 ETH', to: '0xA4f3...B23E', rsv: '0x8a4f...32 (65B)' },
  { t: '11:47', d: 'Today', requestId: 'req_88a01e', group: 'Operations backup', chain: 'Arbitrum', op: 'rejected', value: '1,200 USDC', to: '0x9c1e...44ad', reason: 'allow-list mismatch' },
  { t: '09:22', d: 'Yesterday', requestId: 'req_3c12d9', group: 'Treasury main', chain: 'Ethereum', op: 'signed', value: '0 ETH', to: 'Uniswap v4', rsv: '0x71f8...1b (65B)' },
  { t: '15:03', d: '2 days ago', requestId: 'req_99ef02', group: 'Treasury main', chain: 'TRON', op: 'signed', value: '8,500 USDT', to: 'TPx...4Q9', rsv: '0xc02d...4a (65B)' },
  { t: '10:51', d: '2 days ago', requestId: 'req_71b3aa', group: 'Operations backup', chain: 'Ethereum', op: 'expired', value: '0.04 ETH', to: '0x1c8E...4421', reason: 'no decision within 10 minutes' },
  { t: '09:00', d: '3 days ago', requestId: 'req_2e8f01', group: 'Treasury main', chain: 'Ethereum', op: 'signed', value: '2 ETH', to: '0xA4f3...B23E', rsv: '0x4d2a...90 (65B)' },
];

export const SETTINGS: ReadonlyArray<SettingsSection> = [
  {
    heading: 'identity',
    rows: [
      { key: 'memberId', value: MEMBER.memberId, mono: true },
      { key: 'device', value: MEMBER.device },
      {
        key: 'identityPub',
        value: `${MEMBER.identityPub.slice(0, 16)}…`,
        mono: true,
      },
    ],
  },
  {
    heading: 'connection',
    rows: [
      {
        key: 'coordEndpoint',
        value: COORD.endpoint.replace('https://', ''),
        mono: true,
      },
      { key: 'relayPeer', value: COORD.relayPeerID, mono: true },
      { key: 'dispatch', accent: true },
    ],
  },
  {
    heading: 'shares',
    rows: [
      { key: 'exportShare', action: { kind: 'backup' } },
      { key: 'importShare' },
      { key: 'changeKek' },
      { key: 'reshare', action: { kind: 'reshare' } },
      { key: 'keygen', action: { kind: 'keygen' } },
    ],
  },
  {
    heading: 'security',
    rows: [
      { key: 'faceId', accent: true },
      { key: 'zeroize' },
      { key: 'strictWysiwys', accent: true },
    ],
  },
  {
    heading: 'setup',
    rows: [
      { key: 'rerunOnboarding', action: { kind: 'onboarding' } },
      { key: 'attestLog' },
      { key: 'diagnostics' },
      { key: 'about', action: { kind: 'about' } },
    ],
  },
];

export const ENVELOPES_NEEDING_SELF: number = ENVELOPES.filter((env) => {
  const self = env.decisions.find((d) => d.self);
  return self ? self.state === 'pending' : false;
}).length;
