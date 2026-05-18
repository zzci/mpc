package cc.mcp.wallet.bridge

import com.facebook.react.ReactPackage
import com.facebook.react.bridge.NativeModule
import com.facebook.react.bridge.ReactApplicationContext
import com.facebook.react.uimanager.ViewManager

/**
 * ReactPackage registering [McpWalletModule]. Referenced from the host app's
 * `getPackages()`; auto-linking discovers it via package.json codegenConfig
 * (docs/design/mcp/sdk.md §8 — rn-bridge auto-links the gomobile lib).
 */
class McpWalletPackage : ReactPackage {
    override fun createNativeModules(
        reactContext: ReactApplicationContext,
    ): List<NativeModule> = listOf(McpWalletModule(reactContext))

    override fun createViewManagers(
        reactContext: ReactApplicationContext,
    ): List<ViewManager<*, *>> = emptyList()
}
