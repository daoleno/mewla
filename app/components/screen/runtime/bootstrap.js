// WebView entry for native: mounts the viewer on the whole document and talks
// to React Native through window.screenHost / ReactNativeWebView.
import { mountScreenViewer } from "./viewer.js";

const viewer = mountScreenViewer(document.body, (message) => {
  window.ReactNativeWebView.postMessage(JSON.stringify(message));
});
window.screenHost = (batch) => viewer.handle(batch);
window.ReactNativeWebView.postMessage(JSON.stringify({ type: "ready" }));
