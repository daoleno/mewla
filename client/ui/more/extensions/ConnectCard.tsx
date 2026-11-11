import { announce, Clipboard, computed, openUrl, platform, signal, Text, untracked, View } from "nsh";
import type { JSX } from "nsh";
import { accessControl, connectsWithForm, signInAddressFromText } from "../../../core/extensions/pluginConnectionsModel";
import { brainCanNow, pluginJobs } from "../../../core/extensions/pluginOnboarding";
import { Field, Notice, Spinner } from "../kit/kit";
import { Icon, type IconName } from "../kit/Icon";
import type { PageNav } from "../routes";
import { Action } from "./Action";
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
      const f = s.flow?.flow;
      const none: CardView = { key: "none", render: () => null };
      if (!s.service && !s.connectError) return none;
      if (serviceName === null) {
        // A sign-in that came back to a page still loading the list waits for it to name the service.
        if (s.service && !s.loaded) return none;
        return s.connectError
          ? { key: `ended:${s.connectError}`, render: () => <Card glyph="info-fill" title="That sign-in already ended" detail={s.connectError}><Action label="OK" onPress={() => flow().dismiss()} /></Card> }
          : none;
      }
      if (f?.install_required && f.status === "waiting") {
        return {
          key: `install:${f.id}:${f.message ?? ""}:${f.install_manual}:${f.install_failed}`,
          render: () => (
            <Card title={f.message || `Install ${serviceName} CLI`} detail={f.message ? undefined : "Install the official CLI on this computer, then sign in."}>
              {f.install_manual && f.install_url ? <Action label={`Download ${serviceName} CLI`} onPress={() => void openUrl(f.install_url!)} /> : null}
              <Action filled testID="install-cli" label={f.install_manual ? "Check again" : f.install_failed ? "Retry" : "Install and sign in"} busy={starting() || verifying()} onPress={() => void submit("").catch(() => {})} />
              <Action label="Cancel" onPress={cancel} />
            </Card>
          ),
        };
      }
      if (f?.installing && f.status === "waiting") return { key: `installing:${f.id}`, render: () => <Card busy title={`Installing ${serviceName} CLI…`}><Action label="Cancel" onPress={cancel} /></Card> };
      if (f?.code_required && (s.phase === "waiting" || s.phase === "verifying")) {
        // Tailscale and Google Workspace: an OAuth client made in the service's console, pasted here.
        const secret = f.integration === "tailscale" || !!f.setup_required;
        const noun = f.setup_required ? "client ID and secret" : secret ? "client secret" : "code";
        return {
          key: `code:${f.id}:${noun}`,
          render: () => (
            <Card title={secret ? `Connect ${serviceName}` : `Sign in to ${serviceName}`} detail={secret ? f.message : `Open ${serviceName}, sign in, then paste the code shown there.`}>
              <PasteEntry kind="code" noun={noun} label={noun === "code" ? `Paste the code ${serviceName} shows` : `Paste the ${noun}`} onSubmit={submit} />
              <Action label={`Open ${serviceName} again`} onPress={open} />
              <Action label="Cancel" onPress={cancel} />
            </Card>
          ),
        };
      }
      if (f?.status === "confirm") {
        const identity = f.identity || `${serviceName} account`;
        return {
          key: `confirm:${f.id}`,
          render: () => (
            <Card glyph="person-circle" title={`Use ${identity}?`} detail="The GitHub login on this server. Mewla keeps its own copy; later sign-ins there don't change it.">
              <Action filled label={`Connect ${identity}`} busy={starting()} onPress={() => void flow().importGitHub()} />
              <Action label="Cancel" onPress={cancel} />
            </Card>
          ),
        };
      }
      if (s.phase === "opening") return { key: `opening:${serviceName}`, render: () => <Card busy title={`Opening ${serviceName}…`} /> };
      if ((s.phase === "waiting" || s.phase === "verifying") && f?.address_required) {
        return {
          key: `address:${f.id}`,
          render: () => (
            <Card title={`Finish signing in to ${serviceName}`} detail="After sign-in, copy the full address of the page you land on, paste it below, then choose Continue.">
              <PasteEntry kind="address" noun="address" label="Address from the browser" onSubmit={submit} />
              <Action label={`Open ${serviceName} again`} disabled={verifying()} onPress={open} />
              <Action label="Cancel" onPress={cancel} />
            </Card>
          ),
        };
      }
      if (s.phase === "verifying") return { key: `verifying:${serviceName}`, render: () => <Card busy title={`Checking your ${serviceName} account…`} detail="This takes a moment." /> };
      if (s.phase === "waiting" && f?.user_code) {
        const code = f.user_code;
        return {
          key: `device:${f.id}:${code}`,
          render: () => (
            <Card busy title={`Enter this code on ${serviceName}`} detail="Mewla copies it for you. This updates when you're done.">
              <View selectable>
                <Text testID="device-code" class="py-2 text-center font-mono text-2xl font-semibold tracking-widest" accessibilityLabel={`Code ${code.split("").join(" ")}`}>{code}</Text>
              </View>
              <Action filled testID="open-code-page" label={`Copy code and open ${serviceName}`} onPress={open} />
              {s.service === "github" ? <Action label="Use the GitHub login on this server" onPress={() => void flow().useServerGitHub()} /> : null}
              <Action label="Cancel" onPress={cancel} />
            </Card>
          ),
        };
      }
      if (s.phase === "waiting") {
        const detail = f?.message || s.connectError || undefined;
        return {
          key: `waiting:${f?.id}:${detail ?? ""}`,
          render: () => (
            <Card busy title={`Waiting for ${serviceName}`} detail={detail}>
              {f?.authorization_url ? <Action testID="open-again" label={`Open ${serviceName} again`} onPress={open} /> : null}
              <Action testID="cancel-sign-in" label="Cancel" onPress={cancel} />
            </Card>
          ),
        };
      }
      const connected = s.accounts.find((account) => account.id === s.connectedId) ?? null;
      if (s.phase === "connected" && connected && connected.access?.read === "none" && connected.access.write === "none") {
        return {
          key: `custom:${connected.id}`,
          render: () => (
            <Card glyph="check-circle-fill" tone="ready" title={`${connected.name} connected`} detail="Choose which of its tools Brain may use. None are allowed yet.">
              <Action filled label="Choose tools" onPress={() => { flow().dismiss(); props.nav.push({ name: "extensions-tools", service: connected.integration, account: connected.id }); }} />
              <Action label="Later" onPress={() => flow().dismiss()} />
            </Card>
          ),
        };
      }
      if (s.phase === "connected" && connected) {
        const changes = accessControl(connected, "write");
        const changesOn = changes.kind === "switch" && changes.on;
        const job = s.service ? pluginJobs[s.service] : undefined;
        const detail = `${connected.access?.read === "none" ? `Brain can use ${connected.name} once you allow changes.` : brainCanNow(job?.read, connected.name)}${changes.kind === "consent" ? ` To let it make changes too, ${serviceName} asks you once more.` : ""}`;
        const offerChanges = changes.kind !== "none" && !changesOn;
        // A new AWS profile is offered a readable name before it is left with the CLI's own.
        const naming = connected.integration === "aws" && !!connected.profile;
        return {
          key: `connected:${connected.id}:${offerChanges}:${naming}:${detail}`,
          render: () => (
            <Card testID="connected-card" glyph="check-circle-fill" tone="ready" title={`${serviceName} connected`} detail={detail}>
              {offerChanges ? <Action filled label="Allow changes too" accessibilityLabel={job?.write ? `Allow changes too. ${job.write}` : undefined} busy={flowState().running === `write:${connected.id}`} onPress={() => flow().setAccess(connected, "write", true)} /> : null}
              {naming ? (
                <View class="-mx-4">
                  <ProfileName account={connected} suggest onDone={() => flow().dismiss()} />
                </View>
              ) : null}
              <Action testID="connect-done" label="Done" onPress={() => flow().dismiss()} />
            </Card>
          ),
        };
      }
      if ((s.phase === "failed" || s.phase === "cancelled") && s.connectError) {
        const failed = s.phase === "failed";
        return {
          key: `${s.phase}:${s.connectError}`,
          render: () => (
            <Card testID="connect-failed" glyph={failed ? "alert-circle-fill" : "info-fill"} tone={failed ? "failed" : undefined} title={failed ? `Couldn't connect ${serviceName}` : "Sign-in cancelled"} detail={s.connectError}>
              <Action filled label="Start again" busy={starting()} onPress={retry} />
              <Action label="Dismiss" onPress={() => flow().dismiss()} />
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

/** The sign-in's card. It is made again for each step, and each step is heard: Waiting, then Connected. */
function Card(props: { title: string; detail?: string; busy?: boolean; glyph?: IconName; tone?: "ready" | "failed"; testID?: string; children?: JSX.Element }) {
  if (platform.os === "ios") {
    // Every Plugins page on the stack shows the card: the step is said once.
    const words = untracked(() => [props.title, props.detail].filter(Boolean).join(". "));
    if (words !== lastAnnounced) {
      lastAnnounced = words;
      announce(words);
    }
  }
  return (
    <View testID={props.testID ?? "connect-card"} android={{ accessibilityLiveRegion: "polite" }} class="mt-4 gap-2 rounded-2xl border border-border bg-card px-4 py-4">
      <View class="flex-row items-center gap-2">
        {props.busy ? <Spinner size={18} /> : props.glyph ? <Icon name={props.glyph} size={20} tone={props.tone ?? "soft"} /> : null}
        <Text class="flex-1 text-lg font-semibold" role="header">{props.title}</Text>
      </View>
      {props.detail ? <Text class="text-sm text-muted-foreground">{props.detail}</Text> : null}
      {props.children ? <View class="mt-1 gap-2">{props.children}</View> : null}
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
      if (props.kind === "address") error.set("Paste the full address of the page opened after sign-in, even if that page could not load.");
      return;
    }
    busy.set(true);
    error.set("");
    try {
      await props.onSubmit(value);
    } catch (failure) {
      error.set(failure instanceof Error ? failure.message : props.kind === "address" ? "Could not send the address. Try again." : `Could not send the ${props.noun}. Try again.`);
    } finally {
      busy.set(false);
    }
  };
  const paste = async () => {
    try {
      const pasted = await Clipboard.readText();
      text.set(pasted);
      error.set(pasted.trim() ? "" : props.kind === "address" ? "The clipboard is empty. Copy the address from the browser first." : `The clipboard is empty. Copy the ${props.noun} first.`);
    } catch {
      error.set(`Clipboard access is unavailable. Paste directly into the ${props.kind === "address" ? "address" : "code"} field.`);
    }
  };
  return (
    <View class="-mx-4 gap-2">
      <Field
        label={props.label}
        accessibilityLabel={props.kind === "address" ? "Address the sign-in ended on" : undefined}
        testID={props.kind === "address" ? "sign-in-address" : "sign-in-code"}
        value={text()}
        mono
        multiline={props.kind === "code"}
        keyboardType={props.kind === "address" ? "url" : "default"}
        placeholder={props.kind === "address" ? "Address starting with http…" : `Paste ${props.noun} here`}
        onChange={(value) => { text.set(value); error.set(""); }}
        onSubmit={() => void submit()}
      />
      <View class="gap-2 px-4">
        {error() ? <Notice tone="failed" title={props.kind === "address" ? "Check the address" : `Check the ${props.noun}`} detail={error()} /> : null}
        <Action label={props.kind === "address" ? "Paste address" : `Paste ${props.noun}`} disabled={busy()} onPress={() => void paste()} />
        <Action filled testID={props.kind === "address" ? "sign-in-address-continue" : "sign-in-code-continue"} label="Continue" busy={busy()} disabled={!text().trim()} onPress={() => void submit()} />
      </View>
    </View>
  );
}
