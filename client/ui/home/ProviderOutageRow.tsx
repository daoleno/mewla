import { computed, Text, View } from "nsh";
import { providerOutageLine } from "../../core/brain";
import { Icon } from "../kit/Icon";
import { brain, connection } from "../session";

/** The line at the foot of Brain's Chat while a model provider refuses requests (core/brain providerOutageLine). */
export const providerOutage = computed(() => (connection() === "connected" ? providerOutageLine(brain()) : null));

/** "Claude hasn't answered since 07:03 (rate limit). Work is held, not lost." It clears by itself. */
export function ProviderOutageRow() {
  return (
    <If when={providerOutage()}>
      <View class="px-4">
        <View testID="chat-provider-outage" class="min-h-[44px] flex-row items-center gap-3.5 rounded-(--radius-group) bg-card px-4 py-3">
          <Icon name="cloud-offline" size={22} tone="soft" />
          <Text testID="chat-provider-outage-line" class="flex-1 text-body">{providerOutage()}</Text>
        </View>
      </View>
    </If>
  );
}
