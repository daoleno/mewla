import { expect, mock, test } from "bun:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";

// Keep native leaf mocks out of the rest of the app's test process.
if (!process.env.MEWLA_CLOUD_CODE_TEST_CHILD) {
  test("CLI code entry supports paste, errors and retry", () => {
    const result = Bun.spawnSync([process.execPath, "test", import.meta.filename], {
      env: { ...process.env, MEWLA_CLOUD_CODE_TEST_CHILD: "1" },
    });
    if (result.exitCode) throw new Error(new TextDecoder().decode(result.stdout) + new TextDecoder().decode(result.stderr));
    expect(result.exitCode).toBe(0);
  });
} else {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  const host = (name: string) => (props: any) => React.createElement(name, props, props.children);
  mock.module("react-native", () => ({ StyleSheet: { create: (s: unknown) => s }, TextInput: host("input"), View: host("view") }));
  mock.module("../../constants/tokens", () => ({ Radii: { md: 12 }, ContinuousCorners: {}, useAppColors: () => ({ border: "gray" }) }));
  mock.module("../ui", () => ({ AppText: host("text"), Button: host("button"), InlineNotice: host("notice") }));
  const clipboard = mock(async () => "");
  mock.module("../ui/MultilineField", () => ({ MultilineField: React.forwardRef((props: any, _ref) => React.createElement("input", props)) }));
  mock.module("expo-clipboard", () => ({ getStringAsync: clipboard }));
  const { CloudCodeEntry } = await import("./CloudCodeEntry");
  const callback = "Y29kZT10ZXN0LWNvZGUmc3RhdGU9dGVzdC1zdGF0ZQ==";
  const render = async (submit: (address: string) => Promise<void>) => {
    let tree!: TestRenderer.ReactTestRenderer;
    await act(async () => { tree = TestRenderer.create(<CloudCodeEntry onSubmit={submit} />); });
    return tree;
  };
  const input = (tree: TestRenderer.ReactTestRenderer) => tree.root.findByType("input" as any);
  const button = (tree: TestRenderer.ReactTestRenderer, label: string) => tree.root.findAllByType("button" as any).find(b => b.props.label === label)!;
  const notice = (tree: TestRenderer.ReactTestRenderer) => tree.root.findByType("notice" as any).props.detail;

  test("explicit paste reports clipboard denial and permits manual retry", async () => {
    const tree = await render(async () => {});
    clipboard.mockRejectedValueOnce(new Error("NotAllowedError"));
    await act(async () => button(tree, "Paste code").props.onPress());
    expect(notice(tree)).toContain("Paste directly");
    clipboard.mockResolvedValueOnce(callback);
    await act(async () => button(tree, "Paste code").props.onPress());
    expect(input(tree).props.value).toBe(callback);
    expect(tree.root.findAllByType("notice" as any)).toHaveLength(0);
    await act(async () => tree.unmount());
  });

  test("server errors and pasted input survive rerenders; pending submit runs only once", async () => {
    let reject!: (error: Error) => void;
    const submit = mock(() => new Promise<void>((_, fail) => { reject = fail; }));
    const tree = await render(submit);
    await act(async () => input(tree).props.onChangeText(` ${callback} `));
    await act(async () => { button(tree, "Continue").props.onPress(); button(tree, "Continue").props.onPress(); });
    expect(submit).toHaveBeenCalledTimes(1);
    expect(submit).toHaveBeenCalledWith(callback);
    expect(button(tree, "Continue").props.loading).toBe(true);
    expect(input(tree).props.editable).toBe(false);
    const message = "Paste the code AWS shows after sign-in. It must belong to this sign-in.";
    await act(async () => reject(new Error(message)));
    await act(async () => tree.update(<CloudCodeEntry onSubmit={submit} />));
    expect(notice(tree)).toBe(message);
    expect(input(tree).props.value).toBe(` ${callback} `);
    expect(button(tree, "Continue").props.loading).toBe(false);
    submit.mockResolvedValueOnce(undefined);
    await act(async () => button(tree, "Continue").props.onPress());
    expect(submit).toHaveBeenCalledTimes(2);
    expect(tree.root.findAllByType("notice" as any)).toHaveLength(0);
    await act(async () => tree.unmount());
  });
}
