import { describe, expect, test } from "bun:test";
import {
  liveAnswerRequestId,
  parseLiveAnswerParams,
  redirectLivePath,
  widgetBaseUrl,
} from "./liveSurface";

describe("redirectLivePath", () => {
  test("answer links open Brain with one-shot params", () => {
    expect(redirectLivePath("mewla://live/answer?q=abc123&c=1")).toBe("/?liveAnswer=abc123&liveChoice=1");
  });
  test("an answer link without a valid choice just opens Brain", () => {
    expect(redirectLivePath("mewla://live/answer?q=abc&c=9")).toBe("/");
    expect(redirectLivePath("mewla://live/open?k=running")).toBe("/");
  });
  test("other links are left alone", () => {
    expect(redirectLivePath("mewla://plugins?state=x")).toBe("mewla://plugins?state=x");
    expect(redirectLivePath("/list")).toBe("/list");
  });
});

describe("parseLiveAnswerParams", () => {
  test("needs a question and a choice index 0-3", () => {
    expect(parseLiveAnswerParams("q", "2")).toEqual({ questionId: "q", choiceIndex: 2 });
    expect(parseLiveAnswerParams("", "0")).toBeNull();
    expect(parseLiveAnswerParams("q", undefined)).toBeNull();
    expect(parseLiveAnswerParams("q", "4")).toBeNull();
  });
  test("the request id is stable per question and choice", () => {
    expect(liveAnswerRequestId({ questionId: "q", choiceIndex: 1 })).toBe("live:q:1");
  });
});

describe("widgetBaseUrl", () => {
  test("plain servers give an http(s) base, relays give none", () => {
    expect(widgetBaseUrl({ url: "ws://10.0.2.2:5560/ws", transportKind: "manual" })).toBe("http://10.0.2.2:5560");
    expect(widgetBaseUrl({ url: "wss://mewla.example.com/ws" })).toBe("https://mewla.example.com");
    expect(widgetBaseUrl({ url: "wss://relay.example.com/ws", transportKind: "link" })).toBeNull();
  });
});
