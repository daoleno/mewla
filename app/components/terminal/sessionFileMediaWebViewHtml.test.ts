import { describe, expect, test } from "bun:test";
import {
  buildSessionFileMediaHtml,
  sessionFileMediaFirstFrameUri,
  sessionFileMediaOrigin,
} from "./sessionFileMediaWebViewHtml";

const STREAM =
  "http://127.0.0.1:48123/session-file?path=%2Ftmp%2Fweb.mp4&file_cap_get=ab";

describe("native media page", () => {
  test("streams without browser controls and plays inline", () => {
    const html = buildSessionFileMediaHtml({ kind: "video", uri: STREAM, background: "#F5F3EE" });
    expect(html).toContain('<video id="media" playsinline webkit-playsinline preload="metadata">');
    expect(html).not.toContain(" controls");
    expect(html).toContain(JSON.stringify(sessionFileMediaFirstFrameUri(STREAM)));
    expect(html).toContain("window.__mewlaMedia");
    expect(html).toContain("background:#F5F3EE");
  });

  test("audio uses an audio element and a hostile URL cannot close the script", () => {
    const html = buildSessionFileMediaHtml({
      kind: "audio",
      uri: "http://127.0.0.1:1/x?p=</script><script>alert(1)</script>",
      background: "red;}</style><script>",
    });
    expect(html).toContain('<audio id="media"');
    expect(html).not.toContain("</script><script>alert(1)");
    expect(html).toContain("background:transparent");
  });

  test("the first frame shows as the poster without changing the request", () => {
    expect(sessionFileMediaFirstFrameUri(STREAM)).toBe(`${STREAM}#t=0.001`);
    expect(sessionFileMediaFirstFrameUri(`${STREAM}#t=4`)).toBe(`${STREAM}#t=4`);
    expect(sessionFileMediaOrigin(STREAM)).toBe("http://127.0.0.1:48123/");
    expect(sessionFileMediaOrigin("not a url")).toBeUndefined();
  });
});
