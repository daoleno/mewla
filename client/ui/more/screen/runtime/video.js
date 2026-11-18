// The picture as pushed video: the computer offers one H.264 track and two
// data channels over WebRTC, and this page answers. The signalling (offer,
// answer, candidates) goes through the host, on the authenticated Mewla link;
// the media and input go peer to peer (DTLS-SRTP). The <video> element plays
// with the platform's decoder: no per-frame bridge, no JavaScript decode.
//
// Input goes back as RFB client messages, each led by a sequence number:
// "input" (reliable, ordered) carries keys, button changes, the wheel and the
// clipboard; "pointer" (unordered, never resent) carries moves only, which
// the computer drops when they arrive after a later reliable message.

export function createVideoPicture(doc, handlers) {
  const video = doc.createElement("video");
  video.muted = true;
  video.autoplay = true;
  video.playsInline = true;
  video.setAttribute("playsinline", "");
  video.setAttribute("muted", "");
  video.disablePictureInPicture = true;
  video.tabIndex = 0;
  video.className = "picture";

  const pc = new RTCPeerConnection({ bundlePolicy: "max-bundle" });
  let input = null;
  let pointer = null;
  let seq = 0;
  let lastMask = 0;
  let width = 0;
  let height = 0;
  let closed = false;
  const pendingIce = [];

  pc.ondatachannel = (event) => {
    const channel = event.channel;
    channel.binaryType = "arraybuffer";
    if (channel.label === "input") input = channel;
    else if (channel.label === "pointer") pointer = channel;
  };
  pc.ontrack = (event) => {
    const receiver = event.receiver;
    // Show each picture as soon as it is decoded: a screen has no audio to keep in step with.
    try { receiver.jitterBufferTarget = 0; } catch {}
    try { receiver.playoutDelayHint = 0; } catch {}
    video.srcObject = event.streams[0] || new MediaStream([event.track]);
    play();
  };
  pc.onicecandidate = (event) => {
    handlers.signal({ type: "ice", candidate: event.candidate ? event.candidate.toJSON() : null });
  };
  pc.onconnectionstatechange = () => {
    if (!closed && pc.connectionState === "failed") handlers.failed();
  };

  function play() {
    const p = video.play();
    if (p && p.catch) p.catch(() => {});
  }

  function sized() {
    if (!video.videoWidth || !video.videoHeight) return;
    if (video.videoWidth === width && video.videoHeight === height) return;
    width = video.videoWidth;
    height = video.videoHeight;
    video.style.width = `${width}px`;
    video.style.height = `${height}px`;
    handlers.resized(width, height);
  }
  video.addEventListener("loadedmetadata", sized);
  video.addEventListener("resize", sized);

  // Every picture shown, for the frame rate and the input-to-picture timing.
  const onFrame = () => {
    if (closed) return;
    sized();
    handlers.frame();
    video.requestVideoFrameCallback(onFrame);
  };
  if (video.requestVideoFrameCallback) video.requestVideoFrameCallback(onFrame);
  else video.addEventListener("timeupdate", () => { sized(); handlers.frame(); });

  async function signal(message) {
    if (closed || !message) return;
    try {
      if (message.type === "rtc" && message.sdp_type === "offer") {
        // The relay's TURN (and STUN), for when there is no direct path.
        if (Array.isArray(message.ice_servers) && message.ice_servers.length) {
          try { pc.setConfiguration({ ...pc.getConfiguration(), iceServers: message.ice_servers.map((server) => ({ urls: server.urls, username: server.username, credential: server.credential })) }); } catch {}
        }
        await pc.setRemoteDescription({ type: "offer", sdp: message.sdp });
        for (const candidate of pendingIce.splice(0)) await pc.addIceCandidate(candidate).catch(() => {});
        const answer = await pc.createAnswer();
        await pc.setLocalDescription(answer);
        handlers.signal({ type: "rtc", sdp_type: "answer", sdp: pc.localDescription.sdp });
      } else if (message.type === "ice" && message.candidate) {
        if (!pc.remoteDescription) pendingIce.push(message.candidate);
        else await pc.addIceCandidate(message.candidate).catch(() => {});
      }
    } catch {
      handlers.failed();
    }
  }

  function sendReliable(bytes) {
    if (!input || input.readyState !== "open") return;
    const out = new Uint8Array(4 + bytes.length);
    new DataView(out.buffer).setUint32(0, ++seq >>> 0);
    out.set(bytes, 4);
    input.send(out);
  }

  function sendPointer(x, y, mask) {
    if (mask === lastMask && pointer && pointer.readyState === "open") {
      const out = new Uint8Array(8);
      const view = new DataView(out.buffer);
      view.setUint32(0, ++seq >>> 0);
      view.setUint16(4, x);
      view.setUint16(6, y);
      pointer.send(out);
      return;
    }
    lastMask = mask;
    sendReliable(new Uint8Array([5, mask, x >> 8, x & 255, y >> 8, y & 255]));
  }

  function key(keysym, down) {
    const out = new Uint8Array(8);
    out[0] = 4;
    out[1] = down ? 1 : 0;
    new DataView(out.buffer).setUint32(4, keysym >>> 0);
    sendReliable(out);
  }

  // The person's clipboard, as the computer's (Latin-1, as RFB carries it).
  function paste(text) {
    const chars = Array.from(text).map((ch) => ch.codePointAt(0)).filter((cp) => cp < 256);
    const out = new Uint8Array(8 + chars.length);
    out[0] = 6;
    new DataView(out.buffer).setUint32(4, chars.length);
    out.set(chars, 8);
    sendReliable(out);
  }

  // What arrived in the last call, for the bandwidth line.
  let lastBytes = 0;
  async function received() {
    let bytes = 0;
    try {
      const report = await pc.getStats();
      report.forEach((s) => {
        if (s.type === "inbound-rtp" && s.kind === "video") bytes = s.bytesReceived || 0;
      });
    } catch {}
    const delta = Math.max(0, bytes - lastBytes);
    lastBytes = bytes;
    return delta;
  }

  return {
    element: video,
    signal,
    sendPointer,
    key,
    paste,
    received,
    play,
    get width() { return width; },
    get height() { return height; },
    close() {
      closed = true;
      try { pc.close(); } catch {}
      video.srcObject = null;
      video.remove();
    },
  };
}
