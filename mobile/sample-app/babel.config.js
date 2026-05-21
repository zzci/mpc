// Babel config for the RN 0.74 sample app.
//
// CI-005: the Metro bundler invoked by `react-native bundle` runs the
// @react-native/metro-babel-transformer, which loads babel.config.js via
// upward lookup from each source file. Without this file the transformer
// falls back to defaults that do not include `module:@react-native/babel-preset`,
// so RN-flavored JSX/TS in src/ would not transform correctly even after the
// metro config error is fixed. This file matches the RN 0.74 template.
module.exports = {
  presets: ['module:@react-native/babel-preset'],
};
