// Metro bundler config for the RN 0.74 sample app.
//
// CI-005: the Xcode "Bundle React Native code and images" build phase invokes
// `node node_modules/react-native/cli.js bundle` from this directory and
// errors with "No Metro config found" when the file is absent. The RN 0.74
// template ships this exact file at the project root; we keep it tracked here
// because the workflow only copies the scaffold's ios/ and android/ folders,
// not the JS project root.
//
// CI-006: src/sdk.ts imports `@mcp/rn-bridge`, declared as `file:../bridge`.
// npm (install-links=false, the default) materializes that as a symlink at
// node_modules/@mcp/rn-bridge -> ../../bridge, whose real source lives outside
// this project root. Metro 0.80 (shipped with RN 0.74.5) does not follow such
// symlinks during resolution and only serves files inside watchFolders, so
// bundling fails with "Unable to resolve module @mcp/rn-bridge". We:
//   1) add ../bridge to watchFolders so Metro reads bridge/src/index.ts;
//   2) alias @mcp/rn-bridge to ../bridge via extraNodeModules so resolution
//      succeeds regardless of how npm materializes the file: dep (symlink or
//      copy);
//   3) pin nodeModulesPaths to this app's node_modules so the bridge's peer
//      deps (react, react-native) resolve through the app's installs rather
//      than a non-existent ../bridge/node_modules.
const path = require('path');
const {
  getDefaultConfig,
  mergeConfig,
} = require('@react-native/metro-config');

const projectRoot = __dirname;
const bridgeRoot = path.resolve(projectRoot, '..', 'bridge');

/** @type {import('metro-config').MetroConfig} */
const config = {
  watchFolders: [bridgeRoot],
  resolver: {
    extraNodeModules: {
      '@mcp/rn-bridge': bridgeRoot,
    },
    nodeModulesPaths: [path.resolve(projectRoot, 'node_modules')],
  },
};

module.exports = mergeConfig(getDefaultConfig(projectRoot), config);
