import { describe, expect, test } from "bun:test";
import { sessionPageLinkToOpen, sessionPageShowsInApp, sessionPageUrl } from "./sessionPage";

describe("Session page", () => {
  test("its URL is the grant's path on the computer as this client reaches it", () => {
    expect(sessionPageUrl("ws://127.0.0.1:41234/ws", "/session-page/abc.def/report.html")).toBe("http://127.0.0.1:41234/session-page/abc.def/report.html");
    expect(sessionPageUrl("https://desk.example.net", "/session-page/abc.def/q3%20report.html")).toBe("https://desk.example.net/session-page/abc.def/q3%20report.html");
  });

  test("a path that is not a page grant's, or that leaves it, gives no URL", () => {
    for (const path of ["/devices", "//evil.example/session-page/x/y.html", "https://evil.example/session-page/x/y.html", "/session-page/x/../../devices", "/session-page/x/y.html?z", "/session-page/x\\y.html"]) {
      expect(sessionPageUrl("http://127.0.0.1:1", path)).toBeNull();
    }
    expect(sessionPageUrl("file:///tmp/x", "/session-page/x/y.html")).toBeNull();
  });

  test("the page can ask the app to open a web address only", () => {
    expect(sessionPageLinkToOpen(JSON.stringify({ open: "https://plotly.com/docs" }))).toBe("https://plotly.com/docs");
    expect(sessionPageLinkToOpen(JSON.stringify({ open: "http://example.com/a?b=1" }))).toBe("http://example.com/a?b=1");
    for (const open of ["mewla://pair?token=x", "file:///etc/passwd", "javascript:alert(1)", "intent://x#Intent;end", "data:text/html,x", "https://user:pw@example.com/", 42, null]) {
      expect(sessionPageLinkToOpen(JSON.stringify({ open }))).toBeNull();
    }
    expect(sessionPageLinkToOpen("not json")).toBeNull();
    expect(sessionPageLinkToOpen(JSON.stringify({ open: "https://x.example/" + "a".repeat(5000) }))).toBeNull();
  });
});

describe("where a Session page shows", () => {
  test("in the app's web view at 127.0.0.1 (Mewla Relay's tunnel, Mewla for Mac's own computer) and always in a browser", () => {
    expect(sessionPageShowsInApp("http://127.0.0.1:41234/session-page/a.b/r.html", "android")).toBe(true);
    expect(sessionPageShowsInApp("http://127.0.0.1:9876/session-page/a.b/r.html", "ios")).toBe(true);
    expect(sessionPageShowsInApp("https://desk.example.net/session-page/a.b/r.html", "web")).toBe(true);
  });

  test("a computer paired by its address opens the page in the system browser", () => {
    for (const url of ["http://192.168.1.20:9876/session-page/a.b/r.html", "http://100.64.0.3:9876/session-page/a.b/r.html", "https://desk.example.net/session-page/a.b/r.html", "http://localhost:9876/session-page/a.b/r.html", "not a url"]) {
      expect(sessionPageShowsInApp(url, "android")).toBe(false);
    }
  });
});
