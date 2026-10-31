# Dependency patches

Applied by `bun install` (`patchedDependencies` in the root `package.json`). Each patch fixes
TypeScript declarations only, so the runtime code is untouched. Drop one when the package it
patches is upgraded past the fix.

- `react-native-webview@14.0.1`: `declare class WebView<P = undefined>` makes `WebViewProps & P`
  collapse to `never`, so no `<WebView>` prop type-checks. Fixed upstream in 16.0.0
  (`P = {}`); Expo SDK 58 pins 14.0.1.
- `@react-native-masked-view/masked-view@0.3.2`: the declaration extends
  `ReactNative.NativeMethods`, which React Native 0.88's strict types no longer export, so
  `<MaskedView>` is not a valid JSX element. The patch declares the class directly.
