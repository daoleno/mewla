import { safeArea, Text, View } from "nsh";
import type { WaitForce } from "../../core/motion/wait";
import { showMenu } from "../kit/Menu";
import { canForceWait, forcedWait, forceWait } from "../kit/wait";

const NAMES: Record<WaitForce, string> = { slow: "Slow", none: "No answer", error: "Error", offline: "Offline" };

/**
 * Dev builds only: force a wait state (SPEC §16.4's columns) for whatever is in front, so the rare ones can be
 * felt on the phone. Every wait reads again in it: slow (answers held 5 s), no answer (failed at 15 s), error
 * (failed at once), offline (connecting, Offline at 10 s; back on Off). Reached from Brain's ⋯ (Wait states);
 * while one is forced, the chip at the bottom left names it and changes it from any page.
 */
export async function pickForcedWait(): Promise<void> {
  if (!canForceWait()) return;
  const now = forcedWait();
  const picked = await showMenu({
    title: "Force a wait state (dev)",
    items: [
      ...(Object.keys(NAMES) as WaitForce[]).map((id) => ({ id, label: NAMES[id], checked: now === id })),
      { id: "off", label: "Off", checked: now === null },
    ],
  });
  if (picked === null) return;
  forceWait(picked === "off" ? null : (picked as WaitForce));
}

/** The chip while a state is forced (dev builds only): it names the state; a tap changes it or turns it off. */
export function DevWaitChip() {
  return (
    <If when={canForceWait() && forcedWait() !== null}>
      <View testID="dev-wait-chip" role="button" accessibilityLabel={`Forced wait: ${NAMES[forcedWait() ?? "slow"]}`} onPress={() => void pickForcedWait()}
        class="absolute left-3 z-50 rounded-full bg-foreground px-3 py-1.5 active:opacity-70" style={{ bottom: safeArea().bottom + 72 }}>
        <Text class="text-caption font-semibold text-background">{`Dev · ${NAMES[forcedWait() ?? "slow"]}`}</Text>
      </View>
    </If>
  );
}
