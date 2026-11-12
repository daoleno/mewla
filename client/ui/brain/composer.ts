import { cleanup, computed, effect, signal, untracked } from "nsh";
import { brainExecutorChoices } from "../../core/brainMenu";
import type { CodexSlashCommand } from "../../core/session/codexSlashCommands";
import { routeComposerPick, routeComposerSend, slashMenuState, type ComposerAlert, type ComposerSendRoute } from "../../core/session/composerSlash";
import { chatSlashCommands } from "../../core/session/sessionRequests";
import { agentKindFromProvider } from "../../core/sessions/workerPresentation";
import type { ComposerText } from "../chat/Composer";
import { createAttachmentUploads } from "../more/files/Pages";
import { insertSkillMention, showComposerAlert } from "../session/SlashMenu";
import { mentionPanelOpen } from "./MentionPanel";
import { currentBrain, type BrainSource } from "./source";

/**
 * What Brain's composer does besides typing (soft-final/SPEC.md §1.4): attach
 * (the files go with the next message), `@` (the agents, `MentionPanel`) and, when
 * Brain runs on Codex, `/` (its slash commands: `SlashMenu` with `commands`,
 * routed as a Session routes them). Make one inside the component that holds the
 * composer; it reads the current computer's Brain (`source`, `currentBrain` by default).
 *
 * The host renders: the Composer with `send` as `onSend`, `pickerOpen`, `canSend`
 * and the "+" (`pinned`); SlashMenu (`onAttach: attach`, `onPick: pick`);
 * AttachmentTray (`uploads`); MentionPanel (`executors`); CodexStatusSheet and
 * CodexSkillsSheet on `statusOpen` / `skillsOpen` (a skill pick: `insertSkill`).
 */
export function createBrainComposer(options: {
  text: ComposerText;
  /** A slash command that only runs in the terminal: Brain's own Session on its terminal. */
  onOpenTerminal?: (hostWorkerId: string) => void;
  /** A message to show (an attach that failed). */
  onNotice?: (text: string) => void;
  source?: BrainSource;
}) {
  const brain = options.source ?? currentBrain;
  const source = {
    ...brain,
    text: options.text,
    onNotice: options.onNotice,
    onOpenTerminal: options.onOpenTerminal ? () => options.onOpenTerminal?.(brain.snapshot()?.host_worker?.id ?? "") : undefined,
  };
  const pinned = signal(false);
  const statusOpen = signal(false);
  const skillsOpen = signal(false);
  const commands = signal<CodexSlashCommand[]>([]);
  const commandsUnavailable = signal(false);
  const kind = computed(() => agentKindFromProvider(source.snapshot()?.host_worker?.provider ?? source.snapshot()?.host_executor?.provider));
  const executors = computed(() => brainExecutorChoices(source.snapshot()?.executors ?? [], source.snapshot()?.executor_states).choices);

  const uploads = createAttachmentUploads();
  cleanup(() => uploads.dispose());
  const uploading = () => uploads.items().some((item) => item.status === "uploading" || item.status === "queued");

  // Codex's slash commands, while Brain runs on Codex.
  effect(() => {
    if (kind() !== "codex" || !source.connected()) return commands.set([]);
    const transport = untracked(source.transport);
    if (!transport) return;
    untracked(() => void chatSlashCommands(transport)
      .then((list) => {
        commands.set(list);
        commandsUnavailable.set(false);
      })
      .catch(() => commandsUnavailable.set(true)));
  });

  const answerAlert = async (alert: ComposerAlert) => {
    const action = await showComposerAlert(alert);
    if (action === "open_terminal") source.onOpenTerminal?.();
    else if (action === "send_as_message") {
      const value = untracked(source.text.text);
      if (!value.trim()) return;
      source.chat()?.send(value);
      source.text.clear();
    }
  };
  /** Does what a routed send or pick asks; true when the composer's text went out. */
  const follow = (route: ComposerSendRoute, fromDraft: boolean): boolean => {
    pinned.set(false);
    switch (route.kind) {
      case "send":
        source.chat()?.send(route.text, uploads.ready());
        uploads.clear();
        return fromDraft;
      case "prefill":
        source.text.set(route.text);
        if (route.alert) void answerAlert(route.alert);
        return false;
      case "alert":
        void answerAlert(route);
        return false;
      case "status":
        source.text.clear();
        statusOpen.set(true);
        return false;
      case "skills":
        source.text.clear();
        skillsOpen.set(true);
        return false;
    }
  };

  return {
    pinned,
    statusOpen,
    skillsOpen,
    kind,
    commands: () => commands(),
    commandsUnavailable: () => commandsUnavailable(),
    executors: () => executors(),
    uploads,
    uploading,
    /** Send waits while a file uploads, and needs Brain's chat. */
    canSend: () => Boolean(source.chat()) && !uploading(),
    /** A list is open over the field (`/` commands or `@` agents): Return waits for a pick. */
    pickerOpen: () => {
      const text = source.text.text();
      return slashMenuState({ text, agentKind: kind(), commands: commands(), connected: source.connected(), pinned: false }).open || mentionPanelOpen(text, executors());
    },
    attach: () => {
      pinned.set(false);
      void uploads.pickAndUpload().catch((error) => source.onNotice?.(error instanceof Error ? error.message : "Could not attach the file."));
    },
    /** The Composer's `onSend`: false keeps the text. */
    send: (value: string): void | false => {
      const route = routeComposerSend(value, { agentKind: untracked(kind), slashCommands: untracked(commands), canOpenTerminal: Boolean(source.onOpenTerminal) });
      return follow(route, true) ? undefined : false;
    },
    /** SlashMenu's `onPick`. */
    pick: (command: CodexSlashCommand) => {
      // A command picked while the draft is that `/word` replaces the draft; otherwise the draft stays.
      const typingCommand = untracked(source.text.text).trim().startsWith("/");
      if (follow(routeComposerPick(command, { canOpenTerminal: Boolean(source.onOpenTerminal) }), false) && typingCommand) source.text.clear();
    },
    /** CodexSkillsSheet's `onPick`. */
    insertSkill: (name: string) => source.text.set(insertSkillMention(untracked(source.text.text), name)),
  };
}

export type BrainComposer = ReturnType<typeof createBrainComposer>;
