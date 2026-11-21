import { describe, expect, test } from "bun:test";
import { DEVICE_SECURE_KEYS } from "../auth";
import type { KeyValueStore } from "../platform";
import { SERVER_KEYS } from "../servers";
import { collectHandover, importHandover, parseHandover } from "./handover";

const memory = (entries: Record<string, string>): KeyValueStore => ({
  getItem: async (key) => entries[key] ?? null,
  setItem: async (key, value) => void (entries[key] = value),
  removeItem: async (key) => void delete entries[key],
});

const device = { [DEVICE_SECURE_KEYS[0]]: "device-1", [DEVICE_SECURE_KEYS[1]]: "Pixel", [DEVICE_SECURE_KEYS[2]]: "a".repeat(64), [DEVICE_SECURE_KEYS[3]]: "b".repeat(64) };
const paired = { [SERVER_KEYS.servers]: "[{\"id\":\"s1\"}]", [SERVER_KEYS.currentServer]: "s1", [SERVER_KEYS.onboarded]: "true", "mewla:drafts": "never moves" };

describe("moving to another app", () => {
  test("a paired phone hands over its device key and its computers, and nothing else", async () => {
    const bundle = await collectHandover(memory(device), memory(paired));
    expect(bundle).toEqual({
      version: 1,
      secure: device,
      storage: { [SERVER_KEYS.servers]: paired[SERVER_KEYS.servers], [SERVER_KEYS.currentServer]: "s1", [SERVER_KEYS.onboarded]: "true" },
    });
  });

  test("a phone that was never paired has nothing to move", async () => {
    expect(await collectHandover(memory({}), memory({}))).toBeNull();
    expect(await collectHandover(memory(device), memory({}))).toBeNull();
    expect(await collectHandover(memory({}), memory(paired))).toBeNull();
  });

  test("a staged bundle reads back as itself; anything else is nothing", async () => {
    const bundle = (await collectHandover(memory(device), memory(paired)))!;
    expect(parseHandover(JSON.stringify(bundle))).toEqual(bundle);
    for (const bad of [null, undefined, "", "{", "[]", JSON.stringify({ ...bundle, version: 2 }), JSON.stringify({ ...bundle, secure: { x: 1 } }), JSON.stringify({ ...bundle, secure: {} })]) {
      expect(parseHandover(bad)).toBeNull();
    }
  });

  test("a staged bundle brings only the keys the app moves", () => {
    const text = JSON.stringify({ version: 1, secure: { ...device, "other.secret": "x" }, storage: { ...paired } });
    const parsed = parseHandover(text)!;
    expect(Object.keys(parsed.secure).sort()).toEqual([...DEVICE_SECURE_KEYS].sort());
    expect(parsed.storage["mewla:drafts"]).toBeUndefined();
  });

  test("the new app takes it into an empty store, and only there", async () => {
    const bundle = (await collectHandover(memory(device), memory(paired)))!;
    const secure = memory({});
    const storage = memory({});
    expect(await importHandover(bundle, secure, storage)).toBe("imported");
    expect(await collectHandover(secure, storage)).toEqual(bundle);

    // Scanned first: what is there stays.
    const other = memory({ ...device, [DEVICE_SECURE_KEYS[0]]: "device-2" });
    expect(await importHandover(bundle, other, memory({}))).toBe("kept");
    expect(await other.getItem(DEVICE_SECURE_KEYS[0])).toBe("device-2");
    const servers = memory({ [SERVER_KEYS.servers]: "[]" });
    expect(await importHandover(bundle, memory({}), servers)).toBe("kept");
    expect(await servers.getItem(SERVER_KEYS.servers)).toBe("[]");
  });
});
