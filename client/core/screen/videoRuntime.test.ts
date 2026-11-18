import { describe, expect, test } from "bun:test";

// The viewer page's video runtime (client/ui/more/screen/runtime/video.js), driven with a fake
// RTCPeerConnection: what the person types before the input channel opens is sent, in order,
// once it does.

class FakeChannel {
  readyState = "connecting";
  binaryType = "blob";
  onopen: (() => void) | null = null;
  sent: Uint8Array[] = [];
  constructor(readonly label: string) {}
  send(data: Uint8Array) {
    if (this.readyState !== "open") throw new Error("not open");
    this.sent.push(data);
  }
  open() {
    this.readyState = "open";
    this.onopen?.();
  }
}

class FakePeer {
  static last: FakePeer;
  ondatachannel: ((event: { channel: FakeChannel }) => void) | null = null;
  ontrack = null;
  onicecandidate = null;
  onconnectionstatechange = null;
  constructor() {
    FakePeer.last = this;
  }
  close() {}
}

const fakeDoc = {
  createElement: () => ({ setAttribute() {}, remove() {}, play: () => Promise.resolve(), addEventListener() {} }),
};

describe("video runtime", () => {
  test("keys and paste from before the input channel opened are sent, in order, when it does", async () => {
    (globalThis as Record<string, unknown>).RTCPeerConnection = FakePeer;
    const { createVideoPicture } = await import("../../ui/more/screen/runtime/video.js");
    const picture = createVideoPicture(fakeDoc, { signal() {}, failed() {} });
    picture.key(0x61, true);
    picture.key(0x61, false);
    picture.paste("hi");
    const input = new FakeChannel("input");
    FakePeer.last.ondatachannel?.({ channel: input });
    expect(input.sent).toHaveLength(0);
    input.open();
    const kinds = input.sent.map((m) => m[4]);
    const seqs = input.sent.map((m) => new DataView(m.buffer).getUint32(0));
    expect(kinds).toEqual([4, 4, 6]);
    expect(seqs).toEqual([1, 2, 3]);
    picture.key(0x62, true);
    expect(input.sent).toHaveLength(4);
    picture.close();
  });
});
