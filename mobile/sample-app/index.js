// RN entrypoint. Registers the root component under the app.json name.
//
// SKELETON: structurally complete RN registration. The native iOS/Android
// project shells that would actually host this bundle are intentionally not
// generated — this example is not required to run and targets no device
// (B-005 scope; docs/design/mcp/sdk.md §8 packaging is exercised by scripts/).

import { AppRegistry } from 'react-native';
import App from './src/App';
import { name as appName } from './app.json';

AppRegistry.registerComponent(appName, () => App);
