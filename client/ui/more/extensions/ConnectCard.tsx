import { announce, Clipboard, computed, openUrl, platform, signal, Text, untracked, View } from "nsh";
import type { JSX } from "nsh";
import { accessControl, connectsWithForm, signInAddressFromText } from "../../../core/extensions/pluginConnectionsModel";
import { pluginJobs } from "../../../core/extensions/pluginOnboarding";
import { color, type ColorName } from "../../kit/colors";
import { Field, Spinner } from "../../kit/kit";
import { Icon, type IconName } from "../../kit/Icon";
import { RowTile } from "../../kit/RowTile";
import type { PageNav } from "../routes";
import { Action, GhostAction, IconAction } from "./Action";
import { serviceRowLead } from "./ServiceMark";
import { flowState, plugins } from "./plugins";
import { ProfileName } from "./ProfileName";

type CardView = { key: string; render: () => JSX.Element | null };

/**
 * The one sign-in in progress, or how it ended, on top of every Extensions page.
 * The card is built again only when what it shows changes (its key), so a poll
 * every few seconds never resets a field the person is typing in.
 */
export function ConnectCard(props: { nav: PageNav }) {
  const flow = () => plugins()!;
  const starting = () => flowState().running === `connect:${flowState().service}`;
  const verifying = () => flowState().phase === "verifying";
  const retry = () => {
    const service = flowState().service;
    if (!service) return flow().dismiss();
    if (connectsWithForm(service)) {
      flow().dismiss();
      props.nav.push({ name: "extensions-service", id: service });
    } else void flow().connect(service);
  };
  const open = () => void flow().openBrowser();
  const cancel = () => void flow().cancel();
  const submit = (text: string) => flow().submitAddress(text);

  const view = computed<CardView>(
    () => {
      const s = flowState();
      const serviceName = flow()?.plugin(s.service)?.name ?? null;
      const service = s.service;
      const f = s.flow?.flow;
      const none: CardView = { key: "none", render: () => null };
      if (!s.service && !s.connectError) return none;
      if (serviceName === null) {
        // A sign-in that came back to a page still loading the list waits for it to name the service.
        if (s.service && !s.loaded) return none;
        return s.connectError
          ? { key: `ended:${s.connectError}`, render: () => <Card glyph="info-fill" title="That sign-in already ended" detail={s.connectError} onClose={() => flow().dismiss()} closeLabel="Dismiss" /> }
          : none;
      }
      // Open, first and filled while the browser has not opened the page (it blocked a tab that came late); quiet once it has.
      const openButton = (testID: string, disabled?: boolean) => (
        <If when={flowState().blocked} else={<GhostAction testID={testID} icon="open-external" label={`Open ${serviceName}`} disabled={disabled} onPress={open} />}>
          <Action filled testID={testID} label={`Open ${serviceName}`} disabled={disabled} onPress={open} />
        </If>
      );
      if (f?.install_required && f.status === "waiting") {
        return {
          key: `install:${f.id}:${f.message ?? ""}:${f.install_manual}:${f.install_failed}`,
          render: () => (
            <Card service={service} title={`Install ${serviceName} CLI`} detail={f.message || undefined} onClose={cancel}>
              <Action filled testID="install-cli" label={f.install_manual ? "Check again" : f.install_failed ? "Retry" : "Install and sign in"} busy={starting() || verifying()} onPress={() => void submit("").catch(() => {})} />
              <If when={f.install_manual && f.install_url}><GhostAction icon="download" label="Download" accessibilityLabel={`Download ${serviceName} CLI`} onPress={() => void openUrl(f.install_url!)} /></If>
            </Card>
          ),
        };
      }
      if (f?.installing && f.status === "waiting") return { key: `installing:${f.id}`, render: () => <Card busy title={`Installing ${serviceName} CLI`} onClose={cancel} /> };
      if (f?.code_required && (s.phase === "waiting" || s.phase === "verifying")) {
        // Tailscale and Google Workspace: an OAuth client made in the service's console, pasted here; the computer says how.
        const secret = f.integration === "tailscale" || !!f.setup_required;
        const noun = f.setup_required ? "client ID and secret" : secret ? "client secret" : "code";
        return {
          key: `code:${f.id}:${noun}`,
          render: () => (
            <Card service={service} title={secret ? `Paste the ${noun}` : `Paste the code from ${serviceName}`} detail={secret ? f.message : undefined} onClose={cancel}>
              <If when={f.authorization_url}>{openButton("open-again")}</If>
              <PasteEntry kind="code" noun={noun} label={noun === "code" ? `Code from ${serviceName}` : noun} onSubmit={submit} />
            </Card>
          ),
        };
      }
      if (f?.status === "confirm") {
        const identity = f.identity || `${serviceName} account`;
        return {
          key: `confirm:${f.id}`,
          render: () => (
            <Card service={service} title={`Use ${identity}?`} onClose={cancel}>
              <Action filled label="Connect" accessibilityLabel={`Connect ${identity}`} busy={starting()} onPress={() => void flow().importGitHub()} />
            </Card>
          ),
        };
      }
      if (s.phase === "opening") return { key: `opening:${serviceName}`, render: () => <Card busy title={`Opening ${serviceName}`} /> };
      if ((s.phase === "waiting" || s.phase === "verifying") && f?.address_required) {
        return {
          key: `address:${f.id}`,
          render: () => (
            <Card service={service} title="Paste the address you landed on" onClose={cancel}>
              {openButton("open-again", verifying())}
              <PasteEntry kind="address" noun="address" label="Address from the browser" onSubmit={submit} />
            </Card>
          ),
        };
      }
      if (s.phase === "verifying") return { key: `verifying:${serviceName}`, render: () => <Card busy title={`Checking your ${serviceName} account`} /> };
      if (s.phase === "waiting" && f?.user_code) {
        const code = f.user_code;
        return {
          key: `device:${f.id}:${code}`,
          render: () => (
            <Card service={service} title={`Enter this code on ${serviceName}`} onClose={cancel}>
              <View selectable>
                <Text testID="device-code" class="py-2 text-center font-mono text-2xl font-semibold tracking-widest" accessibilityLabel={`Code ${code.split("").join(" ")}`}>{code}</Text>
              </View>
              <Action filled testID="open-code-page" label={`Copy and open ${serviceName}`} onPress={open} />
              <If when={s.service === "github"}><GhostAction icon="desktop" label="Use this computer's login" accessibilityLabel="Use the GitHub login on this server" onPress={() => void flow().useServerGitHub()} /></If>
            </Card>
          ),
        };
      }
      if (s.phase === "waiting") {
        const detail = f?.message || s.connectError || undefined;
        return {
          key: `waiting:${f?.id}:${detail ?? ""}`,
          render: () => (
            <Card busy title={`Waiting for ${serviceName}`} detail={detail} onClose={cancel} closeTestID="cancel-sign-in">
              <If when={f?.authorization_url}>{openButton("open-again")}</If>
            </Card>
          ),
        };
      }
      const connected = s.accounts.find((account) => account.id === s.connectedId) ?? null;
      if (s.phase === "connected" && connected && connected.access?.read === "none" && connected.access.write === "none") {
        return {
          key: `custom:${connected.id}`,
          render: () => (
            <Card glyph="check-circle-fill" tone="ok" title={`${connected.name} connected`} onClose={() => flow().dismiss()} closeLabel="Later">
              <Action filled label="Choose tools" onPress={() => { flow().dismiss(); props.nav.push({ name: "extensions-tools", service: connected.integration, account: connected.id }); }} />
            </Card>
          ),
        };
      }
      if (s.phase === "connected" && connected) {
        const changes = accessControl(connected, "write");
        const changesOn = changes.kind === "switch" && changes.on;
        const job = s.service ? pluginJobs[s.service] : undefined;
        const offerChanges = changes.kind !== "none" && !changesOn;
        // A new AWS profile is offered a readable name before it is left with the CLI's own.
        const naming = connected.integration === "aws" && !!connected.profile;
        return {
          key: `connected:${connected.id}:${offerChanges}:${naming}`,
          render: () => (
            <Card testID="connected-card" glyph="check-circle-fill" tone="ok" title={`${serviceName} connected`} onClose={() => flow().dismiss()} closeLabel="Done" closeTestID="connect-done">
              <If when={naming}><View class="-mx-4 -mt-2">
                  <ProfileName account={connected} suggest onDone={() => flow().dismiss()} />
                </View></If>
              <If when={offerChanges}><GhostAction icon="edit" label="Allow changes" accessibilityLabel={job?.write ? `Allow changes too. ${job.write}` : "Allow changes too"} busy={flowState().running === `write:${connected.id}`} onPress={() => flow().setAccess(connected, "write", true)} /></If>
            </Card>
          ),
        };
      }
      if ((s.phase === "failed" || s.phase === "cancelled") && s.connectError) {
        const failed = s.phase === "failed";
        // A cancel the person made needs no card of its own.
        if (!failed) return none;
        return {
          key: `${s.phase}:${s.connectError}`,
          render: () => (
            <Card testID="connect-failed" glyph="alert-circle-fill" tone="sealInk" title={`Couldn't connect ${serviceName}`} detail={s.connectError} onClose={() => flow().dismiss()} closeLabel="Dismiss">
              <Action filled label="Try again" busy={starting()} onPress={retry} />
            </Card>
          ),
        };
      }
      return none;
    },
    { equals: (a, b) => a.key === b.key },
  );

  return <View>{untrackedRender(view())}</View>;
}

/** Builds a card without tracking what building it reads; its own expressions track what they show. */
function untrackedRender(view: CardView): JSX.Element | null {
  return untracked(view.render);
}

let lastAnnounced = "";

/**
 * The sign-in's card: the service's mark (a ring while it waits), one line saying what to do, × to stop, then
 * the one thing to do now. It is made again for each step, and each step is heard: Waiting, then Connected.
 */
function Card(props: { title: string; detail?: string; service?: string | null; busy?: boolean; glyph?: IconName; tone?: ColorName; testID?: string; onClose?: () => void; closeLabel?: string; closeTestID?: string; children?: JSX.Element }) {
  if (platform.os === "ios") {
    // Every Plugins page on the stack shows the card: the step is said once.
    const words = untracked(() => [props.title, props.detail].filter(Boolean).join(". "));
    if (words !== lastAnnounced) {
      lastAnnounced = words;
      announce(words);
    }
  }
  const service = untracked(() => props.service);
  return (
    <View testID={props.testID ?? "connect-card"} android={{ accessibilityLiveRegion: "polite" }} class="mt-4 gap-3 rounded-(--radius-group) bg-card px-4 py-4">
      <View class="min-h-[30px] flex-row items-center gap-3">
        <View class="size-[30px] items-center justify-center">
          <If when={props.busy} else={<If when={props.glyph} else={<If when={service}>{(id) => <RowTile lead={serviceRowLead(id(), 28)} />}</If>}>{(glyph) => <Icon name={glyph()} size={24} tone={props.tone ?? "soft"} />}</If>}><Spinner size={18} /></If>
        </View>
        <Text class="min-w-0 flex-1 text-body font-semibold" role="header">{props.title}</Text>
        <If when={props.onClose}><IconAction testID={props.closeTestID ?? "close-sign-in"} icon="close" label={props.closeLabel ?? "Cancel sign-in"} onPress={() => props.onClose?.()} /></If>
      </View>
      <If when={props.detail}>{(detail) => <Text class="text-sub text-muted-foreground">{detail()}</Text>}</If>
      <If when={"children" in props}><View class="gap-2">{props.children}</View></If>
    </View>
  );
}

export { Action };

/**
 * What the person pastes back: the address a sign-in ended on, or a CLI code or
 * client. Sent only on Continue, once; a refusal keeps the text and says why.
 */
function PasteEntry(props: { kind: "address" | "code"; noun: string; label: string; onSubmit: (text: string) => Promise<void> }) {
  const text = signal("");
  const error = signal("");
  const busy = signal(false);
  const submit = async () => {
    if (busy()) return;
    const value = props.kind === "address" ? signInAddressFromText(text()) : text().trim();
    if (!value) {
      if (props.kind === "address") error.set("Paste the whole address, even if the page didn't load.");
      return;
    }
    busy.set(true);
    error.set("");
    try {
      await props.onSubmit(value);
    } catch (failure) {
      error.set(failure instanceof Error ? failure.message : `Couldn't send the ${props.noun}. Try again.`);
    } finally {
      busy.set(false);
    }
  };
  const paste = async () => {
    try {
      const pasted = await Clipboard.readText();
      if (pasted.trim()) {
        text.set(pasted);
        error.set("");
      } else error.set(`Nothing to paste. Copy the ${props.noun} first, or paste into the field.`);
    } catch {
      error.set(`Paste into the field.`);
    }
  };
  return (
    <View class="-mx-4 -my-3 gap-2">
      <Field
        accessibilityLabel={props.kind === "address" ? "Address the sign-in ended on" : props.label}
        testID={props.kind === "address" ? "sign-in-address" : "sign-in-code"}
        value={text()}
        mono
        multiline={props.kind === "code"}
        keyboardType={props.kind === "address" ? "url" : "default"}
        placeholder={props.kind === "address" ? "http://…" : props.label}
        onChange={(value) => { text.set(value); error.set(""); }}
        onSubmit={() => void submit()}
        trailing={<IconAction testID="sign-in-paste" icon="clipboard" label={`Paste ${props.noun}`} disabled={busy()} onPress={() => void paste()} />}
      />
      <View class="gap-2 px-4 pb-3">
        <If when={error()}><Text testID="sign-in-error" role="alert" class="text-sub" style={{ color: color("sealInk") }}>{error()}</Text></If>
        <Action filled testID={props.kind === "address" ? "sign-in-address-continue" : "sign-in-code-continue"} label="Continue" busy={busy()} disabled={!text().trim()} onPress={() => void submit()} />
      </View>
    </View>
  );
}
