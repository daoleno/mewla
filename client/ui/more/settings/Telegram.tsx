import { Clipboard, cleanup, effect, openUrl, signal, Text, untracked, View, lifecycle } from "nsh";
import { Button } from "nsh/base";
import { formatFullDateTime } from "../../../core/more/dates";
import { failureText } from "../../../core/more/daemon";
import {
  beginTelegramBinding,
  configureTelegram,
  telegramLabel,
  telegramLabelTone,
  telegramRecipient,
  telegramRequest,
  telegramView,
  type TelegramAction,
} from "../../../core/settings/telegram";
import { connected, daemon } from "../daemon";
import { Icon } from "../../kit/Icon";
import { MarkView, StateLine } from "../../kit/RowTile";
import { confirmDestructive, EmptyState, Field, Notice, Page, PillButton, Row, Section, Spinner } from "../../kit/kit";
import { loadTelegram, setTelegram, telegram } from "./telegramState";

const POLL_MS = 5_000;
const BOTFATHER_URL = "https://t.me/BotFather";

/** A failed step, in the Expo app's words: what failed, then why. */
class Failure extends Error {
  constructor(readonly title: string, detail: string) {
    super(detail);
  }
}

/** Telegram: one bot per computer, Brain in its private chat. The token never stays on the phone. */
export function Telegram() {
  const token = signal("");
  const replacing = signal(false);
  const busy = signal("");
  const failure = signal<{ title: string; detail: string } | null>(null);
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
  const loadError = () => (telegram().serverId === daemon()?.server.id ? telegram().error : "");
  const view = () => telegramView({ status: status(), error: loadError(), replacing: replacing() });
  /** Nothing can be done while the computer is away, a first read runs, or a step runs. */
  const blocked = () => !connected() || Boolean(busy()) || (telegram().loading && !status());

  async function act(name: string, fallback: Failure, work: () => Promise<void>): Promise<void> {
    if (busy() || !connected()) return;
    busy.set(name);
    failure.set(null);
    try {
      await work();
    } catch (error) {
      token.set("");
      failure.set(error instanceof Failure ? { title: error.title, detail: error.message } : { title: fallback.title, detail: failureText(error, fallback.message) });
    } finally {
      busy.set("");
    }
  }

  const connectionFailure = () => new Failure("Telegram connection", "The connection could not be updated.");
  const run = (action: TelegramAction) => act(action, connectionFailure(), async () => setTelegram(await telegramRequest(daemon()!, action)));

  const verify = () => {
    const value = token().trim();
    token.set("");
    if (!value) return failure.set({ title: "Bot token required", detail: "Enter the token issued by BotFather." });
    void act("verify", new Failure("Telegram setup failed", "The bot token could not be verified."), async () => {
      setTelegram(await configureTelegram(daemon()!, value));
      replacing.set(false);
    });
  };

  const connect = () =>
    act("connect", new Failure("Telegram owner binding", "The private Telegram chat could not be opened."), async () => {
      if (!status()?.enabled) setTelegram(await telegramRequest(daemon()!, "enable"));
      const challenge = await beginTelegramBinding(daemon()!);
      const opened = await openUrl(challenge.url);
      if (!opened.opened) throw new Failure("Telegram owner binding", "The private Telegram chat could not be opened.");
      loadTelegram();
    });

  // Opening a chat needs no computer: the bot is Telegram's.
  const openChat = async () => {
    const result = await openUrl(`https://t.me/${status()!.bot_username}`).catch(() => ({ opened: false }));
    if (!result.opened) failure.set({ title: "Telegram unavailable", detail: "The bot chat could not be opened." });
  };

  const openBotFather = async () => {
    token.set("");
    const result = await openUrl(BOTFATHER_URL).catch(() => ({ opened: false }));
    if (!result.opened) failure.set({ title: "BotFather unavailable", detail: "The official BotFather chat could not be opened." });
  };

  const paste = () =>
    void Clipboard.readText()
      .then((text) => token.set(text.trim()))
      .catch(() => {
        token.set("");
        failure.set({ title: "Clipboard unavailable", detail: "The bot token could not be pasted." });
      });

  const copyId = (value: string) =>
    void Clipboard.writeText(value)
      .then(() => {
        copied.set(true);
        setTimeout(() => copied.set(false), 1_600);
      })
      .catch(() => failure.set({ title: "Clipboard unavailable", detail: "The value could not be copied." }));

  const label = () => telegramLabel(status(), connected(), telegram().loading);

  return (
    <Page display title="Telegram" testID="telegram">
      <>
        {!daemon() ? (
          <EmptyState icon="paper-plane" title="No current server" detail="Pair a computer in Settings to connect Telegram." />
        ) : !connected() && !status() ? (
          <EmptyState icon="paper-plane" title="Server offline" detail="Reconnect the current server in Settings." />
        ) : (
          <View>
            <View class="mt-4 flex-row items-center gap-3 px-1">
              <MarkView mark="telegram" size={40} />
              <View class="flex-1">
                <Text class="text-xl font-semibold" role="header">{status()?.bot_username ? `@${status()!.bot_username}` : "Telegram"}</Text>
                <View class="flex-row items-center gap-1.5">
                  {telegram().loading || busy() ? <Spinner size={12} /> : null}
                  <StateLine testID="telegram-state" tone={telegramLabelTone(label())} label={label()} />
                </View>
                {status()?.owner_hint ? <Text class="text-xs text-muted-foreground">{status()!.owner_hint}</Text> : null}
              </View>
            </View>
            {failure() ? (
              <Notice testID="telegram-failure" tone="failed" title={failure()!.title} detail={failure()!.detail} />
            ) : status()?.last_error || loadError() ? (
              <Notice testID="telegram-problem" tone="failed" title="Telegram problem" detail={status()?.last_error || loadError()} />
            ) : null}

            {!connected() ? (
              <View class="mt-3 px-4"><Text testID="telegram-offline" class="text-sm text-muted-foreground">Reconnect the current server in Settings.</Text></View>
            ) : null}

            {view() === "loading" ? null : view() === "retry" ? (
              <View class="mt-3">
                <PillButton testID="telegram-retry" filled title="Retry" disabled={blocked()} onPress={loadTelegram} />
              </View>
            ) : view() === "token" ? (
              <View>
                <Section title="Bot token">
                  <Field
                    testID="telegram-token"
                    accessibilityLabel="Telegram bot token"
                    value={token()}
                    masked
                    placeholder="Paste the BotFather token"
                    onChange={(value) => token.set(value)}
                    onSubmit={verify}
                    trailing={
                      <Button accessibilityLabel="Paste Telegram bot token from clipboard" disabled={blocked()} onPress={paste} class="p-1 active:opacity-60 disabled:opacity-50">
                        <Icon name="clipboard" size={18} />
                      </Button>
                    }
                  />
                </Section>
                <View class="mt-3 flex-row gap-3">
                  <PillButton testID="telegram-verify" filled title={busy() === "verify" ? "Verifying" : "Verify token"} busy={busy() === "verify"} disabled={blocked() || !token().trim()} onPress={verify} />
                  {replacing() ? (
                    <PillButton title="Cancel" onPress={() => { token.set(""); replacing.set(false); }} />
                  ) : (
                    <PillButton testID="telegram-botfather" title="BotFather" onPress={() => void openBotFather()} />
                  )}
                </View>
              </View>
            ) : (
              <View>
                <View class="mt-3">
                  {view() === "connect" || view() === "open-link" ? (
                    <PillButton testID="telegram-connect" filled title={view() === "open-link" ? "Open connection link" : "Connect Telegram"} busy={busy() === "connect"} disabled={blocked()} onPress={connect} />
                  ) : view() === "reconnect" ? (
                    <PillButton testID="telegram-reconnect" filled title="Reconnect" busy={busy() === "enable"} disabled={blocked()} onPress={() => run("enable")} />
                  ) : (
                    <PillButton testID="telegram-open" filled title="Open Telegram" onPress={() => void openChat()} />
                  )}
                </View>
                <Section>
                  <Row title="Status" value={telegramLabel(status(), connected(), false)} />
                  {status()?.owner_hint ? <Row title="Recipient" value={telegramRecipient(status()!)} /> : null}
                  {status()?.owner_hint && status()?.enabled ? <Row testID="telegram-disconnect" icon="pause" title="Disconnect" onPress={() => run("disable")} disabled={blocked()} /> : null}
                  {status()?.owner_hint && !status()?.enabled ? <Row testID="telegram-open-row" icon="open-external" title="Open Telegram" onPress={() => void openChat()} /> : null}
                </Section>
                {connected() && !(telegram().loading && !status()) ? (
                  <Section>
                    <Row testID="telegram-advanced" title="Advanced" accessibilityLabel="Advanced" trailing={<Icon name={advanced() ? "chevron-up" : "chevron-down"} size={16} tone="faint" />} onPress={() => advanced.set(!advanced())} />
                    {advanced() ? (
                      <View>
                        <Row title="Chat mode" value={status()?.topics_available ? "Native topics" : "Private chat"} />
                        {status()?.topic_mappings ? <Row title="Session topics" value={String(status()!.topic_mappings)} /> : null}
                        {status()?.brain_thread_id ? (
                          <Row
                            title="Brain conversation"
                            value={copied() ? "Copied" : status()!.brain_thread_id}
                            accessibilityLabel={`Brain conversation ${status()!.brain_thread_id}`}
                            accessibilityHint="Copies the brain conversation ID"
                            onPress={() => copyId(status()!.brain_thread_id!)}
                          />
                        ) : null}
                        {status()?.brain_topic_id ? <Row title="Brain topic" value={String(status()!.brain_topic_id)} /> : null}
                        <Row title="Delivery checks" value={String((status()?.ambiguous_delivery_count ?? 0) + (status()?.topic_ambiguous_ops_count ?? 0))} />
                        {status()?.last_receive_at ? <Row title="Last received" value={formatFullDateTime(status()!.last_receive_at!)} /> : null}
                        {status()?.last_send_at ? <Row title="Last sent" value={formatFullDateTime(status()!.last_send_at!)} /> : null}
                        <Row icon="refresh" title="Refresh" disabled={blocked()} onPress={loadTelegram} />
                        <Row icon="key" title="Replace token" disabled={blocked()} onPress={() => replacing.set(true)} />
                      </View>
                    ) : null}
                  </Section>
                ) : null}
                {connected() && advanced() && !(telegram().loading && !status()) ? (
                  <Section>
                    {status()?.owner_hint ? (
                      <Row
                        destructive
                        icon="person-remove"
                        title="Unlink account"
                        disabled={blocked()}
                        onPress={() => void confirmDestructive(`Unlink ${telegramRecipient(status()!)}?`, "Remove the verified Telegram owner and require a new binding?", "Unlink").then((yes) => { if (yes) void run("revoke"); })}
                      />
                    ) : null}
                    <Row
                      destructive
                      icon="trash"
                      title="Remove bot"
                      disabled={blocked()}
                      onPress={() => void confirmDestructive(status()?.bot_username ? `Remove @${status()!.bot_username}?` : "Remove the Telegram bot?", "Remove this server's token, binding and delivery state? Telegram cloud messages are not deleted.", "Remove").then((yes) => { if (yes) void run("remove"); })}
                    />
                  </Section>
                ) : null}
              </View>
            )}
          </View>
        )}
      </>
    </Page>
  );
}
