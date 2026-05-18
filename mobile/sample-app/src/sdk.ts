// Single import surface the example screens use to reach the device-signing
// SDK. It is a thin pass-through to @mcp/rn-bridge (B-004) — the faithful
// re-projection of the gomobile mobileapi flat API (docs/design/mcp/sdk.md §2);
// no tss-lib type ever crosses here.
//
// SKELETON: call sites and types are aligned with the bridge public API.
// The bridge native bodies and the gomobile .aar/.xcframework are stubs, so
// these calls are structurally correct but not exercised (B-005 scope: no
// run, no device).

export {
  newSDK,
  keyGen,
  sign,
  reshare,
  onWireMessage,
  exportShare,
  importShare,
} from '@mcp/rn-bridge';

export type {
  KeygenConfig,
  ReshareConfig,
  GroupSummary,
  SdkError,
  SdkErrorCode,
  KeyGenCallback,
  ReshareCallback,
  SignCallback,
  SignSession,
} from '@mcp/rn-bridge';

/** Device keystore root for this example (real apps derive a per-install,
 *  app-sandboxed path; docs/design/mcp/sdk.md §6 keystore). */
export const KEYSTORE_DIR = 'mcp-sample-keystore';
