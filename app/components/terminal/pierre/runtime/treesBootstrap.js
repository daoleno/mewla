// WebView entry for native: mounts the tree on the whole document and talks to
// React Native through window.pierreTreeHost / ReactNativeWebView.
import { mountTreeViewer } from "./trees.js";

const viewer = mountTreeViewer(document.body, (message) => {
  window.ReactNativeWebView.postMessage(JSON.stringify(message));
});
window.pierreTreeHost = (batch) => viewer.handle(batch);
window.ReactNativeWebView.postMessage(JSON.stringify({ type: "ready" }));
