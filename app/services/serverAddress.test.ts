import { describe, expect, test } from "bun:test";
import {
  normalizeServerURL,
  serverAddressForDisplay,
  serverAddressProblem,
} from "./storedServerContract";

describe("server address", () => {
  test("shows the saved address the way people write it", () => {
    expect(serverAddressForDisplay("wss://mewla.example.com/ws")).toBe("https://mewla.example.com");
    expect(serverAddressForDisplay("ws://192.168.1.4:7681/ws")).toBe("http://192.168.1.4:7681");
    expect(serverAddressForDisplay("wss://host.example/mewla/ws")).toBe("https://host.example/mewla/ws");
  });

  test("saving the shown address keeps the stored URL", () => {
    for (const stored of [
      "wss://mewla.example.com/ws",
      "ws://192.168.1.4:7681/ws",
      "wss://host.example:8443/ws",
      "wss://host.example/mewla/ws",
      "ws://[::1]:7681/ws",
    ]) {
      expect(normalizeServerURL(serverAddressForDisplay(stored))).toBe(stored);
    }
  });

  test("accepts every address form it accepted before", () => {
    for (const typed of [
      "https://mewla.example.com",
      "http://192.168.1.4:7681",
      "wss://mewla.example.com/ws",
      "ws://localhost:7681/ws",
    ]) {
      expect(serverAddressProblem(typed)).toBeNull();
    }
  });

  test("names what is wrong", () => {
    expect(serverAddressProblem("  ")).toContain("Enter the computer's address");
    expect(serverAddressProblem("mewla.example.com")).toContain("Start the address with https://");
    expect(serverAddressProblem("ftp://mewla.example.com")).toContain("ftp:");
    for (const typed of ["", "mewla.example.com", "ftp://x.example", "https://"]) {
      expect(serverAddressProblem(typed)).not.toBeNull();
      expect(serverAddressProblem(typed)).not.toContain("ws://");
    }
  });
});
