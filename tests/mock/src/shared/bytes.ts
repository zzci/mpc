import { Buffer } from 'node:buffer'

// Byte/encoding helpers. base64 uses Node's Buffer (always present under Bun)
// to match the api.md "字节字段 base64" wire convention exactly.

export function toB64(b: Uint8Array): string {
  return Buffer.from(b).toString('base64')
}

export function fromB64(s: string): Uint8Array {
  return new Uint8Array(Buffer.from(s, 'base64'))
}

export function fromHex(h: string): Uint8Array {
  const clean = h.startsWith('0x') ? h.slice(2) : h
  const out = new Uint8Array(clean.length / 2)
  for (let i = 0; i < out.length; i++)
    out[i] = Number.parseInt(clean.slice(i * 2, i * 2 + 2), 16)
  return out
}

export function toHex(b: Uint8Array): string {
  return Array.from(b, x => x.toString(16).padStart(2, '0')).join('')
}

export function bytesEqual(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length)
    return false
  for (let i = 0; i < a.length; i++) {
    if (a[i] !== b[i])
      return false
  }
  return true
}
