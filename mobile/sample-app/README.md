# sample-app

Minimal React Native integration example for the mcp device-signing SDK.
Authoritative design: `docs/design/mcp/sdk.md` §1 (rn-bridge) / §2 (flat surface)
/ §3 (WYSIWYS signing) / §7 (resharing). Depends on `@mcp/rn-bridge`
(B-004), the faithful re-projection of the gomobile mobileapi flat API.

**Status (B-005):** structural skeleton only. Directory layout, RN project
files and the keygen / sign / reshare + multi-party approval call sites are
complete and aligned with the bridge public API. Native bodies and the
gomobile `.aar` / `.xcframework` are stubs from B-004; **this example is not
required to run and targets no device.** The native iOS/Android host project
shells are intentionally not generated (out of skeleton scope).

## Layout

```
rn/sample-app/
├── package.json / tsconfig.json   # RN app manifest (depends on @mcp/rn-bridge)
├── app.json / index.js            # RN registration entrypoint
└── src/
    ├── App.tsx                    # newSDK init + keygen/sign/reshare switch
    ├── sdk.ts                     # single pass-through surface to @mcp/rn-bridge
    ├── screens/
    │   ├── KeygenScreen.tsx       # keyGen(cfg, KeyGenCallback)
    │   ├── SignScreen.tsx         # sign(startJSON, SignCallback) → SignSession
    │   └── ReshareScreen.tsx      # reshare(cfg, ReshareCallback)
    └── components/
        └── ApprovalSheet.tsx      # WYSIWYS A/B/mismatch zones + Approve/Reject
```

## Bridge alignment

| @mcp/rn-bridge (B-004)                  | sample-app call site                     |
|-----------------------------------------|------------------------------------------|
| `newSDK(keystoreDir)`                   | `App.tsx` mount                          |
| `keyGen(cfg, KeyGenCallback)`           | `KeygenScreen`                           |
| `sign(startJSON, cb) → SignSession`     | `SignScreen`                             |
| `SignSession.approve/reject` (host→Go)  | `ApprovalSheet` actions                  |
| `reshare(cfg, ReshareCallback)`         | `ReshareScreen`                          |

Every value crossing the bridge stays string / base64 / JSON; no tss-lib
type leaks into the example.

## Multi-party approval

`sign()`'s `SignCallback` carries **only** notifications. `onDecoded`
delivers the digest-bound A-zone facts (sole funds-safety authority),
advisory B-zone businessInfo, and declarative A/B mismatches; the human
Approve/Reject is a host→Go reverse call on `SignSession` (DREV-001 D4-1),
never a callback. `ApprovalSheet` shows the t-of-n committee roster — each
member reviews and approves on its **own** device and coord gates START on a
quorum; other parties' decisions are not simulated here.
