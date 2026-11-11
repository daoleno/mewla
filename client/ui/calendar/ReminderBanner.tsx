import { effect, host, lifecycle, signal, Storage, Text, View } from "nsh";
import { Button } from "nsh/base";
import { askForReminders, reminderPermission, type PushHost, type ReminderPermission } from "../../core/push/registration";

const permission = signal<ReminderPermission>("granted");

const pushHost: PushHost = {
  permission: () => host.call<{ granted: boolean }>("push.permission"),
  requestPermission: () => host.call<boolean>("push.requestPermission", undefined, { timeout: 0 }),
  deviceToken: () => Promise.reject(new Error("not used here")),
};

/** Set by the notifications module: a grant here registers this phone for pushes now, not on the next connection. */
export const reminderLinks = { granted: () => {} };

/** Reads the permission again: after the first connection's prompt, or a return from Settings. */
export function refreshReminders(): void {
  if (!host.available()) return;
  void reminderPermission(pushHost, Storage).then((value) => {
    if (value === "granted" && permission() !== "granted") reminderLinks.granted();
    permission.set(value);
  });
}

/** At start, beside the other notification effects: checked again whenever the app comes back (the person may have changed it in Settings). */
export function startReminderBanner(): void {
  if (!host.available()) return;
  effect(() => {
    if (lifecycle() === "active") refreshReminders();
  });
}

const failure = signal("");

/** Calendar's banner while the phone cannot remind: Enable asks, Settings leads where a no is undone. */
export function ReminderBanner() {
  const enable = async () => {
    const value = await askForReminders(pushHost, Storage);
    permission.set(value);
    if (value === "granted") reminderLinks.granted();
  };
  const settings = () => {
    failure.set("");
    void host.call("push.openSettings").catch((error) => failure.set(error instanceof Error ? error.message : "Calendar action failed."));
  };
  const title = () => (permission() === "undetermined" ? "Reminders not enabled" : permission() === "denied" ? "Reminders disabled" : "Reminders unavailable");
  return (
    <View>{permission() === "granted" ? null : (
    <View testID="reminder-banner" class="mx-4 mt-3 gap-2 rounded-xl border border-border bg-card px-4 py-3">
      <View class="flex-row items-center gap-3">
        <Text class="flex-1 text-base font-semibold">{title()}</Text>
        {permission() === "undetermined" ? (
          <Button testID="enable-reminders" accessibilityLabel="Enable reminders" onPress={() => void enable()} class="rounded-full bg-primary px-4 py-1.5">
            <Text class="text-base font-semibold text-primary-foreground">Enable</Text>
          </Button>
        ) : permission() === "denied" ? (
          <Button testID="reminder-settings" accessibilityLabel="Open notification settings" onPress={settings} class="rounded-full border border-border px-4 py-1.5 active:bg-accent">
            <Text class="text-base">Settings</Text>
          </Button>
        ) : null}
      </View>
      {failure() ? <Text class="text-sm text-destructive">{failure()}</Text> : null}
    </View>
    )}</View>
  );
}
