import { expect, mock, test } from "bun:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";

if (!process.env.MEWLA_MESSAGE_ACTIONS_TEST) {
  test("message edit flow in isolation", () => {
    const result = Bun.spawnSync([process.execPath, "test", import.meta.filename], {
      env: { ...process.env, MEWLA_MESSAGE_ACTIONS_TEST: "1" },
    });
    if (result.exitCode) {
      throw new Error(new TextDecoder().decode(result.stdout) + new TextDecoder().decode(result.stderr));
    }
    expect(result.exitCode).toBe(0);
  });
} else {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  const calls: string[] = [];
  let rewindImpl: () => Promise<{ composerClean: boolean }> = async () => ({ composerClean: true });
  mock.module("../../services/websocket", () => ({
    wsClient: {
      rewindMessage: (serverId: string, workerId: string, eventId: string) => {
        calls.push(`rewind ${serverId} ${workerId} ${eventId}`);
        return rewindImpl();
      },
    },
  }));
  mock.module("./MessageEditChip", () => ({ MessageEditChip: "MessageEditChip" }));
  const { useInterfaceMessageActions } = await import("./useInterfaceMessageActions");

  type Props = Partial<Parameters<typeof useInterfaceMessageActions>[0]>;
  const subject = {
    id: "u2",
    role: "user" as const,
    body: "ship it",
    hasAttachments: false,
    pending: false,
    streaming: false,
  };

  function mount(initial: Props = {}) {
    let draft = "";
    let current: ReturnType<typeof useInterfaceMessageActions>;
    const props = (): Parameters<typeof useInterfaceMessageActions>[0] => ({
      serverId: "srv",
      workerId: "%7",
      connected: true,
      running: false,
      messageRewind: true,
      replyIds: new Set<string>(),
      desktop: false,
      chrome: {} as any,
      draft,
      setDraft: (value: string) => {
        calls.push(`draft ${JSON.stringify(value)}`);
        draft = value;
        render();
      },
      focusComposer: () => calls.push("focus"),
      sendDraft: () => {
        calls.push(`send ${JSON.stringify(draft)}`);
        draft = "";
      },
      ...initial,
    });
    function Harness() {
      current = useInterfaceMessageActions(props());
      return null;
    }
    let renderer!: TestRenderer.ReactTestRenderer;
    const render = () => act(() => renderer.update(React.createElement(Harness)));
    act(() => {
      renderer = TestRenderer.create(React.createElement(Harness));
    });
    return {
      get hook() {
        return current!;
      },
      type(value: string) {
        draft = value;
        render();
      },
      get draft() {
        return draft;
      },
      rerender(next: Props) {
        Object.assign(initial, next);
        render();
      },
    };
  }

  const reset = () => {
    calls.length = 0;
    rewindImpl = async () => ({ composerClean: true });
  };

  test("editing a message on a rewindable agent rewinds first, then sends the edited text", async () => {
    reset();
    const view = mount();
    act(() => view.hook.onEdit(subject));
    expect(view.draft).toBe("ship it");
    expect(view.hook.accessory).not.toBeNull();
    calls.length = 0;

    view.type("ship it tomorrow");
    await act(async () => view.hook.sendEdited());
    expect(calls).toEqual(["rewind srv %7 u2", 'send "ship it tomorrow"']);
    expect(view.hook.accessory).toBeNull();
  });

  test("an agent that cannot rewind sends the edit as a new message and never rewinds", async () => {
    reset();
    const view = mount({ messageRewind: false });
    act(() => view.hook.onEdit(subject));
    view.type("ship it again");
    calls.length = 0;
    await act(async () => view.hook.sendEdited());
    expect(calls).toEqual(['send "ship it again"']);
  });

  test("Brain's thread is never rewound even when the host advertises it", async () => {
    reset();
    const view = mount({ conversationScopeKey: "brain-thread:abc", messageRewind: true });
    expect(view.hook.environment.brain).toBe(true);
    act(() => view.hook.onEdit(subject));
    view.type("different");
    calls.length = 0;
    await act(async () => view.hook.sendEdited());
    expect(calls).toEqual(['send "different"']);
  });

  test("a failed rewind sends nothing and leaves an honest copy to confirm", async () => {
    reset();
    rewindImpl = async () => {
      throw new Error("Claude highlighted a different message.");
    };
    const view = mount();
    act(() => view.hook.onEdit(subject));
    view.type("ship it tomorrow");
    calls.length = 0;
    await act(async () => view.hook.sendEdited());
    expect(calls).toEqual(["rewind srv %7 u2"]);
    expect((view.hook.accessory as any).props).toMatchObject({
      error: "Claude highlighted a different message.",
    });
    calls.length = 0;
    await act(async () => view.hook.sendEdited());
    expect(calls).toEqual(['send "ship it tomorrow"']);
  });

  test("a rewind that left the agent's input box dirty sends nothing and is never repeated", async () => {
    reset();
    rewindImpl = async () => ({ composerClean: false });
    const view = mount();
    act(() => view.hook.onEdit(subject));
    view.type("ship it tomorrow");
    calls.length = 0;
    await act(async () => view.hook.sendEdited());
    expect(calls).toEqual(["rewind srv %7 u2"]);
    expect((view.hook.accessory as any).props.error).toContain("input box");
    // After the person clears the box, the same send goes out without a second rewind.
    calls.length = 0;
    await act(async () => view.hook.sendEdited());
    expect(calls).toEqual(['send "ship it tomorrow"']);
    expect(view.hook.accessory).toBeNull();
  });

  test("sending twice while the rewind is in flight rewinds once", async () => {
    reset();
    let release!: () => void;
    rewindImpl = () => new Promise((resolve) => (release = () => resolve({ composerClean: true })));
    const view = mount();
    act(() => view.hook.onEdit(subject));
    view.type("ship it tomorrow");
    calls.length = 0;
    await act(async () => {
      void view.hook.sendEdited();
      void view.hook.sendEdited();
    });
    expect(view.hook.editBlocksSend).toBe(true);
    await act(async () => release());
    expect(calls.filter((c) => c.startsWith("rewind"))).toHaveLength(1);
    expect(calls.filter((c) => c.startsWith("send"))).toHaveLength(1);
  });

  test("cancelling an edit gives back the draft that was in the composer", () => {
    reset();
    const view = mount();
    view.type("half-written");
    act(() => view.hook.onEdit(subject));
    expect(view.draft).toBe("ship it");
    act(() => view.hook.accessory && (view.hook.accessory as any).props.onCancel());
    expect(view.draft).toBe("half-written");
    expect(view.hook.accessory).toBeNull();
  });

  test("sending an edit unchanged rewinds and sends the same text", async () => {
    reset();
    const view = mount();
    act(() => view.hook.onEdit(subject));
    calls.length = 0;
    await act(async () => view.hook.sendEdited());
    expect(calls).toEqual(["rewind srv %7 u2", 'send "ship it"']);
  });

  test("switching conversation drops the edit", () => {
    reset();
    const view = mount();
    act(() => view.hook.onEdit(subject));
    expect(view.hook.accessory).not.toBeNull();
    view.rerender({ workerId: "%9" });
    expect(view.hook.accessory).toBeNull();
  });
}
