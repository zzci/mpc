// Typed demo data shape for the Trine Signer sample app. Adapted from the
// read-only design handoff at design/tss-mpc/project/data.jsx. These types
// describe the minimum view-model the foundation screens need; later L3s
// extend them as feature flows are filled in.

export interface Member {
  readonly memberId: string;
  readonly identityPub: string;
  readonly partyIndex: number;
  readonly device: string;
}

export interface CoordStatus {
  readonly endpoint: string;
  readonly tls: string;
  readonly status: 'connected' | 'locked' | 'offline';
  readonly latencyMs: number;
  readonly lastHeartbeat: string;
  readonly relayPeerID: string;
}

export type MemberPresence = 'online' | 'offline' | 'standby';

export interface GroupMember {
  readonly id: string;
  readonly label: string;
  readonly self: boolean;
  readonly status: MemberPresence;
  readonly last: string;
}

export interface WalletAddress {
  readonly id: string;
  readonly label: string;
  readonly chain: string;
  readonly chainLabel: string;
  readonly path: string;
  readonly address: string;
  readonly isDefault?: boolean;
}

export interface Wallet {
  readonly groupId: string;
  readonly moniker: string;
  readonly threshold: number;
  readonly parties: number;
  readonly members: ReadonlyArray<GroupMember>;
  readonly ecdsaPubkey: string;
  readonly chaincode: string;
  readonly addresses: ReadonlyArray<WalletAddress>;
  readonly xpubReady: boolean;
}

export type DecisionState = 'pending' | 'approved' | 'rejected';

export interface MemberDecision {
  readonly id: string;
  readonly state: DecisionState;
  readonly self: boolean;
}

export interface UnsignedTxSummary {
  readonly to: string;
  readonly toLabel: string;
  readonly value: string;
  readonly valueFiat: string;
  readonly nonce: number;
  readonly gasLimit: number;
  readonly gasPrice: string;
  readonly data: string;
}

export interface BusinessInfo {
  readonly title: string;
  readonly orderId: string;
  readonly operator: string;
  readonly memo: string;
}

export type EnvelopeStatus = 'PENDING' | 'APPROVED' | 'REJECTED' | 'SIGNED' | 'EXPIRED';

export interface SigningEnvelope {
  readonly requestId: string;
  readonly groupId: string;
  readonly chain: string;
  readonly chainLabel: string;
  readonly proposer: string;
  readonly proposerLabel: string;
  readonly expiryISO: string;
  readonly expiresIn: number;
  readonly receivedAt: string;
  readonly status: EnvelopeStatus;
  readonly digest32: string;
  readonly unsignedTxSummary: UnsignedTxSummary;
  readonly businessInfo: BusinessInfo;
  readonly metaHashOk: boolean;
  readonly crossCheckOk: boolean;
  readonly mismatchHint?: string;
  readonly decisions: ReadonlyArray<MemberDecision>;
}

export type AuditOp = 'signed' | 'rejected' | 'expired';

export interface AuditEvent {
  readonly t: string;
  readonly d: string;
  readonly requestId: string;
  readonly group: string;
  readonly chain: string;
  readonly op: AuditOp;
  readonly value: string;
  readonly to: string;
  readonly rsv?: string;
  readonly reason?: string;
}

export type SettingsRowKey =
  | 'memberId'
  | 'device'
  | 'identityPub'
  | 'coordEndpoint'
  | 'relayPeer'
  | 'dispatch'
  | 'exportShare'
  | 'importShare'
  | 'changeKek'
  | 'reshare'
  | 'keygen'
  | 'faceId'
  | 'zeroize'
  | 'strictWysiwys'
  | 'rerunOnboarding'
  | 'attestLog'
  | 'diagnostics'
  | 'about';

export type SettingsSectionKey =
  | 'identity'
  | 'connection'
  | 'shares'
  | 'security'
  | 'setup';

export interface SettingsRow {
  readonly key: SettingsRowKey;
  readonly value?: string;
  readonly mono?: boolean;
  readonly accent?: boolean;
  readonly action?: SettingsAction;
}

export type SettingsAction =
  | { readonly kind: 'onboarding' }
  | { readonly kind: 'backup' }
  | { readonly kind: 'reshare' }
  | { readonly kind: 'keygen' }
  | { readonly kind: 'about' };

export interface SettingsSection {
  readonly heading: SettingsSectionKey;
  readonly rows: ReadonlyArray<SettingsRow>;
}
