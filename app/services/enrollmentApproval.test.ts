import { describe, expect, test } from "bun:test";
import { canApproveEnrollment, verificationChoices } from "./enrollmentApproval";

describe("enrollment approval", () => {
  test("offers the real number among several choices", () => {
    expect(verificationChoices("042")).toContain("042");
    expect(verificationChoices("042")).toHaveLength(4);
  });
  test("requires exact number and unexpired request", () => {
    const request = { id: "r", deviceName: "Browser", platform: "web", origin: "zen.example", verificationNumber: "042", expiresAt: new Date(2000).toISOString() };
    expect(canApproveEnrollment(request, "042", Date.parse("2001-01-01"))).toBe(false);
    expect(canApproveEnrollment({ ...request, expiresAt: new Date(3000).toISOString() }, "043", Date.parse("2001-01-01"))).toBe(false);
  });
});
