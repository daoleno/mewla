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
    ScrollView: "ScrollView",
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

  const fourQuestions = {
    kind: "AskUserQuestion",
    state: "pending" as const,
    questions: [
      {
        question: "Where do backups go?",
        header: "Backup",
        options: [
          { label: "Daily pg_dump", description: "Dump once a day" },
          { label: "Cloud SQL", description: "Managed" },
          { label: "Other node", description: "Simplest" },
        ],
      },
      {
        question: "Who can sign up?",
        header: "Signup",
        options: [{ label: "Invite only" }, { label: "Open" }],
      },
      {
        question: "Which regions?",
        header: "Regions",
        multi_select: true,
        options: [{ label: "US East" }, { label: "EU West" }, { label: "Asia" }],
      },
      {
        question: "Default tier?",
        header: "Tier",
        options: [{ label: "Free" }, { label: "Pro" }],
      },
    ],
  };
  // Captured by driving the flat card (the one before the one-question-at-a-time
  // rewrite) through the same answers: Cloud SQL, Invite only, US East + Asia +
  // Other "Oceania", Pro. The wizard must send these exact answers.
  const FOUR_QUESTION_PAYLOAD = {
    call_id: "toolu_4",
    answers: [{ selected: [1] }, { selected: [0] }, { selected: [0, 2], other: "Oceania" }, { selected: [1] }],
  };

  function render(live: boolean, onSubmit: (payload: any) => Promise<void>, shown: any = choice, callId = "toolu_1") {
    let renderer!: TestRenderer.ReactTestRenderer;
    act(() => {
      renderer = TestRenderer.create(
        <ClaudeChoiceCard callId={callId} choice={shown} live={live} chrome={chrome} onSubmit={onSubmit} />,
      );
    });
    const all = () => renderer.root.findAllByType("Pressable" as any);
    const pressable = (label: string) => all().find((node) => node.props.accessibilityLabel === label)!;
    const optionPressable = (label: string) =>
      all().find((node) => {
        const name = String(node.props.accessibilityLabel);
        return name === label || name.startsWith(`${label},`);
      })!;
    const tab = (name: string) =>
      all().find(
        (node) => node.props.accessibilityRole === "tab" && String(node.props.accessibilityLabel).startsWith(`${name},`),
      )!;
    const inputs = () => renderer.root.findAllByType("TextInput" as any);
    const text = () => JSON.stringify(renderer.toJSON());
    const press = (node: TestRenderer.ReactTestInstance) => act(async () => node.props.onPress());
    return { renderer, pressable, optionPressable, tab, inputs, text, press };
  }

  test("a 4-question prompt sends the same payload the flat card sent", async () => {
    const payloads: any[] = [];
    const { renderer, pressable, optionPressable, inputs, press, text } = render(
      true,
      async (payload) => {
        payloads.push(payload);
      },
      fourQuestions,
      "toolu_4",
    );
    await press(optionPressable("Cloud SQL")); // Backup, moves on
    expect(text()).toContain("Who can sign up?");
    await press(optionPressable("Invite only")); // Signup, moves on
    expect(text()).toContain("Which regions?");
    await press(optionPressable("US East"));
    await press(optionPressable("Asia"));
    await act(async () => inputs()[0].props.onFocus());
    await act(async () => inputs()[0].props.onChangeText("Oceania"));
    await act(async () => inputs()[0].props.onBlur());
    await press(pressable("Next question"));
    await press(optionPressable("Pro")); // Tier, moves to review
    expect(text()).toContain("Review your answers");
    expect(pressable("Send answer").props.disabled).toBe(false);
    await press(pressable("Send answer"));
    expect(payloads).toEqual([FOUR_QUESTION_PAYLOAD]);
    await act(async () => renderer.unmount());
  });

  test("a single-select pick moves to the next question and the last one to review", async () => {
    const { renderer, optionPressable, press, text } = render(true, async () => {});
    expect(text()).toContain("Which fruit?");
    expect(text()).not.toContain("Which colors?");
    await press(optionPressable("Banana"));
    expect(text()).toContain("Which colors?");
    expect(text()).not.toContain("Which fruit?");
    await act(async () => renderer.unmount());
  });

  test("going back and changing an answer keeps the other answers", async () => {
    const payloads: any[] = [];
    const { renderer, pressable, optionPressable, tab, press, text } = render(true, async (payload) => {
      payloads.push(payload);
    });
    await press(optionPressable("Banana"));
    await press(optionPressable("Blue"));
    await press(pressable("Next question")); // multi-select has no auto-advance
    expect(text()).toContain("Review your answers");
    // Back to the first question from the tab row, change the answer.
    await press(tab("Fruit"));
    expect(text()).toContain("Which fruit?");
    await press(optionPressable("Apple"));
    expect(text()).toContain("Which colors?");
    // Previous returns one step.
    await press(pressable("Previous question"));
    expect(text()).toContain("Which fruit?");
    await press(pressable("Next question"));
    await press(pressable("Next question"));
    await press(pressable("Send answer"));
    expect(payloads).toEqual([{ call_id: "toolu_1", answers: [{ selected: [0] }, { selected: [2] }] }]);
    await act(async () => renderer.unmount());
  });

  test("tabs show which questions are answered and the review says what is missing", async () => {
    const { renderer, pressable, optionPressable, tab, press, text } = render(true, async () => {});
    expect(tab("Fruit").props.accessibilityLabel).toBe("Fruit, not answered");
    await press(optionPressable("Apple"));
    expect(tab("Fruit").props.accessibilityLabel).toBe("Fruit, answered");
    expect(tab("Colors").props.accessibilityLabel).toBe("Colors, not answered");
    expect(tab("Review").props.accessibilityLabel).toBe("Review, 1 not answered");
    await press(tab("Review"));
    expect(text()).toContain("Not answered");
    expect(text()).toContain("1 question left");
    expect(pressable("Answer 1 more").props.disabled).toBe(false);
    expect(() => pressable("Send answer").props).toThrow();
    // The button takes the user to the first unanswered question.
    await press(pressable("Answer 1 more"));
    expect(text()).toContain("Which colors?");
    await act(async () => renderer.unmount());
  });

  test("review rows jump to their question", async () => {
    const { renderer, optionPressable, tab, press, text } = render(true, async () => {});
    await press(optionPressable("Apple"));
    await press(optionPressable("Red"));
    await press(tab("Review"));
    const row = renderer.root
      .findAllByType("Pressable" as any)
      .find(
        (node) => node.props.accessibilityRole === "button" && String(node.props.accessibilityLabel).startsWith("Colors,"),
      )!;
    expect(row.props.accessibilityLabel).toBe("Colors, Red");
    await press(row);
    expect(text()).toContain("Which colors?");
    await act(async () => renderer.unmount());
  });

  test("multi-select toggles checkboxes and Next only opens once something is chosen", async () => {
    const { renderer, pressable, optionPressable, press, text } = render(true, async () => {});
    await press(optionPressable("Apple"));
    expect(optionPressable("Red").props.accessibilityRole).toBe("checkbox");
    expect(pressable("Next question").props.disabled).toBe(true);
    await press(optionPressable("Red"));
    await press(optionPressable("Blue"));
    expect(optionPressable("Red").props.accessibilityState.checked).toBe(true);
    expect(text()).toContain("Which colors?"); // no auto-advance
    await press(optionPressable("Red"));
    expect(optionPressable("Red").props.accessibilityState.checked).toBe(false);
    expect(optionPressable("Blue").props.accessibilityState.checked).toBe(true);
    expect(pressable("Next question").props.disabled).toBe(false);
    await press(optionPressable("Blue"));
    expect(pressable("Next question").props.disabled).toBe(true);
    await act(async () => renderer.unmount());
  });

  test("Other is an inline row: typing selects it, an empty Other is unanswered", async () => {
    const payloads: any[] = [];
    const { renderer, pressable, optionPressable, tab, inputs, press, text } = render(true, async (payload) => {
      payloads.push(payload);
    });
    expect(optionPressable("Apple").props.accessibilityRole).toBe("radio");
    await act(async () => inputs()[0].props.onFocus());
    // While typing, the options step aside so the keyboard leaves room under the header.
    expect(text()).not.toContain("Red and crisp");
    expect(pressable("Next question").props.disabled).toBe(true); // empty Other
    await act(async () => inputs()[0].props.onChangeText("Mango; ripe\n"));
    expect(inputs()[0].props.value).toBe("Mango; ripe ");
    expect(pressable("Next question").props.disabled).toBe(false);
    // Submitting a single-select Other moves on like a pick does.
    await act(async () => inputs()[0].props.onSubmitEditing());
    expect(text()).toContain("Which colors?");
    await press(optionPressable("Green"));
    await press(pressable("Next question"));
    await press(pressable("Send answer"));
    expect(payloads).toEqual([
      { call_id: "toolu_1", answers: [{ selected: [], other: "Mango; ripe" }, { selected: [1] }] },
    ]);
    // Picking a listed option afterwards replaces Other.
    await press(tab("Fruit"));
    await press(optionPressable("Apple"));
    expect(inputs().length).toBe(1);
    await act(async () => renderer.unmount());
  });

  test("shows the daemon's refusal and keeps the draft for retry", async () => {
    let attempts = 0;
    const { renderer, pressable, optionPressable, press, text } = render(true, async () => {
      attempts += 1;
      throw new Error("The Terminal is not showing this question.");
    });
    await press(optionPressable("Apple"));
    await press(optionPressable("Green"));
    await press(pressable("Next question"));
    await press(pressable("Send answer"));
    expect(text()).toContain("The Terminal is not showing this question.");
    expect(pressable("Send answer").props.disabled).toBe(false);
    await press(pressable("Send answer"));
    expect(attempts).toBe(2);
    await act(async () => renderer.unmount());
  });

  test("is read-only until the live pane shows the prompt, but can be browsed", async () => {
    const { renderer, pressable, optionPressable, tab, press, text } = render(false, async () => {});
    expect(optionPressable("Apple").props.disabled).toBe(true);
    expect(text()).toContain("Waiting for the prompt to appear in the Terminal");
    expect(text()).not.toContain("Open Terminal");
    await press(tab("Review"));
    expect(text()).toContain("Review your answers");
    expect(pressable("Answer 2 more").props.disabled).toBe(false);
    await act(async () => renderer.unmount());
  });

  test("a new tool call starts over at the first question", async () => {
    const { renderer, optionPressable, press, text } = render(true, async () => {});
    await press(optionPressable("Apple"));
    expect(text()).toContain("Which colors?");
    await act(async () => {
      renderer.update(
        <ClaudeChoiceCard callId="toolu_2" choice={choice} live chrome={chrome} onSubmit={async () => {}} />,
      );
    });
    expect(text()).toContain("Which fruit?");
    await act(async () => renderer.unmount());
  });

  test("caps its height when given a budget so it scrolls instead of passing under the header", async () => {
    const { renderer } = render(true, async () => {});
    expect(JSON.stringify(renderer.toJSON())).not.toContain("maxHeight");
    await act(async () => {
      renderer.update(
        <ClaudeChoiceCard callId="toolu_1" choice={choice} live chrome={chrome} maxHeight={321} onSubmit={async () => {}} />,
      );
    });
    expect(JSON.stringify(renderer.toJSON())).toContain('"maxHeight":321');
    await act(async () => renderer.unmount());
  });
}
