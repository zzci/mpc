// Wraps sdk.ts exportShare / importShare so the foundation flow can drive the
// real bridge call when the native module is available, and fall back to a
// structurally-equivalent demo response when it is not (B-005: native bodies
// are stubs). The conceptual method shape — `(moniker, passphrase) => blob`
// for export and `(blob, passphrase) => moniker` for import — is preserved
// either way, so swapping the demo response for a live return value is a
// single-line change once the gomobile .aar/.xcframework ships.

import { exportShare, importShare } from '../../sdk';

export type BridgeMode = 'live' | 'demo';

export interface ExportOutcome {
  readonly mode: BridgeMode;
  readonly blobBase64: string;
}

export interface ImportOutcome {
  readonly mode: BridgeMode;
  readonly moniker: string;
}

export async function callExportShare(
  moniker: string,
  passphrase: string,
): Promise<ExportOutcome> {
  try {
    const blobBase64 = await exportShare(moniker, passphrase);
    if (blobBase64 && blobBase64.length > 0) {
      return { mode: 'live', blobBase64 };
    }
  } catch {
    // Native body is a stub in B-005. Fall through to the demo response,
    // preserving the bridge contract for callers.
  }
  return { mode: 'demo', blobBase64: demoBlob(moniker, passphrase) };
}

export async function callImportShare(
  blobBase64: string,
  passphrase: string,
): Promise<ImportOutcome> {
  if (!blobBase64.trim()) {
    throw new BridgeCallError('emptyBlob');
  }
  if (!passphrase) {
    throw new BridgeCallError('badPassphrase');
  }
  try {
    const moniker = await importShare(blobBase64.trim(), passphrase);
    if (moniker && moniker.length > 0) {
      return { mode: 'live', moniker };
    }
  } catch (err) {
    // Real bridge surface returns SdkError. For the demo we only differentiate
    // shape errors locally; the live path will surface SdkError verbatim once
    // the native body lands.
    if (looksLikePassphraseError(err)) {
      throw new BridgeCallError('badPassphrase');
    }
    // Fall through to the demo response.
  }
  const moniker = demoMoniker(blobBase64);
  if (!moniker) throw new BridgeCallError('malformedBlob');
  return { mode: 'demo', moniker };
}

export type BridgeCallErrorKind = 'badPassphrase' | 'emptyBlob' | 'malformedBlob';

export class BridgeCallError extends Error {
  public readonly kind: BridgeCallErrorKind;
  constructor(kind: BridgeCallErrorKind) {
    super(kind);
    this.kind = kind;
    this.name = 'BridgeCallError';
  }
}

function looksLikePassphraseError(err: unknown): boolean {
  if (!(err instanceof Error)) return false;
  const m = err.message.toLowerCase();
  return m.includes('passphrase') || m.includes('aead') || m.includes('unwrap');
}

// Stable demo blob: encodes the moniker and a short fingerprint of the
// passphrase length, so the result varies per input without leaking the
// passphrase itself. The native body is expected to return base64 — we keep
// the shape identical.
function demoBlob(moniker: string, passphrase: string): string {
  const header = `mcpbk:1:${moniker}:argon2id:${passphrase.length}`;
  const body = repeatToLen(`${moniker}-share-demo`, 96);
  return toBase64(`${header}|${body}`);
}

function demoMoniker(blobBase64: string): string {
  const decoded = tryFromBase64(blobBase64.trim());
  if (!decoded) return '';
  const i = decoded.indexOf('mcpbk:1:');
  if (i !== 0) return decoded.slice(0, 16) || 'recovered-share';
  const parts = decoded.split(':');
  return parts[2] ?? 'recovered-share';
}

function repeatToLen(seed: string, total: number): string {
  let out = '';
  while (out.length < total) out += seed;
  return out.slice(0, total);
}

function toBase64(value: string): string {
  if (typeof (globalThis as { btoa?: (s: string) => string }).btoa === 'function') {
    return (globalThis as { btoa: (s: string) => string }).btoa(value);
  }
  // RN runtime always ships btoa; fall back to a literal pass-through marker
  // to keep types honest if the polyfill is unavailable.
  return `b64:${value}`;
}

function tryFromBase64(value: string): string | null {
  try {
    const decoder = (globalThis as { atob?: (s: string) => string }).atob;
    if (typeof decoder === 'function') return decoder(value);
  } catch {
    return null;
  }
  if (value.startsWith('b64:')) return value.slice(4);
  return null;
}
