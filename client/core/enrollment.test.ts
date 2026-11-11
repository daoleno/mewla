import { beforeEach, describe, expect, test } from "bun:test";
import { browserEnrollmentServer, decideEnrollment, enrollmentOrigin, pendingEnrollments, readBrowserEnrollmentStatus, requestBrowserEnrollment, verificationChoices } from "./enrollment";
import type { StoredServer } from "./storedServerContract";
import { installMemoryPlatform } from "./testing/memoryPlatform";

type Call = { url: string; init?: RequestInit };
let calls: Call[];
let answer: (call: Call) => Response;

beforeEach(() => {
  calls = [];
  installMemoryPlatform({
    fetch: async (url, init) => {
      const call = { url, init };
      calls.push(call);
      return answer(call);
    },
  });
});

const server = { id: "s1", name: "laptop", url: "https://laptop.example.ts.net", daemonId: "d".repeat(64) } as StoredServer;

describe("a browser asks to be let in", () => {
  test("requests with this browser's key, polls the status, and saves the computer named by its address", async () => {
    answer = ({ url }) => url.endsWith("/enrollment/request")
      ? Response.json({ request_id: "r", secret: "s", verification_number: "042", expires_at: "2099-01-01T00:00:00Z" })
      : Response.json({ status: "approved", daemon_id: "a".repeat(64), daemon_public_key: "b".repeat(64), device_id: "device" });
    const prompt = await requestBrowserEnrollment("https://laptop.example.ts.net/settings#x");
    expect(prompt).toMatchObject({ requestId: "r", verificationNumber: "042" });
    const body = JSON.parse(String(calls[0]!.init!.body));
    expect(body).toMatchObject({ platform: "web", origin: "https://laptop.example.ts.net" });
    expect(body.device_public_key).toMatch(/^[0-9a-f]{64}$/);
    const status = await readBrowserEnrollmentStatus("https://laptop.example.ts.net", prompt);
    expect(calls[1]!.url).toBe("https://laptop.example.ts.net/enrollment/status?id=r&secret=s");
    expect(browserEnrollmentServer("https://laptop.example.ts.net/", status)).toMatchObject({ name: "laptop.example.ts.net", daemonId: "a".repeat(64), transportKind: "manual" });
    expect(browserEnrollmentServer("https://laptop.example.ts.net", { status: "denied" })).toBeNull();
  });

  test("only https, or http on this computer", () => {
    expect(enrollmentOrigin("http://127.0.0.1:9877/x")).toBe("http://127.0.0.1:9877");
    expect(() => enrollmentOrigin("http://192.0.2.4:9877")).toThrow("Mewla enrollment requires HTTPS.");
  });
});

describe("a paired device approves", () => {
  test("offers the real number among four", () => {
    const choices = verificationChoices("042");
    expect(choices).toHaveLength(4);
    expect(choices).toContain("042");
    expect(new Set(choices).size).toBe(4);
  });

  test("lists pending requests and sends a signed decision; a wrong number says so", async () => {
    answer = ({ url, init }) => url.endsWith("/pending")
      ? Response.json({ requests: [{ request_id: "r", verification_number: "042", device_name: "Firefox", platform: "web", origin: "https://laptop.example.ts.net", expires_at: "2099-01-01T00:00:00Z", status: "pending" }, { request_id: "old", verification_number: "001", expires_at: "2099-01-01T00:00:00Z", status: "approved" }] })
      : JSON.parse(String(init?.body)).verification_number === "042" ? new Response("{}") : new Response("verification number mismatch", { status: 409 });
    const pending = await pendingEnrollments(server);
    expect(pending.map((p) => p.id)).toEqual(["r"]);
    expect((calls[0]!.init!.headers as Record<string, string>).Authorization).toBeTruthy();
    await decideEnrollment(server, pending[0]!, "042", true);
    expect(JSON.parse(String(calls[1]!.init!.body))).toEqual({ request_id: "r", verification_number: "042", approve: true });
    await expect(decideEnrollment(server, pending[0]!, "043", true)).rejects.toThrow("That number doesn’t match");
  });
});
