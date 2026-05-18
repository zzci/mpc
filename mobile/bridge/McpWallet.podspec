require "json"

package = JSON.parse(File.read(File.join(__dir__, "package.json")))

# CocoaPods spec for the mcp RN bridge. Vendors the gomobile-produced
# McpWallet.xcframework (docs/design/mcp/sdk.md §8). SKELETON (B-004): declares the
# pod shape; the xcframework is dropped under ios/ by the packaging step.
Pod::Spec.new do |s|
  s.name         = "McpWallet"
  s.version      = package["version"]
  s.summary      = package["description"]
  s.homepage     = "https://github.com/zzci/mpc"
  s.license      = "MIT"
  s.authors      = "mcp"
  s.platforms    = { :ios => "13.0" }
  s.source       = { :path => "." }

  s.source_files = "ios/**/*.{swift,m,h}"
  s.vendored_frameworks = "ios/McpWallet.xcframework"

  s.dependency "React-Core"
end
