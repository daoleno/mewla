import { describe, expect, test } from "bun:test";
import { chatUpdateRow } from "./chatRow";

describe("chatUpdateRow", () => {
  test("a computer from before bounded updates is offered its waiting update at once", () => {
    // The 11-17 customer: 0.4.12 with a Worker always running never installed 0.4.14 by itself.
    expect(chatUpdateRow({ version: "0.4.12", supported: true, latest: "0.4.14" })).toEqual({
      key: "update",
      title: "Update to 0.4.14 waiting for Workers to finish",
      button: "Update now",
      action: "update",
    });
  });

  test("a bounded computer says what its staged update waits for", () => {
    const row = chatUpdateRow({ version: "0.4.15", supported: true, latest: "0.4.16", waiting_for: "a permission answer", install_by: "2026-11-18T10:00:00Z" });
    expect(row?.title).toBe("Update to 0.4.16 waiting for a permission answer");
    expect(row?.button).toBe("Update now");
  });

  test("nothing while a bounded update is about to install by itself, is under way, failed, or cannot install", () => {
    expect(chatUpdateRow({ version: "0.4.15", supported: true, latest: "0.4.16" })).toBeNull();
    expect(chatUpdateRow({ version: "0.4.12", supported: true, latest: "0.4.14", updating: true })).toBeNull();
    expect(chatUpdateRow({ version: "0.4.12", supported: true, latest: "0.4.14", error: "Mewla 0.4.14 did not start; still on 0.4.12" })).toBeNull();
    expect(chatUpdateRow({ version: "0.4.12", supported: false, latest: "0.4.14" })).toBeNull();
    expect(chatUpdateRow({ version: "0.4.14", supported: true })).toBeNull();
    expect(chatUpdateRow(null)).toBeNull();
  });

  test("old terminals ask for a restart and say what it ends", () => {
    expect(chatUpdateRow({ version: "0.4.15", supported: true, host_version: "0.4.6", terminals_restart: true, live_terminals: 2 })).toEqual({
      key: "terminals",
      title: "Restart terminals to finish updating",
      detail: "Ends 2 open terminals",
      button: "Restart",
      action: "terminals",
    });
    expect(chatUpdateRow({ version: "0.4.15", supported: true, terminals_restart: true, live_terminals: 1 })?.detail).toBe("Ends 1 open terminal");
  });
});
