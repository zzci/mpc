// Demo deterministic address derivation. Mirrors the future bridge contract
// (chaincode + group pubkey + chain + path → chain-specific address) but
// uses a small in-process FNV-1a hash so the sample app stays dependency
// free and runs without the gomobile binding. Replace with a real bridge
// call once the SDK exposes one.

import type { Wallet } from '../../data';

export interface DerivePreview {
  readonly address: string;
  readonly chain: string;
  readonly chainLabel: string;
  readonly path: string;
}

export function derivePreview(
  wallet: Wallet,
  chain: string,
  chainLabel: string,
  rawPath: string,
): DerivePreview {
  const path = normalizePath(rawPath);
  const seed = `${wallet.ecdsaPubkey}:${wallet.chaincode}:${chain}:${path}`;
  const address = chain.startsWith('eip155:')
    ? deriveEvmAddress(seed)
    : chain === 'tron'
      ? deriveTronAddress(seed)
      : deriveGenericAddress(seed);
  return { address, chain, chainLabel, path };
}

export function normalizePath(raw: string): string {
  const trimmed = raw.trim();
  if (trimmed === '') return 'm/0';
  if (trimmed.startsWith('m/')) return trimmed;
  if (trimmed.startsWith('/')) return `m${trimmed}`;
  return `m/${trimmed}`;
}

function deriveEvmAddress(seed: string): string {
  const hex = hashStream(seed, 20);
  return `0x${hex}`;
}

function deriveTronAddress(seed: string): string {
  // T + 33 base58-style chars; deterministic from the seed.
  const alphabet = '123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz';
  const tail = hashStream(seed, 33).split('');
  const out = tail.map((c) => alphabet[parseInt(c, 16) % alphabet.length]).join('');
  return `T${out}`;
}

function deriveGenericAddress(seed: string): string {
  return `0x${hashStream(seed, 32)}`;
}

function hashStream(seed: string, bytes: number): string {
  // FNV-1a 32-bit chained: rehash the buffer with an incrementing nonce
  // until enough hex characters have been produced. Stable across runs.
  const chars: Array<string> = [];
  let nonce = 0;
  while (chars.length < bytes * 2) {
    const h = fnv1a(`${seed}#${nonce}`);
    chars.push(h.toString(16).padStart(8, '0'));
    nonce += 1;
  }
  return chars.join('').slice(0, bytes * 2);
}

function fnv1a(input: string): number {
  let hash = 0x811c9dc5;
  for (let i = 0; i < input.length; i += 1) {
    hash ^= input.charCodeAt(i);
    // FNV prime 16777619 — keep within 32-bit using imul.
    hash = Math.imul(hash, 0x01000193);
  }
  return hash >>> 0;
}
