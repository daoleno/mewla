import { expect, mock, test } from "bun:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";

if (!process.env.MEWLA_CLAUDE_CHOICE_CARD_TEST) {
  test("Claude choice card in isolation", () => {
    const result = Bun.spawnSync([process.execPath, "test", import.meta.filename], {
      env: { ...process.env, MEWLA_CLAUDE_CHOICE_CARD_TEST: "1" },
    });
    if (result.exitCode) throw new Error(new TextDecoder().decode(result.stderr));
    expect(result.exitCode).toBe(0);
  });
} else {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  mock.module("react-native", () => ({
    ActivityIndicator: "ActivityIndicator",
    Pressable: "Pressable",
    Text: "Text",
    TextInput: "TextInput",
    View: "View",
    StyleSheet: { create: (styles: unknown) => styles },
  }));
  mock.module("../../constants/tokens", () => ({
    TypeScale: { micro: {}, label: {}, caption: {} },
    Typography: { chatFont: "f", chatFontMedium: "fm", chatMonoFont: "mono" },
  }));
  mock.module("../ui/outlinedSurface", () => ({ outlinedSurface: () => ({}) }));
  mock.module("../icons/Icon", () => ({ Icon: "Icon" }));
  const { ClaudeChoiceCard } = await import("./ClaudeChoiceCard");
  const chrome = new Proxy({}, { get: (_target, key) => String(key) }) as any;
  const choice = {
    kind: "AskUserQuestion",
    state: "pending" as const,
    questions: [
      {
        question: "Which fruit?",
        header: "Fruit",
        options: [
          { label: "Apple", description: "Red and crisp" },
          { label: "Banana", description: "Yellow" },
        ],
      },
      {
        question: "Which colors?",
        header: "Colors",
        multi_select: true,
        options: [
          { label: "Red", description: "warm" },
          { label: "Green", description: "calm" },
          { label: "Blue", description: "cool" },
        ],
      },
    ],
  };

  function render(live: boolean, onSubmit: (payload: any) => Promise<void>) {
    let renderer!: TestRenderer.ReactTestRenderer;
    act(() => {
      renderer = TestRenderer.create(
        <ClaudeChoiceCard callId="toolu_1" choice={choice} live={live} chrome={chrome} onSubmit={onSubmit} onSwitchToTerminal={() => {}} />,
      );
    });
    const pressable = (label: string) =>
      renderer.root.findAllByType("Pressable" as any).find((node) => node.props.accessibilityLabel === label)!;
    const optionPressable = (label: string) =>
      renderer.root
        .findAllByType("Pressable" as any)
        .find((node) => String(node.props.accessibilityLabel).startsWith(`${label},`))!;
    const inputs = () => renderer.root.findAllByType("TextInput" as any);
    return { renderer, pressable, optionPressable, inputs };
  }

  test("sends exactly the selected options and Other text", async () => {
    const payloads: any[] = [];
    const { renderer, pressable, optionPressable, inputs } = render(true, async (payload) => {
      payloads.push(payload);
    });
    expect(pressable("Send answer").props.disabled).toBe(true);
    await act(async () => optionPressable("Banana").props.onPress());
    await act(async () => optionPressable("Red").props.onPress());
    await act(async () => optionPressable("Blue").props.onPress());
    await act(async () => optionPressable("Red").props.onPress());
    await act(async () => inputs()[1].props.onFocus());
    // Other is chosen but empty: the answer is incomplete.
    expect(pressable("Send answer").props.disabled).toBe(true);
    await act(async () => inputs()[1].props.onChangeText("Teal; x"));
    expect(pressable("Send answer").props.disabled).toBe(false);
    await act(async () => pressable("Send answer").props.onPress());
    expect(payloads).toEqual([
      { call_id: "toolu_1", answers: [{ selected: [1] }, { selected: [2], other: "Teal; x" }] },
    ]);
    expect(optionPressable("Blue").props.accessibilityRole).toBe("checkbox");
    expect(optionPressable("Apple").props.accessibilityRole).toBe("radio");
    await act(async () => renderer.unmount());
  });

  test("shows the daemon's refusal and keeps the draft for retry", async () => {
    let attempts = 0;
    const { renderer, pressable, optionPressable } = render(true, async () => {
      attempts += 1;
      throw new Error("The Terminal is not showing this question.");
    });
    await act(async () => optionPressable("Apple").props.onPress());
    await act(async () => optionPressable("Green").props.onPress());
    await act(async () => pressable("Send answer").props.onPress());
    expect(JSON.stringify(renderer.toJSON())).toContain("The Terminal is not showing this question.");
    expect(pressable("Send answer").props.disabled).toBe(false);
    await act(async () => pressable("Send answer").props.onPress());
    expect(attempts).toBe(2);
    await act(async () => renderer.unmount());
  });

  test("is read-only until the live pane shows the prompt", async () => {
    const { renderer, pressable, optionPressable } = render(false, async () => {});
    expect(optionPressable("Apple").props.disabled).toBe(true);
    expect(pressable("Send answer").props.disabled).toBe(true);
    expect(pressable("Open Terminal")).toBeTruthy();
    expect(JSON.stringify(renderer.toJSON())).toContain("Waiting for the prompt to appear in the Terminal");
    await act(async () => renderer.unmount());
  });
}
