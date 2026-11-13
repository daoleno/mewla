import { cleanup, Clipboard, computed, openUrl, signal, Text, untracked, View } from "nsh";
import type { JSX } from "nsh";
import { accountRow, endpointHost, openAccountId } from "../../../core/extensions/accountListModel";
import { cloudProfileGroups } from "../../../core/extensions/cloudProfilesModel";
import { accountStatus, type ConnectionRequest, type PluginAccount, type PluginIntegration } from "../../../core/extensions/connections";
import {
  accessControl,
  accountRecovery,
  accountTone,
  allowedToolCount,
  awsAccountSummary,
  canDisconnect,
  connectLabel,
  networkLabel,
  serviceState,
} from "../../../core/extensions/pluginConnectionsModel";
import { pluginJobs } from "../../../core/extensions/pluginOnboarding";
import { serverDisplayName } from "../../../core/extensions/serverDisplayName";
import { formatDateTime, formatFullDateTime } from "../../../core/more/dates";
import { daemonName } from "../daemon";
import { confirmDestructive, EmptyState, Field, Notice, Row, RowButton, Section, Spinner, SwitchRow } from "../../kit/kit";
import { LeadHero, StateLine, tile } from "../../kit/RowTile";
import { connectionStateTone } from "../../../core/extensions/colour";
import type { PageNav } from "../routes";
import { Action } from "./Action";
import { connectingService, PluginsFrame, ToolUpdateNotices } from "./Pages";
import { ProfileName } from "./ProfileName";
import { flowState, plugins } from "./plugins";
import { serviceRowLead } from "./ServiceMark";

const API_WALLETS = "https://app.hyperliquid.xyz/API";
const CLI_NAMES: Record<string, string> = { aws: "AWS CLI", gws: "Google Workspace CLI", gcp: "Google Cloud CLI" };

/** One service: what Brain may do with it, its accounts, and how to connect another. */
export function ServicePage(props: { id: string; nav: PageNav }) {
  const plugin = () => plugins()?.plugin(props.id) ?? null;
  const accounts = () => flowState().accounts.filter((account) => account.integration === props.id && account.status !== "disconnected").sort((a, b) => a.name.localeCompare(b.name));
  const custom = props.id === "mcp" || props.id === "openapi";
  const hasAccounts = computed(() => accounts().length > 0);
  // Built again only when the service itself changes, so a form keeps what was typed.
  const view = computed(
    () => {
      const p = plugin();
      return { key: p ? `${p.id}:${p.available}` : flowState().loaded ? "gone" : "loading", plugin: p };
    },
    { equals: (a, b) => a.key === b.key },
  );
  // The trading form is offered as "Add another key" once an account holds a key.
  const keyed = computed(() => accounts().some((account) => !!account.signer));
  const body = (p: PluginIntegration | null): JSX.Element => {
    if (!p) return flowState().loaded ? <Notice tone="failed" title="Service unavailable" detail={`${serverDisplayName(daemonName(), "This server")} does not offer this service.`} /> : <EmptyState busy title="Loading services" />;
    return (
      <View>
        <ToolUpdateNotices service={p.id} />
        {hasAccounts() ? <AccountList plugin={p} accounts={accounts()} nav={props.nav} /> : !custom && p.id !== "hyperliquid" ? <ConnectOffer plugin={p} /> : null}
        {custom ? <CustomServiceForm plugin={p} again={untracked(hasAccounts)} /> : null}
        {p.id === "hyperliquid" ? <HyperliquidForm plugin={p} keyed={keyed()} /> : null}
        {hasAccounts() && !custom && p.id !== "hyperliquid" && p.available ? (
          <View class="mt-4">
            <Action testID="add-another" label={`Add another ${p.name} account`} busy={connectingService() === p.id} disabled={flowState().running !== null || (connectingService() !== null && connectingService() !== p.id)} onPress={() => void plugins()?.connect(p.id)} />
          </View>
        ) : null}
      </View>
    );
  };
  return (
    <PluginsFrame title={plugin()?.name ?? "Service"} testID="service-page" nav={props.nav} hero={plugin() ? <Header plugin={plugin()!} accounts={accounts()} /> : undefined}>
      {((current) => untracked(() => body(current.plugin)))(view())}
    </PluginsFrame>
  );
}

/** The detail's top block (SPEC §15 Collection): a 64 pt tile, the name in display type, the state line. */
function Header(props: { plugin: PluginIntegration; accounts: PluginAccount[] }) {
  const state = () => serviceState(props.plugin, props.accounts);
  const cli = props.plugin.cli && CLI_NAMES[props.plugin.id] ? `${CLI_NAMES[props.plugin.id]} ${props.plugin.cli.version} · ${props.plugin.cli.managed ? "managed by Mewla" : "local installation"}` : "";
  return (
    <View testID="service-header" class="gap-3 px-1 pb-2 pt-3">
      <LeadHero testID="service-hero-lead" lead={serviceRowLead(props.plugin.id, 40)} />
      <View class="gap-1">
        <Text class="text-display font-bold tracking-display" role="header" style={{ numberOfLines: 2 }}>{props.plugin.name}</Text>
        {(() => {
          const s = state();
          return s.kind === "status" ? <StateLine testID="service-state" tone={connectionStateTone(s.tone)} label={s.label} /> : null;
        })()}
        {cli ? <Text class="text-caption text-muted-foreground">{cli}</Text> : null}
      </View>
    </View>
  );
}

/** A service with nothing connected: what Brain gets, and the one way to connect. */
function ConnectOffer(props: { plugin: PluginIntegration }) {
  const job = pluginJobs[props.plugin.id];
  if (!props.plugin.available) {
    return <Section><Row lead={tile("info")} title="Not yet available" subtitle={props.plugin.unavailable_reason || "Sign-in is not set up yet."} /></Section>;
  }
  const connecting = () => connectingService() === props.plugin.id;
  const busy = () => flowState().running !== null || (connectingService() !== null && !connecting());
  return (
    <View>
      <Section title="What Brain can do" footer={`Your sign-in stays on ${serverDisplayName(daemonName())}. Brain only acts on tasks you give it.`}>
        <Row lead={tile("eye")} title="Read and search" subtitle={job?.read} value="On" />
        <Row lead={tile("edit")} title="Make changes when asked" subtitle={job ? `${job.write}. Off until you allow it.` : "Off until you allow it."} value="Off" />
      </Section>
      <View class="mt-4 gap-2">
        <Action filled testID="service-connect" label={connectLabel(props.plugin)} busy={connecting()} disabled={busy()} onPress={() => void plugins()?.connect(props.plugin.id)} />
        {props.plugin.id === "github" ? <Action label="Use the GitHub login on this server" disabled={busy() || connecting()} onPress={() => void plugins()?.useServerGitHub()} /> : null}
      </View>
    </View>
  );
}

/**
 * The accounts of one service: one collapsed row each, a lone account open. Cloud
 * profiles put the expired ones last, and a long list can be filtered.
 */
function AccountList(props: { plugin: PluginIntegration; accounts: PluginAccount[]; nav: PageNav }) {
  const chosen = signal<string | null | undefined>(undefined);
  const query = signal("");
  const showExpired = signal(false);
  // Expiry labels ("Up to 3h left") count down while the page is open.
  const now = signal(Date.now());
  const tick = setInterval(() => now.set(Date.now()), 60_000);
  cleanup(() => clearInterval(tick));
  // Every service lists its accounts the same way; a long list (more than eight) can be filtered.
  const filterVisible = computed(() => props.accounts.length > 8);
  const groups = () => cloudProfileGroups(props.accounts, filterVisible() ? query() : "");
  const filtering = () => filterVisible() && !!query().trim();
  const row = (account: PluginAccount) => {
    const open = () => openAccountId(props.accounts, chosen()) === account.id;
    const info = () => accountRow(account, now());
    return (
      <View>
        <Section>
          <Row
            testID={`account-${account.id}`}
            lead={tile("person-circle")}
            title={info().name}
            subtitle={[info().identity, info().expiry?.label].filter(Boolean).join(" · ") || undefined}
            trailing={<StateLine tone={connectionStateTone(info().status.tone)} label={info().status.label} />}
            chevron={!open()}
            accessibilityLabel={[info().name, info().identity, info().status.label].filter(Boolean).join(", ")}
            accessibilityHint="Show account actions"
            expanded={open()}
            onPress={() => chosen.set(open() ? null : account.id)}
          />
          {open() ? <AccountCard plugin={props.plugin} account={account} nav={props.nav} /> : null}
        </Section>
        {open() ? <AccountEnd account={account} /> : null}
      </View>
    );
  };
  return (
    <View>
      {filterVisible() ? (
        <View class="-mx-4 mt-2">
          <Field testID="filter-accounts" accessibilityLabel="Filter accounts" placeholder="Filter accounts" value={query()} onChange={(value) => query.set(value)} />
        </View>
      ) : null}
      {(() => {
        const g = groups();
        const shown = [...g.signedIn, ...g.unchecked];
        if (!shown.length && !g.expired.length) return query() ? <EmptyState title="No matching accounts" /> : null;
        return (
          <View>
            {shown.map(row)}
            {g.expired.length ? (
              <View>
                <Section>
                  <Row title={`Expired (${g.expired.length})`} chevron={!(showExpired() || filtering())} expanded={showExpired() || filtering()} onPress={() => showExpired.set(!showExpired())} />
                </Section>
                {showExpired() || filtering() ? g.expired.map(row) : null}
              </View>
            ) : null}
          </View>
        );
      })()}
    </View>
  );
}

function disconnectCopy(account: PluginAccount): { title: string; detail: string; action: string } {
  if (account.integration === "gws") return { title: `Sign ${account.identity || account.name} out of Google Workspace on this computer?`, detail: "Your Google client stays, so connecting again is quick.", action: "Sign out" };
  if (account.integration === "gcp") return { title: `Delete ${account.identity || account.name} from this computer?`, detail: "This signs it out of gcloud and removes its configurations. Other accounts are kept.", action: "Delete account" };
  if (account.profile) return { title: `Delete profile ${account.profile}${account.aws?.account_id ? ` (${account.aws.account_id})` : ""} from this computer?`, detail: "It will also disappear from the AWS CLI. A backup is kept.", action: "Delete profile" };
  return { title: `Disconnect ${account.name}?`, detail: "Brain loses access right away, and Mewla removes its saved sign-in.", action: "Disconnect" };
}

/**
 * One account: its switch, the one fix it needs if any, what Brain may do, its
 * tools, and Disconnect after one confirm.
 */
function AccountCard(props: { plugin: PluginIntegration; account: PluginAccount; nav: PageNav }) {
  const account = () => flowState().accounts.find((item) => item.id === props.account.id) ?? props.account;
  const running = () => flowState().running;
  const busy = () => running() !== null;
  const job = pluginJobs[props.plugin.id];
  const custom = props.plugin.id === "mcp" || props.plugin.id === "openapi";
  const showDetails = signal(false);
  const copied = signal<string | null>(null);
  const renaming = signal(false);
  const flow = () => plugins()!;

  const group = (name: "read" | "write"): JSX.Element | null => {
    const a = account();
    const control = accessControl(a, name);
    if (control.kind === "none") return null;
    const title = name === "read" ? "Read and search" : "Make changes when asked";
    const note = control.kind === "consent" ? `${props.plugin.name} asks you to approve this once.` : control.note;
    const subtitle = [name === "read" ? job?.read : job?.write, note].filter(Boolean).join(". ");
    const pending = running() === `${name}:${a.id}` || (name === "write" && control.kind === "consent" && running() === `connect:${props.plugin.id}`);
    if (control.kind === "consent") {
      return <Row lead={tile("edit")} title={title} subtitle={subtitle} trailing={pending ? <Spinner size={16} /> : <RowButton label="Allow" accessibilityLabel={`Allow changes in ${props.plugin.name}`} disabled={busy()} onPress={() => flow().setAccess(a, name, true)} />} />;
    }
    return <SwitchRow testID={`${name}-${a.id}`} title={title} subtitle={subtitle} checked={control.on} busy={pending} disabled={busy() && !pending} onChange={(on) => flow().setAccess(a, name, on)} />;
  };

  return (
    <View>
      {(() => {
        const a = account();
        const aws = awsAccountSummary(a);
        const recovery = accountRecovery(a, props.plugin.name);
        const network = a.network === "testnet" ? networkLabel(a.network) : null;
        const subtitle = a.profile
          ? [a.profile_kind, a.profile_status, a.active ? "Active" : "", a.project, a.integration === "gcp" ? a.identity : "", a.aws?.account_id, a.aws?.account_name].filter(Boolean).join(" · ")
          : aws
            ? aws.subtitle
            : [custom ? endpointHost(a.endpoint) : a.identity !== a.name ? a.identity : "", a.verified_at ? `Checked ${formatDateTime(a.verified_at)}` : ""].filter(Boolean).join(" · ");
        const tools = allowedToolCount(a);
        return (
          <View>
            {/* An account that is off reads quieter; its switch still turns it on. */}
            <View style={{ opacity: a.enabled ? 1 : 0.6 }}>
            <SwitchRow
              testID={`enabled-${a.id}`}
              accessibilityLabel={`${props.plugin.name} ${a.name} on`}
              title={[network, recovery ? null : accountStatus(a)].filter(Boolean).join(" · ") || "On"}
              subtitle={subtitle || undefined}
              checked={a.enabled}
              busy={running() === `enabled:${a.id}`}
              disabled={busy() && running() !== `enabled:${a.id}`}
              onChange={(on) => flow().setEnabled(a, on)}
            />
            </View>
            {a.integration === "aws" && a.profile ? (renaming() ? <ProfileName account={a} onDone={() => renaming.set(false)} /> : <Row lead={tile("edit")} title="Rename profile" onPress={() => renaming.set(true)} />) : null}
            {a.profile_sign_in_by ? <Row lead={tile("time")} title="Sign in again by" subtitle={`${formatFullDateTime(a.profile_sign_in_by)} · ${a.profile_sign_in_limit === "maximum" ? "At latest · AWS allows up to 12h; your session may end sooner." : "SSO session"}`} /> : null}
            {a.profile ? <Row lead={tile("person-circle")} title="Sign in again" subtitle={`Use ${props.plugin.name} remote sign-in for this ${a.integration === "aws" ? "profile" : "account"}.`} trailing={connectingService() === props.plugin.id ? <Spinner size={16} /> : <RowButton label="Sign in again" disabled={busy()} onPress={() => flow().recover(a, "reconnect")} />} /> : null}
            {!a.profile && recovery ? (
              <Row
                testID={`recover-${a.id}`}
                lead={tile(recovery.tone === "neutral" ? "pause-circle" : "alert-circle")}
                title={recovery.title}
                subtitle={recovery.detail}
                // Whichever fix it is (check, sign in again, finish the removal), its button turns while it runs.
                trailing={
                  running() === `recover:${a.id}` || (recovery.action === "reconnect" && connectingService() === props.plugin.id) || (recovery.action === "disconnect" && running() === `disconnect:${a.id}`)
                    ? <Spinner size={16} />
                    : <RowButton label={recovery.actionLabel} disabled={busy()} onPress={() => flow().recover(a, recovery.action)} />
                }
              />
            ) : null}
            {aws ? (
              <View>
                <Row lead={tile("info")} title="Details" chevron={!showDetails()} onPress={() => showDetails.set(!showDetails())} />
                {showDetails()
                  ? aws.details.map((detail) => (
                      <Row
                        title={detail.label}
                        accessibilityLabel={detail.copy ? `Copy ${detail.label}` : undefined}
                        subtitle={detail.value}
                        value={copied() === detail.label ? "Copied" : undefined}
                        onPress={detail.copy ? () => void Clipboard.writeText(detail.value).then(() => copied.set(detail.label)) : undefined}
                      />
                    ))
                  : null}
              </View>
            ) : null}
            {custom ? null : group("read")}
            {custom ? null : group("write")}
            <Row
              testID={`tools-${a.id}`}
              lead={tile("list")}
              title={custom ? `${tools.allowed} of ${tools.total} tools allowed` : "Tools & activity"}
              subtitle={custom && tools.allowed === 0 ? "Choose which tools Brain may use." : undefined}
              value={custom || !tools.total ? undefined : `${tools.allowed}/${tools.total}`}
              chevron
              onPress={() => props.nav.push({ name: "extensions-tools", service: props.plugin.id, account: a.id })}
            />
          </View>
        );
      })()}
    </View>
  );
}

/** An open account's irreversible steps, alone in the last group (SPEC §15): removing its trading key, Disconnect. */
function AccountEnd(props: { account: PluginAccount }) {
  const account = () => flowState().accounts.find((item) => item.id === props.account.id) ?? props.account;
  const running = () => flowState().running;
  const busy = () => running() !== null;
  const flow = () => plugins()!;
  return (
    <View>
      {(() => {
        const a = account();
        const copy = disconnectCopy(a);
        if (!a.signer && !a.profile && !canDisconnect(a)) return null;
        return (
          <Section>
            {a.signer ? (
              <Row
                icon="key"
                title="Remove trading key"
                subtitle={`API wallet ${a.signer.slice(0, 6)}…${a.signer.slice(-4)}`}
                trailing={running() === `key:${a.id}` ? <Spinner size={16} /> : undefined}
                destructive
                disabled={busy()}
                onPress={() =>
                  void confirmDestructive(`Remove the trading key from ${a.name}?`, "Brain can still read it, and can no longer place or cancel orders. Mewla deletes the saved key.", "Remove key").then((yes) => yes && flow().removeKey(a))
                }
              />
            ) : null}
            {!a.profile && !canDisconnect(a) ? null : (
              <Row
                testID={`disconnect-${a.id}`}
                icon="remove-circle"
                title={copy.action}
                trailing={running() === `disconnect:${a.id}` ? <Spinner size={16} /> : undefined}
                destructive
                disabled={busy()}
                onPress={() => void confirmDestructive(copy.title, copy.detail, copy.action).then((yes) => yes && flow().disconnect(a))}
              />
            )}
          </Section>
        );
      })()}
    </View>
  );
}

/** A custom service's address and sign-in, handed to the one connect action. */
function CustomServiceForm(props: { plugin: PluginIntegration; again: boolean }) {
  const name = signal("");
  const endpoint = signal("");
  const token = signal("");
  const spec = signal("");
  const browser = signal(props.plugin.id === "mcp");
  const trust = signal(false);
  const networks = signal("");
  const error = signal("");
  const open = signal(!props.again);
  const connect = async () => {
    error.set("");
    let document: unknown;
    if (props.plugin.id === "openapi") {
      try {
        document = JSON.parse(spec());
      } catch {
        return error.set("Enter a valid OpenAPI JSON document.");
      }
    }
    const input: NonNullable<ConnectionRequest["input"]> = {
      integration: props.plugin.id,
      name: name().trim() || props.plugin.name,
      endpoint: endpoint().trim(),
      credential: token(),
      spec: document,
      trusted_networks: trust() ? networks().split(",").map((s) => s.trim()).filter(Boolean) : undefined,
    };
    token.set("");
    await plugins()?.connect(props.plugin.id, { input, signIn: browser() });
  };
  return (
    <View>
      {open() ? (
        <View>
          <Section title="Service">
            <Field label="Name" accessibilityLabel="Service name" testID="custom-name" placeholder={props.plugin.name} value={name()} onChange={(value) => name.set(value)} />
            <Field label="Endpoint" accessibilityLabel="Service endpoint" testID="custom-endpoint" placeholder="https://service.example.com/mcp" keyboardType="url" value={endpoint()} onChange={(value) => endpoint.set(value)} />
          </Section>
          <Section title="Authorization">
            {props.plugin.id === "mcp" ? <SwitchRow testID="custom-browser" title="Sign in with browser" checked={browser()} onChange={(on) => browser.set(on)} /> : null}
            {!browser() ? <Field label="Token" accessibilityLabel="Service token" testID="custom-token" placeholder="Optional for public services" masked value={token()} onChange={(value) => token.set(value)} /> : null}
          </Section>
          {props.plugin.id === "openapi" ? (
            <Section title="OpenAPI document">
              <Field label="OpenAPI 3 JSON" accessibilityLabel="OpenAPI document" testID="custom-spec" placeholder="Paste the JSON document" multiline mono value={spec()} onChange={(value) => spec.set(value)} />
            </Section>
          ) : null}
          <Section title="Network" footer="Only for an internal service you trust.">
            <SwitchRow title="Allow a private network" checked={trust()} onChange={(on) => trust.set(on)} />
            {trust() ? <Field label="Trusted ranges" accessibilityLabel="Trusted network ranges" placeholder="192.168.1.10/32" value={networks()} onChange={(value) => networks.set(value)} /> : null}
          </Section>
          {error() ? <Notice tone="failed" title="Check the form" detail={error()} /> : null}
          <View class="mt-4">
            <Action filled testID="custom-connect" label={browser() ? "Continue in browser" : "Connect service"} busy={connectingService() === props.plugin.id} disabled={!endpoint().trim() || (trust() && !networks().trim())} onPress={() => void connect()} />
          </View>
          <View class="mt-2 px-4"><Text testID="custom-note" class="text-caption text-muted-foreground">{`Credentials stay on ${serverDisplayName(daemonName())}. Each tool needs your permission.`}</Text></View>
        </View>
      ) : (
        <View class="mt-4">
          <Action label={props.plugin.id === "mcp" ? "Add another MCP server" : "Add another OpenAPI service"} onPress={() => open.set(true)} />
        </View>
      )}
    </View>
  );
}

/**
 * Hyperliquid: reading needs nothing; trading needs an API wallet key, an
 * optional step that stays one row until the person wants it.
 */
function HyperliquidForm(props: { plugin: PluginIntegration; keyed: boolean }) {
  const open = signal(false);
  const key = signal("");
  const connect = async () => {
    const credential = key().trim();
    key.set("");
    await plugins()?.connect(props.plugin.id, { input: { integration: props.plugin.id, credential }, signIn: false });
  };
  return (
    <View>
      {!open() ? (
        <Section title="Trading">
          <Row testID="hyperliquid-add-key" lead={tile("key")} title={props.keyed ? "Add another key" : "Add API wallet key"} chevron onPress={() => open.set(true)} />
        </Section>
      ) : (
        <View>
          <Section title="Trading" footer={`The key stays on ${serverDisplayName(daemonName())}.`}>
            <Field label="API wallet key" accessibilityLabel="Hyperliquid API wallet key" placeholder="0x…" masked value={key()} onChange={(value) => key.set(value)} />
          </Section>
          <View class="mt-4 gap-2">
            <Action filled label="Add key" busy={connectingService() === props.plugin.id} disabled={!key().trim()} onPress={() => void connect()} />
            <Action label="Create an API wallet" onPress={() => void openUrl(API_WALLETS)} />
          </View>
        </View>
      )}
    </View>
  );
}

export { accountTone };
