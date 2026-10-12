import { describe, expect, test, mock } from "bun:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";

// Keep the websocket mock out of the shared Bun process; execute the real hook.
if (!process.env.MEWLA_DRAFT_CARRY_CHILD) {
  test("draft carry behavioral scenarios", () => {
    const result = Bun.spawnSync(
      [process.execPath, "test", import.meta.filename],
      { env: { ...process.env, MEWLA_DRAFT_CARRY_CHILD: "1" } },
    );
    const output =
      new TextDecoder().decode(result.stdout) +
      new TextDecoder().decode(result.stderr);
    if (result.exitCode !== 0) throw new Error(output);
    expect(output).not.toContain("(fail)");
  });
} else {
  mock.module("../../services/websocket", () => ({
    wsClient: { subscribeCodexConversation: () => () => {} },
  }));
  const { useInterfaceChatSession } = await import("./InterfaceChatSession");

  type Session = ReturnType<typeof useInterfaceChatSession>;
  type Props = { serverId: string; workerId: string; scope?: string };
  function mountSession(initial: Props) {
    const latest: { current: Session | null } = { current: null };
    function Probe(props: Props) {
      latest.current = useInterfaceChatSession({
        serverId: props.serverId,
        workerId: props.workerId,
        conversationScopeKey: props.scope,
        connectionState: "connecting",
        screenFocused: true,
      });
      return null;
    }
    let renderer!: TestRenderer.ReactTestRenderer;
    act(() => {
      renderer = TestRenderer.create(<Probe {...initial} />);
    });
    return {
      session: () => latest.current!,
      update: (props: Props) =>
        act(() => {
          renderer.update(<Probe {...props} />);
        }),
      unmount: () => act(() => renderer.unmount()),
    };
  }

  describe("composer draft across chat identity", () => {
    test("a draft typed before Brain's first snapshot survives the thread binding", () => {
      const chat = mountSession({ serverId: "s1", workerId: "" });
      act(() => chat.session().setDraft("half a thought"));
      chat.update({ serverId: "s1", workerId: "host", scope: "brain-thread:t1" });
      expect(chat.session().draft).toBe("half a thought");
      chat.unmount();

      // The draft now belongs to the thread, not the unbound placeholder.
      const later = mountSession({ serverId: "s1", workerId: "host", scope: "brain-thread:t1" });
      expect(later.session().draft).toBe("half a thought");
      later.update({ serverId: "s1", workerId: "" });
      expect(later.session().draft).toBe("");
      later.unmount();
    });

    test("a bound chat's draft stays with its own thread", () => {
      const chat = mountSession({ serverId: "s2", workerId: "host", scope: "brain-thread:a" });
      act(() => chat.session().setDraft("for thread a"));
      chat.update({ serverId: "s2", workerId: "host", scope: "brain-thread:b" });
      expect(chat.session().draft).toBe("");
      chat.update({ serverId: "s2", workerId: "host", scope: "brain-thread:a" });
      expect(chat.session().draft).toBe("for thread a");
      chat.unmount();
    });
  });
}
