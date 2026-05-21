// Metro bundler config for the RN 0.74 sample app.
//
// CI-005: the Xcode "Bundle React Native code and images" build phase invokes
// `node node_modules/react-native/cli.js bundle` from this directory and
// errors with "No Metro config found" when the file is absent. The RN 0.74
// template ships this exact file at the project root; we keep it tracked here
// because the workflow only copies the scaffold's ios/ and android/ folders,
// not the JS project root.
const {
  getDefaultConfig,
  mergeConfig,
} = require('@react-native/metro-config');

/** @type {import('metro-config').MetroConfig} */
const config = {};

module.exports = mergeConfig(getDefaultConfig(__dirname), config);
