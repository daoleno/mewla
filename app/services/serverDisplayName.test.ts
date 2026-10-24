import { expect, test } from "bun:test";
import { serverDisplayName } from "./serverDisplayName";

test("a server's own name is kept", () => {
  expect(serverDisplayName("Studio Mac")).toBe("Studio Mac");
  expect(serverDisplayName("  mac-mini.local ")).toBe("mac-mini.local");
});

test("an address, or no name, reads as this computer", () => {
  for (const name of ["127.0.0.1", "127.0.0.1:9876", "localhost", "localhost:9876", "192.168.1.20:19933", "::1", "[::1]:9876", "fe80::1", "", "  ", undefined, null]) {
    expect(serverDisplayName(name)).toBe("this computer");
  }
  expect(serverDisplayName("127.0.0.1", "your computer")).toBe("your computer");
});
