# rn-bridge

RN native-module skeleton bridging the mcp gomobile device-signing SDK
(`internal/mobileapi`, merged B-001) into JavaScript. Authoritative design:
`docs/design/mcp/sdk.md` §1 (rn-bridge) / §2 (flat surface) / §8 (packaging).

**Status (B-004):** structural skeleton only. Directory layout, native module
declarations, event wiring and TS interface signatures are complete and
aligned with the mobileapi flat API. Native bodies are stubs; the gomobile
`.aar` / `.xcframework` are not built, RN is not run, no device.

## Layout

```
rn/bridge/
├── package.json / tsconfig.json   # RN module manifest + codegenConfig
├── src/
│   ├── types.ts                   # flat types 1:1 with mobileapi
│   ├── NativeMcpWallet.ts         # TurboModule spec
│   └── index.ts                   # public API + event→callback projection
├── android/                       # Kotlin module + ReactPackage
│   └── src/main/java/cc/mcp/wallet/bridge/
└── ios/                           # Swift RCTEventEmitter + ObjC export
    + McpWallet.podspec
```

## API alignment

| mobileapi (Go)                         | rn-bridge (TS)                          |
|----------------------------------------|-----------------------------------------|
| `NewSDK(keystoreDir)`                  | `newSDK(keystoreDir)`                    |
| `SDK.KeyGen(configJSON, cb)`           | `keyGen(cfg, KeyGenCallback)`           |
| `SDK.Sign(startJSON, cb) → *SignSession` | `sign(startJSON, cb) → SignSession`   |
| `SignSession.Approve/Reject`           | `SignSession.approve/reject` (host→Go)  |
| `SDK.Reshare(configJSON, cb)`          | `reshare(cfg, ReshareCallback)`         |
| `SDK.OnWireMessage([]byte)`            | `onWireMessage(base64)`                 |
| `SDK.ExportShare(moniker, pass) []byte`| `exportShare(moniker, pass) → base64`   |
| `SDK.ImportShare([]byte, pass) string` | `importShare(base64, pass) → moniker`   |

`[]byte` crosses the JS bridge as base64; Go→host callbacks
(`KeyGenCallback`/`SignCallback`/`ReshareCallback`,
`OnProgress/OnDecoded/OnResult/OnError`) are delivered as native events and
re-projected onto the typed callback objects. Approve/Reject stay a
host→Go reverse call on `SignSession`, never a callback method (DREV-001 D4-1).
