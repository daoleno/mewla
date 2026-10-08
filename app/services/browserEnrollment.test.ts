import { expect, mock, test } from "bun:test";

if (!process.env.ZEN_ENROLLMENT_API_TEST) {
  test("enrollment API contract in isolation", () => {
    const result = Bun.spawnSync([process.execPath, "test", import.meta.filename], { env: { ...process.env, ZEN_ENROLLMENT_API_TEST: "1" } });
    if (result.exitCode) throw new Error(new TextDecoder().decode(result.stderr));
    expect(result.exitCode).toBe(0);
  });
} else {
  mock.module("./auth", () => ({ getOrCreateLocalDeviceIdentity: async () => ({ deviceId: "device", deviceName: "Test", publicKeyHex: "a".repeat(64) }) }));
  const { requestBrowserEnrollment, readBrowserEnrollmentStatus, browserEnrollmentServer } = await import("./browserEnrollment");
  test("maps actual daemon snake_case responses through to trusted server storage", async () => {
    const fetcher = mock(async (input: any, init?: any) => {
      if (String(input).endsWith("/request")) {
        expect(JSON.parse(init.body).device_id).toBe("device");
        return Response.json({ request_id: "r", secret: "s", verification_number: "042", expires_at: "2099-01-01T00:00:00Z" });
      }
      return Response.json({ status: "approved", daemon_id: "a".repeat(64), daemon_public_key: "b".repeat(64), device_id: "device" });
    }) as unknown as typeof fetch;
    const prompt = await requestBrowserEnrollment("https://zen.example", fetcher);
    expect(prompt.requestId).toBe("r");
    expect(prompt.verificationNumber).toBe("042");
    const status = await readBrowserEnrollmentStatus("https://zen.example", prompt, fetcher);
    const server = browserEnrollmentServer("https://manjaro.example.ts.net/", status);
    expect(server?.daemonId).toBe("a".repeat(64));
    expect(server?.name).toBe("manjaro.example.ts.net");
    expect(browserEnrollmentServer("https://zen.example", { status: "denied" })).toBeNull();
  });
}
