// Public RN API for the mcp device-signing SDK. It is a thin, faithful
// re-projection of the gomobile mobileapi flat surface (docs/design/mcp/sdk.md
// §1 rn-bridge / §2): native promise calls for the request direction, a
// NativeEventEmitter for the Go→host callback interfaces, and a SignSession
// object for the host→Go Approve/Reject reverse path (DREV-001 D4-1).
//
// SKELETON: structure, signatures and event wiring are complete and aligned
// with B-001 mobileapi; the native modules under android/ and ios/ are stubs
// (B-004 scope — not required to run, no device).

import { NativeEventEmitter } from 'react-native';
import NativeMcpWallet from './NativeMcpWallet';
import type {
  GroupSummary,
  KeyGenCallback,
  KeygenConfig,
  ReshareCallback,
  ReshareConfig,
  SdkError,
  SignCallback,
  SignSession,
} from './types';

export * from './types';

const emitter = new NativeEventEmitter(NativeMcpWallet as never);

/** Opens (creating if absent) the device keystore-rooted SDK handle. */
export function newSDK(keystoreDir: string): Promise<void> {
  return NativeMcpWallet.newSDK(keystoreDir);
}

/** t-of-n ECDSA keygen. Resolves once the run is launched; the committee
 *  summary and progress arrive on `cb` (mobileapi KeyGenCallback). */
export function keyGen(cfg: KeygenConfig, cb: KeyGenCallback): Promise<void> {
  const subs = [
    emitter.addListener('keygen:progress', (s: string) => cb.onProgress(s)),
    emitter.addListener('keygen:result', (j: GroupSummary) => {
      dispose(subs);
      cb.onResult(j);
    }),
    emitter.addListener('keygen:error', (e: SdkError) => {
      dispose(subs);
      cb.onError(e);
    }),
  ];
  return NativeMcpWallet.keyGen(JSON.stringify(cfg)).catch((err) => {
    dispose(subs);
    throw err;
  });
}

/** Starts a signing session. Resolves to a SignSession whose approve()/
 *  reject() drive the human WYSIWYS decision back into Go. */
export async function sign(
  startJSON: string,
  cb: SignCallback,
): Promise<SignSession> {
  const sessionId = await NativeMcpWallet.sign(startJSON);
  const keyed =
    (handler: (payload: unknown) => void) =>
    (p: { sessionId: string } & Record<string, unknown>) => {
      if (p.sessionId === sessionId) handler(p);
    };
  const subs = [
    emitter.addListener(
      'sign:decoded',
      keyed((p: any) => cb.onDecoded(p.aFacts, p.bInfo, p.mismatch)),
    ),
    emitter.addListener(
      'sign:result',
      keyed((p: any) => {
        dispose(subs);
        cb.onResult(p.rsvBase64);
      }),
    ),
    emitter.addListener(
      'sign:error',
      keyed((p: any) => {
        dispose(subs);
        cb.onError(p as SdkError);
      }),
    ),
  ];
  return {
    sessionId,
    approve: () => NativeMcpWallet.approve(sessionId),
    reject: () => NativeMcpWallet.reject(sessionId),
  };
}

/** Resharing onto a new committee; master public key stays fixed. */
export function reshare(
  cfg: ReshareConfig,
  cb: ReshareCallback,
): Promise<void> {
  const subs = [
    emitter.addListener('reshare:progress', (s: string) => cb.onProgress(s)),
    emitter.addListener('reshare:result', (j: GroupSummary) => {
      dispose(subs);
      cb.onResult(j);
    }),
    emitter.addListener('reshare:error', (e: SdkError) => {
      dispose(subs);
      cb.onError(e);
    }),
  ];
  return NativeMcpWallet.reshare(JSON.stringify(cfg)).catch((err) => {
    dispose(subs);
    throw err;
  });
}

/** Feeds a transport-received MPC wire message (base64 JSON) into Go. */
export function onWireMessage(wireBase64: string): Promise<void> {
  return NativeMcpWallet.onWireMessage(wireBase64);
}

/** Passphrase-encrypted backup of one held share (base64 blob). */
export function exportShare(
  moniker: string,
  passphrase: string,
): Promise<string> {
  return NativeMcpWallet.exportShare(moniker, passphrase);
}

/** Restores a share from an exportShare backup; returns its moniker. */
export function importShare(
  blobBase64: string,
  passphrase: string,
): Promise<string> {
  return NativeMcpWallet.importShare(blobBase64, passphrase);
}

function dispose(subs: { remove(): void }[]): void {
  for (const s of subs) s.remove();
}
