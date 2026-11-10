// WebView entry for NSH: mounts the viewer on the whole document. The app sends
// batches as `message` events (WebViewRef.postMessage); the viewer answers
// through window.nsh.postMessage.
import { mountScreenViewer } from "./viewer.js";

const viewer = mountScreenViewer(document.body, (message) => {
  window.nsh.postMessage(JSON.stringify(message));
});
window.addEventListener("message", (event) => {
  try {
    const batch = JSON.parse(event.data);
    if (Array.isArray(batch)) viewer.handle(batch);
  } catch {}
});
window.nsh.postMessage(JSON.stringify({ type: "ready" }));
