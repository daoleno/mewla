// WebView entry for native: mounts the reader on the whole document and talks
// to React Native through window.pierreDiffHost / ReactNativeWebView.
import { mountDiffViewer } from "./diffs.js";

const viewer = mountDiffViewer(document.body, (message) => {
  window.ReactNativeWebView.postMessage(JSON.stringify(message));
});
window.pierreDiffHost = (batch) => viewer.handle(batch);
window.ReactNativeWebView.postMessage(JSON.stringify({ type: "ready" }));
