import { expect, test } from "bun:test";
import { getOrCreateLocalDeviceIdentity } from "./auth";
import { installMemoryPlatform, memoryStore } from "./testing/memoryPlatform";

test("the device key the Expo app stored is the one this app signs with", async () => {
  const secure = memoryStore({
    "mewla.device.v3.id": "phone-1",
    "mewla.device.v3.name": "Pixel",
    "mewla.device.v3.seed": "1".repeat(64),
    "mewla.device.v3.public-key": "2".repeat(64),
  });
  installMemoryPlatform({ secure });
  expect(await getOrCreateLocalDeviceIdentity()).toMatchObject({ deviceId: "phone-1", deviceName: "Pixel" });
});

test("a secure store that can't read the key fails the sign-in and never writes a new key over it", async () => {
  const secure = memoryStore();
  let writes = 0;
  installMemoryPlatform({
    secure: {
      ...secure,
      getItem: () => Promise.reject(new Error("the key that protects it is missing")),
      setItem: async () => void writes++,
    },
  });
  await expect(getOrCreateLocalDeviceIdentity()).rejects.toThrow("missing");
  expect(writes).toBe(0);
});
