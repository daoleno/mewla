import { Clipboard, cleanup, effect, lifecycle, openUrl, signal, Text, untracked, View } from "nsh";
import { Button } from "nsh/base";
import { formatFullDateTime } from "../../../core/more/dates";
import { failureText } from "../../../core/more/daemon";
import { beginTelegramBinding, configureTelegram, telegramLabel, telegramRecipient, telegramRequest, telegramStep, type TelegramAction } from "../../../core/settings/telegram";
import { connected, daemon } from "../daemon";
import { Icon } from "../kit/Icon";
import { confirmDestructive, EmptyState, Field, Notice, Page, PillButton, Row, Section, Spinner } from "../kit/kit";
import { loadTelegram, setTelegram, telegram } from "./telegram";

const POLL_MS = 5_000;

/** Telegram: one bot per computer, Brain in its private chat. The token never stays on the phone. */
export function Telegram() {
  const token = signal("");
  const replacing = signal(false);
  const busy = signal("");
  const failure = signal("");
  const advanced = signal(false);
  const copied = signal(false);

  // Read now, then every five seconds while the page is open and the app in front.
  let timer: ReturnType<typeof setInterval> | null = null;
  effect(() => {
    if (timer) clearInterval(timer);
    timer = null;
    if (!daemon() || !connected() || lifecycle() !== "active") {
      untracked(() => token.set(""));
      return;
    }
    untracked(loadTelegram);
    timer = setInterval(loadTelegram, POLL_MS);
  });
  cleanup(() => {
    if (timer) clearInterval(timer);
    token.set("");
  });

  const status = () => (telegram().serverId === daemon()?.server.id ? telegram().status : null);
  const step = () => (replacing() ? "token" : telegramStep(status()));

  async function act(name: string, work: () => Promise<void>): Promise<void> {
    if (busy()) return;
    busy.set(name);
    failure.set("");
    try {
      await work();
    } catch (error) {
      failure.set(failureText(error, "The connection could not be updated."));
    } finally {
      busy.set("");
    }
  }

  const run = (action: TelegramAction) => act(action, async () => setTelegram(await telegramRequest(daemon()!, action)));

  const verify = () =>
    act("verify", async () => {
      const value = token().trim();
      token.set("");
      if (!value) throw new Error("Enter the token issued by BotFather.");
      try {
        setTelegram(await configureTelegram(daemon()!, value));
        replacing.set(false);
      } catch {
        throw new Error("The bot token could not be verified.");
      }
    });

  const connect = () =>
    act("connect", async () => {
      if (!status()?.enabled) setTelegram(await telegramRequest(daemon()!, "enable"));
      const challenge = await beginTelegramBinding(daemon()!);
      await openUrl(challenge.url);
      loadTelegram();
    });

  const openChat = () =>
    act("open", async () => {
      const result = await openUrl(`https://t.me/${status()!.bot_username}`);
      if (!result.opened) throw new Error("The bot chat could not be opened.");
    });

  return (
    <Page title="Telegram" testID="telegram">
      {!daemon() ? (
        <EmptyState icon="paper-plane" title="No current server" detail="Pair a computer in Settings to connect Telegram." />
      ) : !connected() && !status() ? (
        <EmptyState icon="paper-plane" title="Server offline" detail="Reconnect the current server in Settings." />
      ) : (
        <View>
          <View class="mt-4 flex-row items-center gap-3 px-1">
            <Icon name="paper-plane" size={28} tone="ink" />
            <View class="flex-1">
              <Text class="text-xl font-semibold">{status()?.bot_username ? `@${status()!.bot_username}` : "Telegram"}</Text>
              <View class="flex-row items-center gap-1.5">
                {telegram().loading || busy() ? <Spinner size={12} /> : null}
                <Text testID="telegram-state" class="text-sm text-muted-foreground">{telegramLabel(status(), connected(), telegram().loading)}</Text>
              </View>
              {status()?.owner_hint ? <Text class="text-xs text-muted-foreground">{status()!.owner_hint}</Text> : null}
            </View>
          </View>
          {failure() || status()?.last_error || telegram().error ? (
            <Notice tone="failed" title="Telegram problem" detail={failure() || status()?.last_error || telegram().error} />
          ) : null}

          {step() === "token" ? (
            <View>
              <Section title="Bot token" footer="Bot chats are stored by Telegram. Your token stays on the connected server.">
                <Field
                  testID="telegram-token"
                  value={token()}
                  masked
                  placeholder="Paste the BotFather token"
                  onChange={(value) => token.set(value)}
                  onSubmit={verify}
                  trailing={
                    <Button accessibilityLabel="Paste token" onPress={() => void Clipboard.readText().then((text) => token.set(text.trim())).catch(() => failure.set("Clipboard unavailable"))} class="p-1 active:opacity-60">
                      <Icon name="clipboard" size={18} />
                    </Button>
                  }
                />
              </Section>
              <View class="mt-3 flex-row gap-3">
                <PillButton testID="telegram-verify" filled title={busy() === "verify" ? "Verifying" : "Verify token"} busy={busy() === "verify"} disabled={!token().trim()} onPress={verify} />
                {replacing() ? (
                  <PillButton title="Cancel" onPress={() => { token.set(""); replacing.set(false); }} />
                ) : (
                  <PillButton title="BotFather" onPress={() => void openUrl("https://t.me/BotFather")} />
                )}
              </View>
            </View>
          ) : (
            <View>
              <View class="mt-3">
                {step() === "connect" || step() === "open-link" ? (
                  <PillButton testID="telegram-connect" filled title={step() === "open-link" ? "Open connection link" : "Connect Telegram"} busy={busy() === "connect"} onPress={connect} />
                ) : step() === "reconnect" ? (
                  <PillButton testID="telegram-reconnect" filled title="Reconnect" busy={busy() === "enable"} onPress={() => run("enable")} />
                ) : (
                  <PillButton testID="telegram-open" filled title="Open Telegram" busy={busy() === "open"} onPress={openChat} />
                )}
              </View>
              <Section>
                <Row title="Status" value={telegramLabel(status(), connected(), false)} />
                {status()?.owner_hint ? <Row title="Recipient" value={telegramRecipient(status()!)} /> : null}
                {status()?.owner_hint && status()?.enabled ? <Row testID="telegram-disconnect" title="Disconnect" onPress={() => run("disable")} disabled={Boolean(busy())} /> : null}
              </Section>
              {connected() ? (
                <Section>
                  <Row testID="telegram-advanced" title="Advanced" trailing={<Icon name={advanced() ? "chevron-up" : "chevron-down"} size={16} tone="faint" />} onPress={() => advanced.set(!advanced())} />
                  {advanced() ? (
                    <View>
                      <Row title="Chat mode" value={status()?.topics_available ? "Native topics" : "Private chat"} />
                      {status()?.topic_mappings !== undefined ? <Row title="Session topics" value={String(status()!.topic_mappings)} /> : null}
                      {status()?.brain_thread_id ? (
                        <Row
                          title="Brain conversation"
                          value={copied() ? "Copied" : status()!.brain_thread_id}
                          onPress={() => void Clipboard.writeText(status()!.brain_thread_id!).then(() => { copied.set(true); setTimeout(() => copied.set(false), 1_600); })}
                        />
                      ) : null}
                      {status()?.brain_topic_id !== undefined ? <Row title="Brain topic" value={String(status()!.brain_topic_id)} /> : null}
                      <Row title="Delivery checks" value={String((status()?.ambiguous_delivery_count ?? 0) + (status()?.topic_ambiguous_ops_count ?? 0))} />
                      {status()?.last_receive_at ? <Row title="Last received" value={formatFullDateTime(status()!.last_receive_at!)} /> : null}
                      {status()?.last_send_at ? <Row title="Last sent" value={formatFullDateTime(status()!.last_send_at!)} /> : null}
                      <Row title="Refresh" onPress={loadTelegram} />
                      <Row title="Replace token" onPress={() => replacing.set(true)} />
                      {status()?.owner_hint ? (
                        <Row
                          destructive
                          title="Unlink account"
                          onPress={() => void confirmDestructive("Unlink Telegram account", "Remove the verified Telegram owner and require a new binding?", "Unlink").then((yes) => { if (yes) void run("revoke"); })}
                        />
                      ) : null}
                      <Row
                        destructive
                        title="Remove bot"
                        onPress={() => void confirmDestructive("Remove Telegram bot", "Remove this server's token, binding and delivery state? Telegram cloud messages are not deleted.", "Remove").then((yes) => { if (yes) void run("remove"); })}
                      />
                    </View>
                  ) : null}
                </Section>
              ) : null}
            </View>
          )}
        </View>
      )}
    </Page>
  );
}
