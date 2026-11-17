import { computed, signal, Text, View } from "nsh";
import { chatUpdateRow } from "../../core/update/chatRow";
import { installDaemonUpdate, restartComputerTerminals } from "../../core/settings/computer";
import { PillButton } from "../kit/kit";
import { Icon } from "../kit/Icon";
import { computerUpdate } from "../computer/state";
import { connection, current } from "../session";

/** The row at the foot of Brain's Chat about the current computer's Mewla (core/update/chatRow), while connected. */
export const chatUpdate = computed(() => (connection() === "connected" ? chatUpdateRow(computerUpdate()) : null));

const busy = signal(false);
const failed = signal("");

async function act(): Promise<void> {
  const server = current();
  const row = chatUpdate();
  if (!server || !row || busy()) return;
  busy.set(true);
  failed.set("");
  try {
    const next = row.action === "update" ? await installDaemonUpdate(server) : await restartComputerTerminals(server);
    if (current()?.id !== server.id) return;
    computerUpdate.set(row.action === "update" ? { ...next, updating: true } : { ...next, terminals_restart: false });
  } catch (error) {
    failed.set(error instanceof Error && error.message ? error.message : "Couldn't reach the computer.");
  } finally {
    busy.set(false);
  }
}

/** "Update to 0.4.15 waiting for a model reply" · Update now; "Restart terminals to finish updating" · Restart. */
export function ChatUpdateRow() {
  return (
    <If when={chatUpdate()}>
      <View class="px-4">
      <View testID="chat-update-row" class="min-h-[56px] gap-2 rounded-(--radius-group) bg-card px-4 py-3">
        <View class="flex-row items-center gap-3.5">
          <Icon name="cloud-download" size={22} tone="soft" />
          <View class="flex-1">
            <Text testID="chat-update-title" class="text-body font-semibold">{chatUpdate()!.title}</Text>
            <If when={chatUpdate()!.detail}><Text testID="chat-update-detail" class="mt-0.5 text-sub text-muted-foreground">{chatUpdate()!.detail}</Text></If>
          </View>
          <PillButton testID="chat-update-action" kind="ink" small busy={busy()} disabled={busy()} title={chatUpdate()!.button} onPress={() => void act()} />
        </View>
        <If when={failed()}><Text testID="chat-update-error" class="pl-[36px] text-sub text-foreground">{failed()}</Text></If>
      </View>
      </View>
    </If>
  );
}
