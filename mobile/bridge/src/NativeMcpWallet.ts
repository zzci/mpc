// TurboModule spec for the native bridge. The native side (Kotlin/Swift)
// owns the gomobile-bound *SDK and forwards its callback interfaces
// (KeyGenCallback/SignCallback/ReshareCallback) to JS as emitted events,
// because a gomobile callback fires off the UI thread long after the
// originating call returns (docs/design/mcp/sdk.md §5) and cannot hold a JS
// function across that boundary.
//
// SKELETON: signatures only. The TurboModuleRegistry lookup and codegen
// wiring are intentionally not exercised here — this module is structurally
// complete but not required to run (B-004 scope).

import type { TurboModule } from 'react-native';
import { TurboModuleRegistry } from 'react-native';

export interface Spec extends TurboModule {
  /** NewSDK(keystoreDir) → opens/creates the device keystore-rooted handle. */
  newSDK(keystoreDir: string): Promise<void>;

  /** SDK.KeyGen(configJSON, KeyGenCallback). configJSON is KeygenConfig.
   *  Outcomes arrive as `keygen:progress|result|error` events. */
  keyGen(configJSON: string): Promise<void>;

  /** SDK.Sign(startJSON, SignCallback) → returns the sessionId of the
   *  *SignSession. Outcomes arrive as `sign:decoded|result|error` events
   *  keyed by sessionId. */
  sign(startJSON: string): Promise<string>;

  /** host→Go SignSession.Approve(). Idempotent / no-op if not applicable. */
  approve(sessionId: string): Promise<void>;

  /** host→Go SignSession.Reject(). Same idempotency as approve. */
  reject(sessionId: string): Promise<void>;

  /** SDK.Reshare(configJSON, ReshareCallback). configJSON is ReshareConfig.
   *  Outcomes arrive as `reshare:progress|result|error` events. */
  reshare(configJSON: string): Promise<void>;

  /** SDK.OnWireMessage([]byte) — base64 of the JSON wire MpcMessage.
   *  Rejects with the {code,msg} of the receive-side security gate drop. */
  onWireMessage(wireBase64: string): Promise<void>;

  /** SDK.ExportShare(moniker, passphrase) → base64 of the encrypted blob. */
  exportShare(moniker: string, passphrase: string): Promise<string>;

  /** SDK.ImportShare(blob, passphrase) → restored share moniker. */
  importShare(blobBase64: string, passphrase: string): Promise<string>;

  // Event-emitter plumbing (required by RN's NativeEventEmitter contract).
  addListener(eventName: string): void;
  removeListeners(count: number): void;
}

export default TurboModuleRegistry.getEnforcing<Spec>('McpWallet');
