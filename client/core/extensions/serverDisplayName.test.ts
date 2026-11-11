import { expect, test } from "bun:test";
import { serverDisplayName } from "./serverDisplayName";

test("a server named by its address is called this computer in sentences", () => {
  expect(serverDisplayName("Studio")).toBe("Studio");
  expect(serverDisplayName("127.0.0.1:9877")).toBe("this computer");
  expect(serverDisplayName("localhost")).toBe("this computer");
  expect(serverDisplayName(" ")).toBe("this computer");
  expect(serverDisplayName(undefined, "the server")).toBe("the server");
});
