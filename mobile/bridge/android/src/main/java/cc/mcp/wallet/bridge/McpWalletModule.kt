package cc.mcp.wallet.bridge

import com.facebook.react.bridge.Promise
import com.facebook.react.bridge.ReactApplicationContext
import com.facebook.react.bridge.ReactContextBaseJavaModule
import com.facebook.react.bridge.ReactMethod
import com.facebook.react.modules.core.DeviceEventManagerModule

/**
 * Android side of the mcp RN bridge. It owns the single gomobile-bound
 * `mobileapi.SDK` handle and translates each flat call (docs/design/mcp/sdk.md §2)
 * into JS: request methods are @ReactMethod + Promise; the Go→host callback
 * interfaces (KeyGenCallback / SignCallback / ReshareCallback) are adapted to
 * `RCTDeviceEventEmitter` events because a gomobile callback fires off the UI
 * thread well after the call returns (docs/design/mcp/sdk.md §5).
 *
 * SKELETON (B-004): method/signature/event shape is complete and aligned with
 * B-001 mobileapi; bodies are stubs. The actual `mobileapi.Mobileapi.newSDK`
 * / `SDK.keyGen(...)` wiring lands when the .aar is bound. Not built or run
 * in this task; no device.
 */
class McpWalletModule(
    private val reactContext: ReactApplicationContext,
) : ReactContextBaseJavaModule(reactContext) {

    // private var sdk: mobileapi.SDK? = null  // bound once .aar is linked

    override fun getName(): String = NAME

    private fun emit(event: String, payload: Any?) {
        reactContext
            .getJSModule(DeviceEventManagerModule.RCTDeviceEventEmitter::class.java)
            .emit(event, payload)
    }

    /** NewSDK(keystoreDir) → mobileapi.Mobileapi.newSDK(keystoreDir). */
    @ReactMethod
    fun newSDK(keystoreDir: String, promise: Promise) {
        promise.reject(E_UNIMPLEMENTED, "rn-bridge skeleton: newSDK not wired")
    }

    /** SDK.KeyGen(configJSON, KeyGenCallback) → keygen:{progress,result,error}. */
    @ReactMethod
    fun keyGen(configJSON: String, promise: Promise) {
        promise.reject(E_UNIMPLEMENTED, "rn-bridge skeleton: keyGen not wired")
    }

    /** SDK.Sign(startJSON, SignCallback) → resolves sessionId;
     *  emits sign:{decoded,result,error} keyed by sessionId. */
    @ReactMethod
    fun sign(startJSON: String, promise: Promise) {
        promise.reject(E_UNIMPLEMENTED, "rn-bridge skeleton: sign not wired")
    }

    /** host→Go SignSession.Approve(). */
    @ReactMethod
    fun approve(sessionId: String, promise: Promise) {
        promise.reject(E_UNIMPLEMENTED, "rn-bridge skeleton: approve not wired")
    }

    /** host→Go SignSession.Reject(). */
    @ReactMethod
    fun reject(sessionId: String, promise: Promise) {
        promise.reject(E_UNIMPLEMENTED, "rn-bridge skeleton: reject not wired")
    }

    /** SDK.Reshare(configJSON, ReshareCallback) → reshare:{progress,result,error}. */
    @ReactMethod
    fun reshare(configJSON: String, promise: Promise) {
        promise.reject(E_UNIMPLEMENTED, "rn-bridge skeleton: reshare not wired")
    }

    /** SDK.OnWireMessage([]byte) — base64-decoded before the call. */
    @ReactMethod
    fun onWireMessage(wireBase64: String, promise: Promise) {
        promise.reject(E_UNIMPLEMENTED, "rn-bridge skeleton: onWireMessage not wired")
    }

    /** SDK.ExportShare(moniker, passphrase) → base64 blob. */
    @ReactMethod
    fun exportShare(moniker: String, passphrase: String, promise: Promise) {
        promise.reject(E_UNIMPLEMENTED, "rn-bridge skeleton: exportShare not wired")
    }

    /** SDK.ImportShare(blob, passphrase) → moniker. */
    @ReactMethod
    fun importShare(blobBase64: String, passphrase: String, promise: Promise) {
        promise.reject(E_UNIMPLEMENTED, "rn-bridge skeleton: importShare not wired")
    }

    // NativeEventEmitter contract.
    @ReactMethod fun addListener(eventName: String) {}

    @ReactMethod fun removeListeners(count: Double) {}

    companion object {
        const val NAME = "McpWallet"
        private const val E_UNIMPLEMENTED = "INTERNAL"
    }
}
