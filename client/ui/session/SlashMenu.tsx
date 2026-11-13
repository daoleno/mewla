import { Scroll, Text, View } from "nsh";
import { Button } from "nsh/base";
import { slashCommandEmptyText, slashCommandIcon, type CodexSlashCommand } from "../../core/session/codexSlashCommands";
import { groupSlashCommands, slashMenuState, type ComposerAlert } from "../../core/session/composerSlash";
import type { SlashCommandAlertButton } from "../../core/session/slashCommandRouter";
import type { AgentKind } from "../../core/sessions/workerPresentation";
import { ICON_ASSETS } from "../icons/assets";
import { Icon, type IconName } from "../icons/Icon";
import { showAlert } from "../shell/alert";

export {
  insertSkillMention,
  routeComposerPick,
  routeComposerSend,
  slashMenuState,
  type ComposerAlert,
  type ComposerSendRoute,
  type ComposerSlashContext,
} from "../../core/session/composerSlash";

function commandIcon(name: string): IconName {
  const icon = slashCommandIcon(name);
  return icon in ICON_ASSETS ? (icon as IconName) : "terminal";
}

/**
 * The menu over the composer: with the "+" button pinned, Attach File and the
 * slash commands by group; while the draft is a `/word` (a Codex Session), the
 * commands it matches. A normal draft hides the commands. Nothing shows while
 * disconnected or with nothing to show. A tap calls `onPick`; the Session
 * routes it (`routeComposerPick`) and unpins.
 */
export function SlashMenu(props: {
  text: () => string;
  /** From `chatSlashCommands`; empty for other agents. */
  commands: () => CodexSlashCommand[];
  /** The list could not be loaded (its empty line says so). */
  commandsUnavailable?: () => boolean;
  agentKind: () => AgentKind;
  connected: () => boolean;
  /** The "+" button is on. */
  pinned: () => boolean;
  onPick: (command: CodexSlashCommand) => void;
  /** Attach File: picks files for the next message; absent where a composer can't attach. */
  onAttach?: () => void;
  uploading?: () => boolean;
}) {
  const state = () => slashMenuState({ text: props.text(), agentKind: props.agentKind(), commands: props.commands(), connected: props.connected(), pinned: props.pinned() });
  return (
    <View>
      {state().open ? (
        <View testID="slash-menu" class="mb-2 overflow-hidden rounded-2xl border border-border bg-card">
          <Scroll keyboardShouldPersistTaps="handled" style={{ maxHeight: 360 }}>
            <View class="gap-2 p-2">
              {state().showActions ? (
                <Button testID="attach-file" accessibilityLabel="Upload file" disabled={!props.onAttach || !props.connected() || Boolean(props.uploading?.())} onPress={() => props.onAttach?.()}
                  class={props.onAttach && props.connected() ? "flex-row items-center gap-3 rounded-xl px-2 py-2 active:bg-accent" : "flex-row items-center gap-3 rounded-xl px-2 py-2 opacity-50"}>
                  <Icon name={props.uploading?.() ? "cloud-upload" : "attach"} size={20} color={props.onAttach && props.connected() ? "primary" : undefined} />
                  <View class="flex-1 gap-0.5">
                    <Text class="text-base">Attach File</Text>
                    <Text class="text-xs text-muted-foreground">{props.uploading?.() ? "Uploading..." : "Add a file to this message"}</Text>
                  </View>
                  <Icon name="chevron-right" size={16} />
                </Button>
              ) : null}
              {state().showCommandList ? (
                state().visibleCommands.length > 0 ? (
                  groupSlashCommands(state().visibleCommands).map((group) => (
                    <View testID={`slash-group-${group.key}`} class="gap-0.5">
                      <View class="px-2 pt-1"><Text class="text-xs font-semibold uppercase text-muted-foreground">{group.label}</Text></View>
                      {group.commands.map((command) => (
                        <Button testID={`slash-${command.name}`} accessibilityLabel={`${command.value} ${command.description}`} onPress={() => props.onPick(command)}
                          class="flex-row items-center gap-3 rounded-xl px-2 py-2 active:bg-accent">
                          <Icon name={commandIcon(command.name)} size={20} color="primary" />
                          <View class="flex-1 gap-0.5">
                            <Text class="font-mono text-base">{command.value}</Text>
                            {command.description ? <Text class="text-xs text-muted-foreground line-clamp-2">{command.description}</Text> : null}
                          </View>
                          {state().commandQuery === command.value ? <Icon name="check" size={18} color="primary" /> : null}
                        </Button>
                      ))}
                    </View>
                  ))
                ) : (
                  <View testID="slash-empty" class="flex-row items-center gap-2 px-2 py-3">
                    <Icon name="search" size={16} />
                    <Text class="text-sm text-muted-foreground">{slashCommandEmptyText(Boolean(props.commandsUnavailable?.()))}</Text>
                  </View>
                )
              ) : null}
            </View>
          </Scroll>
        </View>
      ) : null}
    </View>
  );
}

/** The composer's "+" button: toggles the menu; off while disconnected. */
export function ComposerPlusButton(props: { pinned: () => boolean; connected: () => boolean; onPress: () => void }) {
  return (
    <Button testID="composer-plus" accessibilityLabel={props.pinned() ? "Close actions" : "Open actions"} disabled={!props.connected()} onPress={props.onPress}
      class="h-[52px] w-11 items-center justify-center active:opacity-60">
      <Icon name={props.pinned() ? "close" : "add"} size={22} color="muted-foreground" />
    </Button>
  );
}

/** Shows a route's alert and settles with the button pressed (`cancel` when it closed without one). */
export async function showComposerAlert(alert: ComposerAlert): Promise<SlashCommandAlertButton["action"]> {
  const result = await showAlert({
    title: alert.title,
    description: alert.message,
    actions: alert.buttons.map((button) => ({ id: button.action, label: button.label, role: button.action === "cancel" ? "cancel" : "default" })),
  });
  const chosen = alert.buttons.find((button) => button.action === result.action);
  return chosen?.action ?? "cancel";
}
