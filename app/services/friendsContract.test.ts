import { describe, expect, test } from "bun:test";
import {
  FRIENDS_RETURN_URL,
  friendsEnrollStartURL,
  friendsKeyFromSeed,
  friendsRequestPath,
  friendsServiceURL,
  friendsSignaturePayload,
  isFriendsReturnUrl,
  parseFriendsEnrollReturn,
  signFriendsRequest,
} from "./friendsContract";

const seed = Array.from({ length: 32 }, (_, i) => (i + 1).toString(16).padStart(2, "0")).join("");

describe("Friends signing contract", () => {
  // Fixture produced by daemon/reqsign (Go) over the same inputs: the app and
  // the service must agree on these bytes.
  test("signs exactly what the service verifies", () => {
    const key = friendsKeyFromSeed(seed);
    expect(key.publicKeyHex).toBe("79b5562e8fe654f94078b112e8a98ba7901f853ae695bed7e0e3910bad049664");
    const bodyDigest = "015abd7f5cc57a2dd94b7590f04ad8084273905ee33ec5cebeae62276a97f862"; // sha256 of {"a":1}
    const header = signFriendsRequest(key, {
      method: "POST",
      path: "/friends/v1/me/link-code",
      timestampMS: "1800000000000",
      nonceHex: "00112233445566778899aabbccddeeff",
      bodySha256Hex: bodyDigest,
    });
    expect(header).toBe(
      "MewlaFriendsHuman v1:79b5562e8fe654f94078b112e8a98ba7901f853ae695bed7e0e3910bad049664:1800000000000:00112233445566778899aabbccddeeff:" +
        "f0ea1aeb15bc48fc3e0e797b3afeaa3db1a81c1d13b63c4c8a145efbef2589335a25d01e1550fc8ab33facb10c4b85c3f5f10821b334dd8a55efa76ba421c602",
    );
  });

  test("the payload is domain, method, path, time, nonce, body digest", () => {
    const text = new TextDecoder().decode(friendsSignaturePayload("GET", "/friends/v1/me", "1", "ab", "CD"));
    expect(text).toBe("mewla-friends-human-v1\nGET\n/friends/v1/me\n1\nab\ncd");
  });

  test("the signed path keeps the mount prefix", () => {
    expect(friendsRequestPath("https://connect.mewla.ai/friends", "/v1/me")).toBe("/friends/v1/me");
    expect(friendsRequestPath("http://10.0.2.2:9000/friends/", "/v1/me")).toBe("/friends/v1/me");
  });

  test("the service URL defaults and can be pointed at a local one", () => {
    expect(friendsServiceURL("")).toBe("https://connect.mewla.ai/friends");
    expect(friendsServiceURL(" http://10.0.2.2:9000/friends/ ")).toBe("http://10.0.2.2:9000/friends");
  });
});

describe("Friends sign-in links", () => {
  test("the start link carries the key and only the app's return link", () => {
    const url = friendsEnrollStartURL("https://x.example/friends/", "ab".repeat(32));
    expect(url.startsWith("https://x.example/friends/v1/enroll/start?")).toBe(true);
    expect(new URL(url).searchParams.get("return")).toBe(FRIENDS_RETURN_URL);
    expect(new URL(url).searchParams.get("key")).toBe("ab".repeat(32));
  });

  test("reads a ticket and refuses anything malformed or foreign", () => {
    const ticket = "a".repeat(32);
    const challenge = "b".repeat(32);
    expect(parseFriendsEnrollReturn(`${FRIENDS_RETURN_URL}?ticket=${ticket}&challenge=${challenge}`)).toEqual({ ok: true, ticket, challenge });
    expect(parseFriendsEnrollReturn(`${FRIENDS_RETURN_URL}?error=account_too_new`)).toEqual({ ok: false, error: "account_too_new" });
    expect(parseFriendsEnrollReturn(`${FRIENDS_RETURN_URL}?error=denied`)).toEqual({ ok: false, error: "denied" });
    expect(parseFriendsEnrollReturn(`${FRIENDS_RETURN_URL}?ticket=short&challenge=${challenge}`)).toEqual({ ok: false, error: "invalid" });
    expect(parseFriendsEnrollReturn(`mewla://plugins?ticket=${ticket}&challenge=${challenge}`)).toEqual({ ok: false, error: "invalid" });
    expect(isFriendsReturnUrl("mewla://friends/enrollx")).toBe(false);
  });
});
