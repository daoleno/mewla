import { expect, mock, test } from "bun:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";
import type { PluginAccount } from "../../services/connections";
if (!process.env.MEWLA_PROFILE_NAME_TEST) {
  test("profile name suggests identity, validates and submits the edited CLI name", () => {
    const child = Bun.spawnSync([process.execPath, "test", import.meta.filename], { env: { ...process.env, MEWLA_PROFILE_NAME_TEST: "1" } });
    if (child.exitCode) throw new Error(new TextDecoder().decode(child.stdout) + new TextDecoder().decode(child.stderr));
    expect(child.exitCode).toBe(0);
  });
} else {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  const host = (name: string) => (props: any) => React.createElement(name, props, props.children);
  mock.module("react-native", () => ({ StyleSheet: { create: (s: unknown) => s }, TextInput: host("input"), View: host("view") }));
  mock.module("../../constants/tokens", () => ({ useAppTheme: () => ({ colors: {} }) }));
  mock.module("../ui", () => ({ AppText: host("text"), Button: host("button") }));
  mock.module("../ui/MobileSingleLineInput", () => ({ MobileSingleLineInput: host("input") }));
  const { CloudProfileNameEntry } = await import("./CloudProfileNameEntry");
  test("post-sign-in suggestion is editable and concurrent submission is single-use", async () => {
    const account: PluginAccount = { id: "a", integration: "aws", name: "mewla-temp", profile: "mewla-temp", identity: "", enabled: true, status: "connected", history: [], aws: { account_id: "123456789012", account_name: "ACE Labs Mgmt", who: "Admin" } };
    let resolve!: (ok: boolean) => void;
    const save = mock(() => new Promise<boolean>(r => { resolve = r; }));
    let tree!: TestRenderer.ReactTestRenderer;
    await act(async () => { tree = TestRenderer.create(<CloudProfileNameEntry account={account} suggest onSave={save} />); });
    const input = () => tree.root.findByType("input" as any);
    const submit = () => tree.root.findByType("button" as any).props.onPress();
    expect(input().props.value).toBe("ace-labs-mgmt-admin");
    await act(async () => input().props.onChangeText("../bad"));
    await act(async () => submit());
    expect(save).not.toHaveBeenCalled();
    await act(async () => input().props.onChangeText("my-work-admin"));
    await act(async () => { submit(); submit(); });
    expect(save).toHaveBeenCalledTimes(1);
    expect(save).toHaveBeenCalledWith("my-work-admin");
    await act(async () => resolve(true));
    await act(async () => tree.unmount());
  });
}
