import { describe, expect, test } from "bun:test";
import type { AgentDisplay } from "./screen";
import { echoLine, echoNextChange, fitMessage, hostLabel, parseReflow, pickStageAgent, pushEcho, shouldFit, stageAgents, stageName } from "./stage";

const display = (id: string, more: Partial<AgentDisplay> = {}): AgentDisplay => ({ id, width: 1280, height: 800, control: "agent", apps: 0, browser: 1, ...more });

describe("stage agents", () => {
  test("a Worker's name never carries the daemon's Session id", () => {
    expect(stageName(display("mh-1", { name: "Sign in to Acme (mh-17b3337d43f38037916f0e5b)" }))).toBe("Sign in to Acme");
    expect(stageName(display("mh-1"), "Release notes (mh-aa)")).toBe("Release notes");
    expect(stageName(display("mh-1"))).toBe("An agent");
  });

  test("rows keep the computer's order and say who holds what", () => {
    const rows = stageAgents([display("a", { control: "person", idle_s: 0 }), display("b", { stopped: true, browser: 0, apps: 1 })], (id) => (id === "a" ? "Acme" : undefined));
    expect(rows.map((r) => r.id)).toEqual(["a", "b"]);
    expect(rows[0]).toMatchObject({ name: "Acme", held: true, acting: true, browser: true });
    expect(rows[1]).toMatchObject({ name: "An agent", stopped: true, browser: false });
  });

  test("the stage shows the agent asked for, else the one shown, else the first", () => {
    const list = [{ id: "a" }, { id: "b" }];
    expect(pickStageAgent(list, "b", "a")).toBe("b");
    expect(pickStageAgent(list, "gone", "b")).toBe("b");
    expect(pickStageAgent(list, undefined, "gone")).toBe("a");
    expect(pickStageAgent([], "a", "a")).toBeNull();
  });
});

describe("laying the page out for the phone", () => {
  const phone = { width: 412, height: 760, dpr: 2.625 };

  test("the first fit, a turn of the phone and a taller view lay it out again", () => {
    expect(shouldFit(null, phone)).toBe(true);
    expect(shouldFit(phone, { ...phone, width: 760, height: 412 })).toBe(true);
    expect(shouldFit({ ...phone, height: 500 }, phone)).toBe(true);
  });

  test("the keyboard coming up never lays it out again", () => {
    expect(shouldFit(phone, { ...phone, height: 430 })).toBe(false);
    expect(shouldFit(phone, phone)).toBe(false);
  });

  test("a view too small for a page (a float) is never laid out for", () => {
    expect(shouldFit(null, { width: 168, height: 300, dpr: 3 })).toBe(false);
    expect(shouldFit(null, { width: 412, height: 760, dpr: 0 })).toBe(false);
  });

  test("the fit message is whole points", () => {
    expect(fitMessage({ width: 411.43, height: 759.6, dpr: 2.6251 })).toEqual({ type: "fit", width: 411, height: 760, dpr: 2.625 });
  });

  test("the daemon's reflow message", () => {
    expect(parseReflow({ type: "reflow", on: true, host: "github.com" })).toEqual({ on: true, host: "github.com" });
    expect(parseReflow({ type: "reflow", on: false, host: "github.com" })).toEqual({ on: false, host: "" });
    expect(parseReflow({ type: "reflow" })).toBeNull();
    expect(parseReflow({ type: "pong", on: true })).toBeNull();
    expect(hostLabel("www.github.com")).toBe("github.com");
  });
});

describe("the typing echo", () => {
  test("each character shows for a second, then a dot", () => {
    let echo = pushEcho([], { kind: "text", text: "pa" }, 1_000);
    echo = pushEcho(echo, { kind: "text", text: "ss" }, 1_600);
    expect(echoLine(echo, 1_700)).toBe("pass");
    expect(echoLine(echo, 2_100)).toBe("••ss");
    expect(echoLine(echo, 2_700)).toBe("••••");
    expect(echoNextChange(echo, 1_700)).toBe(2_000);
    expect(echoNextChange(echo, 2_700)).toBeNull();
  });

  test("a backspace takes a character away; Chinese stays whole characters", () => {
    let echo = pushEcho([], { kind: "text", text: "你好a" }, 0);
    echo = pushEcho(echo, { kind: "backspace", count: 1 }, 10);
    expect(echoLine(echo, 20)).toBe("你好");
  });
});
