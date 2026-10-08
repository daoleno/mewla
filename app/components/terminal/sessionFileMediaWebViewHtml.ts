import type { SessionFileMediaKind } from "../../services/sessionFileMedia";

/**
 * The page the native player runs in: a bare media element with no browser
 * controls. Mewla draws the controls in React Native and drives the element
 * through `window.__mewlaMedia`; the element reports back with `status` and
 * `error` messages (see parseSessionFileMediaEvent).
 */
export function buildSessionFileMediaHtml(input: {
  kind: SessionFileMediaKind;
  uri: string;
  background: string;
}): string {
  const tag = input.kind === "video" ? "video" : "audio";
  const config = JSON.stringify({ uri: sessionFileMediaFirstFrameUri(input.uri) })
    .replace(/</g, "\\u003c");
  return `<!doctype html>
<html><head>
<meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=1,user-scalable=no">
<style>
html,body{margin:0;padding:0;width:100%;height:100%;overflow:hidden;background:${cssColor(input.background)};}
${tag}{display:block;width:100%;height:100%;object-fit:contain;background:transparent;}
</style>
</head><body>
<${tag} id="media" playsinline webkit-playsinline preload="metadata"></${tag}>
<script>
(function () {
  var config = ${config};
  var media = document.getElementById("media");
  var last = 0;
  function post(payload) {
    if (window.ReactNativeWebView) window.ReactNativeWebView.postMessage(JSON.stringify(payload));
  }
  function status(force) {
    var now = Date.now();
    if (!force && now - last < 200) return;
    last = now;
    var duration = isFinite(media.duration) ? media.duration : 0;
    post({
      type: "status",
      currentTime: media.currentTime || 0,
      duration: duration,
      paused: media.paused,
      muted: media.muted,
      ended: media.ended,
      waiting: media.readyState < 1 || media.seeking || (!media.paused && media.readyState < 3),
      loaded: media.readyState >= 1
    });
  }
  ["loadedmetadata", "loadeddata", "durationchange", "play", "pause", "playing", "waiting",
   "seeking", "seeked", "ended", "volumechange", "canplay", "emptied"].forEach(function (name) {
    media.addEventListener(name, function () { status(true); });
  });
  media.addEventListener("timeupdate", function () { status(false); });
  media.addEventListener("error", function () {
    post({ type: "error", code: media.error ? media.error.code : 0 });
  });
  window.__mewlaMedia = {
    play: function () { var p = media.play(); if (p && p.catch) p.catch(function () { status(true); }); },
    pause: function () { media.pause(); },
    seek: function (time) { if (isFinite(time)) media.currentTime = Math.max(0, time); },
    setMuted: function (muted) { media.muted = !!muted; },
    load: function (uri, time, play) {
      media.src = uri;
      media.addEventListener("loadedmetadata", function resume() {
        media.removeEventListener("loadedmetadata", resume);
        if (time > 0) media.currentTime = time;
        if (play) window.__mewlaMedia.play();
      });
      media.load();
    },
    fullscreen: function () {
      if (media.webkitEnterFullscreen) media.webkitEnterFullscreen();
      else if (media.requestFullscreen) media.requestFullscreen();
    },
    stop: function () { media.pause(); media.removeAttribute("src"); media.load(); }
  };
  media.src = config.uri;
  status(true);
})();
</script>
</body></html>`;
}

/**
 * A tiny media fragment makes WebKit decode and paint the first frame as a
 * poster while only metadata is preloaded. It never reaches the daemon.
 */
export function sessionFileMediaFirstFrameUri(uri: string): string {
  return uri.includes("#") ? uri : `${uri}#t=0.001`;
}

/** The page origin for the WebView, so the stream loads same-origin. */
export function sessionFileMediaOrigin(uri: string): string | undefined {
  try {
    const url = new URL(uri);
    return `${url.protocol}//${url.host}/`;
  } catch {
    return undefined;
  }
}

function cssColor(value: string): string {
  return /^[#(),.%\w\s-]+$/.test(value) ? value : "transparent";
}
