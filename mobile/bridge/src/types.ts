// Flat types mirroring the gomobile-exported mobileapi surface
// (docs/design/mcp/sdk.md §2). Every value that crosses the JS bridge is a
// string / base64 string / JSON string — no tss-lib type ever leaks here,
// exactly as the Go side guarantees (internal/mobileapi/doc.go).

/** KeyGen configJSON schema — mobileapi keygenConfig. */
export interface KeygenConfig {
  threshold: number;
  parties: number;
  passphrase: string;
}

/** Reshare configJSON schema — mobileapi reshareConfig. */
export interface ReshareConfig {
  oldThreshold: number;
  newThreshold: number;
  newParties: number;
  passphrase: string;
}

/**
 * OnResult summary payload shared by KeyGen and Reshare — mobileapi
 * keygenSummary. groupPubKey is invariant across resharing
 * (docs/design/mcp/sdk.md §7).
 */
export interface GroupSummary {
  threshold: number;
  parties: number;
  monikers: string[];
  groupPubKey: string;
}

/**
 * Stable error codes — 1:1 with internal/mobileapi/errors.go. Security-class
 * codes are hard rejections: no retry, no downgrade (docs/design/mcp/sdk.md §5).
 */
export type SdkErrorCode =
  | 'BAD_CONFIG'
  | 'UNSUPPORTED_VERSION'
  | 'INVALID_ENVELOPE'
  | 'BAD_PROPOSER_SIG'
  | 'DIGEST_MISMATCH'
  | 'DECODE_REJECTED'
  | 'UNSUPPORTED_CHAIN'
  | 'EXPIRED'
  | 'REJECTED'
  | 'NO_SHARES'
  | 'SESSION_MISMATCH'
  | 'INTERNAL';

/** {code,msg} pair carried by every OnError — mobileapi sdkError. */
export interface SdkError {
  code: SdkErrorCode;
  msg: string;
}

/**
 * KeyGenCallback (Go→host). Exactly one of onResult / onError fires, always
 * after any onProgress (mobileapi callback-ordering contract).
 */
export interface KeyGenCallback {
  onProgress(stage: string): void;
  onResult(summary: GroupSummary): void;
  onError(err: SdkError): void;
}

/** ReshareCallback (Go→host) — same shape as KeyGenCallback. */
export interface ReshareCallback {
  onProgress(stage: string): void;
  onResult(summary: GroupSummary): void;
  onError(err: SdkError): void;
}

/**
 * SignCallback (Go→host). Per DREV-001 D4-1 it carries ONLY notifications;
 * the human Approve/Reject decision is a host→Go call on the SignSession,
 * never a callback method. Ordering: onDecoded at most once and only after
 * every security check passed; onResult/onError exactly once and last. On any
 * security-class failure onError fires and onDecoded never does — hard reject
 * without entering MPC (docs/design/mcp/sdk.md §3/§5).
 */
export interface SignCallback {
  /** Digest-bound A-zone facts, optional B-zone businessInfo, and A/B
   *  declarative mismatches, each parsed from its mobileapi JSON payload. */
  onDecoded(aFacts: unknown, bInfo: unknown, mismatch: unknown): void;
  /** Final 65-byte [V+27‖R‖S] signature, base64 across the bridge. */
  onResult(rsvBase64: string): void;
  onError(err: SdkError): void;
}

/**
 * SignSession is the opaque host→Go reverse handle returned by sign()
 * (DREV-001 D4-1). The host calls approve() or reject() exactly once after
 * onDecoded; extra/early/late calls are safe no-ops, matching
 * mobileapi.SignSession.
 */
export interface SignSession {
  readonly sessionId: string;
  approve(): Promise<void>;
  reject(): Promise<void>;
}
