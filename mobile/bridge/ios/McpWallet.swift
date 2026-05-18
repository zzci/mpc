import Foundation

/// iOS side of the mcp RN bridge. It owns the single gomobile-bound
/// `MobileapiSDK` handle (from the `.xcframework` produced by `gomobile bind`,
/// docs/design/mcp/sdk.md §8) and projects the flat mobileapi surface
/// (docs/design/mcp/sdk.md §2) into JS: request methods are promise-resolving
/// RCT_EXTERN methods; the Go→host callback protocols (KeyGenCallback /
/// SignCallback / ReshareCallback) are adapted onto RCTEventEmitter events
/// because a gomobile callback fires off the UI thread after the call returns
/// (docs/design/mcp/sdk.md §5).
///
/// SKELETON (B-004): method/signature/event shape is complete and aligned
/// with B-001 mobileapi; bodies are stubs. The `MobileapiNewSDK(...)` /
/// `sdk.keyGen(...)` wiring lands when the xcframework is linked. Not built
/// or run in this task; no device.
@objc(McpWallet)
class McpWallet: RCTEventEmitter {

  // private var sdk: MobileapiSDK?  // bound once the xcframework is linked

  override static func requiresMainQueueSetup() -> Bool { false }

  /// Events bridged from the gomobile callback protocols.
  override func supportedEvents() -> [String] {
    [
      "keygen:progress", "keygen:result", "keygen:error",
      "sign:decoded", "sign:result", "sign:error",
      "reshare:progress", "reshare:result", "reshare:error",
    ]
  }

  private func unimplemented(_ reject: RCTPromiseRejectBlock, _ what: String) {
    reject("INTERNAL", "rn-bridge skeleton: \(what) not wired", nil)
  }

  /// NewSDK(keystoreDir) → MobileapiNewSDK(keystoreDir).
  @objc(newSDK:resolver:rejecter:)
  func newSDK(_ keystoreDir: String,
              resolver resolve: RCTPromiseResolveBlock,
              rejecter reject: RCTPromiseRejectBlock) {
    unimplemented(reject, "newSDK")
  }

  /// SDK.KeyGen(configJSON, KeyGenCallback) → keygen:{progress,result,error}.
  @objc(keyGen:resolver:rejecter:)
  func keyGen(_ configJSON: String,
              resolver resolve: RCTPromiseResolveBlock,
              rejecter reject: RCTPromiseRejectBlock) {
    unimplemented(reject, "keyGen")
  }

  /// SDK.Sign(startJSON, SignCallback) → resolves sessionId;
  /// emits sign:{decoded,result,error} keyed by sessionId.
  @objc(sign:resolver:rejecter:)
  func sign(_ startJSON: String,
            resolver resolve: RCTPromiseResolveBlock,
            rejecter reject: RCTPromiseRejectBlock) {
    unimplemented(reject, "sign")
  }

  /// host→Go SignSession.Approve().
  @objc(approve:resolver:rejecter:)
  func approve(_ sessionId: String,
               resolver resolve: RCTPromiseResolveBlock,
               rejecter reject: RCTPromiseRejectBlock) {
    unimplemented(reject, "approve")
  }

  /// host→Go SignSession.Reject().
  @objc(reject:resolver:rejecter:)
  func reject(_ sessionId: String,
              resolver resolve: RCTPromiseResolveBlock,
              rejecter reject: RCTPromiseRejectBlock) {
    unimplemented(reject, "reject")
  }

  /// SDK.Reshare(configJSON, ReshareCallback) → reshare:{progress,result,error}.
  @objc(reshare:resolver:rejecter:)
  func reshare(_ configJSON: String,
               resolver resolve: RCTPromiseResolveBlock,
               rejecter reject: RCTPromiseRejectBlock) {
    unimplemented(reject, "reshare")
  }

  /// SDK.OnWireMessage([]byte) — base64-decoded before the call.
  @objc(onWireMessage:resolver:rejecter:)
  func onWireMessage(_ wireBase64: String,
                     resolver resolve: RCTPromiseResolveBlock,
                     rejecter reject: RCTPromiseRejectBlock) {
    unimplemented(reject, "onWireMessage")
  }

  /// SDK.ExportShare(moniker, passphrase) → base64 blob.
  @objc(exportShare:passphrase:resolver:rejecter:)
  func exportShare(_ moniker: String,
                   passphrase: String,
                   resolver resolve: RCTPromiseResolveBlock,
                   rejecter reject: RCTPromiseRejectBlock) {
    unimplemented(reject, "exportShare")
  }

  /// SDK.ImportShare(blob, passphrase) → moniker.
  @objc(importShare:passphrase:resolver:rejecter:)
  func importShare(_ blobBase64: String,
                   passphrase: String,
                   resolver resolve: RCTPromiseResolveBlock,
                   rejecter reject: RCTPromiseRejectBlock) {
    unimplemented(reject, "importShare")
  }
}
