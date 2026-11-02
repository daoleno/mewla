import { cloudProfilesSummary } from "../../services/cloudProfilesModel";
import { BottomSheetFrame } from "../ui/BottomSheetFrame";
import { CloudProfileNameEntry } from "./CloudProfileNameEntry";
import { CloudCodeEntry } from "./CloudCodeEntry";
import { DateStyle, formatDate } from "../../services/dateFormat";
import React, { useState } from "react";
import { Ionicons } from "@expo/vector-icons";
import * as Clipboard from "expo-clipboard";
import { ActivityIndicator, Linking, StyleSheet, Switch, View, type SwitchProps } from "react-native";
import { ContinuousCorners, useAppTheme } from "../../constants/tokens";
import { AppText, Button, EmptyState, InlineNotice, ListRow, ListSection, StatusPill } from "../ui";
import type { StatusTone } from "../ui/StatusPill";
import { accountStatus, type PluginAccount, type PluginIntegration } from "../../services/connections";
import { brainCanNow, type ConnectPhase, type PendingConnection } from "../../services/pluginOnboarding";
import {
  accessControl,
  accountRecovery,
  awsAccountSummary,
  connectLabel,
  toolUpdates,
  accountTone,
  allowedToolCount,
  canDisconnect,
  connectsWithForm,
  networkLabel,
  serviceOff,
  toolSections,
  toolsSwitch,
  type AccountRecoveryAction,
  type PluginCatalogRows,
  type ServiceRow,
  type ToolsSwitch,
} from "../../services/pluginConnectionsModel";
import { Icon, type IconName } from "../icons/Icon";
import { SignInAddressEntry } from "./SignInAddressEntry";
import { ServiceMark } from "./ServiceMarks";
import { hasServiceMark } from "./serviceMarkPaths";

type Job = { read: string; write: string; example: string } | undefined;

// Product marks keep their own logos; everything else is the app's icon set.
const SERVICE_LOGOS: Record<string, React.ComponentProps<typeof Ionicons>["name"]> = {
  github: "logo-github",
  slack: "logo-slack",
};

const SERVICE_ICONS: Record<string, IconName> = {
  mcp: "server",
  openapi: "code",
};

export function ServiceGlyph({ id, size = 30 }: { id: string; size?: number }) {
  const { colors, theme } = useAppTheme();
  return (
    <View
      accessible={false}
      importantForAccessibility="no-hide-descendants"
      style={[styles.glyph, { width: size, height: size, borderRadius: size * 0.3, backgroundColor: theme.materials.tint }]}
    >
      {SERVICE_LOGOS[id] ? (
        <Ionicons name={SERVICE_LOGOS[id]} size={Math.round(size * 0.6)} color={colors.textPrimary} />
      ) : hasServiceMark(id) ? (
        <ServiceMark id={id} size={Math.round(size * 0.56)} color={colors.textPrimary} />
      ) : (
        <Icon name={SERVICE_ICONS[id] ?? "puzzle"} size={Math.round(size * 0.6)} color={colors.textPrimary} />
      )}
    </View>
  );
}

/** Switches are ink, like every other control (DESIGN.md: Paper and ink). */
export function InkSwitch(props: SwitchProps) {
  const { colors } = useAppTheme();
  // react-native-web colours an on thumb with activeThumbColor, outside SwitchProps.
  const web = { activeThumbColor: colors.bgSurface, activeTrackColor: colors.accent } as object;
  return <Switch trackColor={{ false: colors.border, true: colors.accent }} thumbColor={colors.bgSurface} {...web} {...props} />;
}

export function NoServerState({ onOpenSettings }: { onOpenSettings(): void }) {
  return (
    <EmptyState
      icon="server"
      title="No current server"
      detail="Choose one in Settings."
      action={{ label: "Open Settings", onPress: onOpenSettings }}
    />
  );
}

/**
 * Mewla's own plugins lead Extensions: the heading every state sits under.
 * The agents' own plugins and Skills follow, quieter (AgentToolsSection).
 */
/**
 * The Plugins list: one row per service. A service with nothing connected
 * offers Connect right on its row, one that needs sign-in offers Reconnect,
 * and the row itself opens the service's page.
 */
export function CatalogView({
  rows,
  loading,
  error,
  connecting,
  onRetry,
  onOpen,
  onConnect,
  onReconnect,
}: {
  rows: PluginCatalogRows;
  loading: boolean;
  error: string;
  /** The service whose sign-in is starting or in progress. */
  connecting: string | null;
  onRetry(): void;
  onOpen(plugin: PluginIntegration): void;
  onConnect(plugin: PluginIntegration): void;
  onReconnect(account: PluginAccount): void;
}) {
  if (!rows.services.length && !rows.custom.length) {
    return error ? (
      <EmptyState icon="cloud-offline" tone="danger" title="Services unavailable" detail={error} action={{ label: "Try again", icon: "refresh", onPress: onRetry, loading }} />
    ) : loading ? (
      <EmptyState busy title="Loading services" />
    ) : (
      <EmptyState icon="puzzle" title="No services" detail="This server has none to connect." action={{ label: "Try again", icon: "refresh", onPress: onRetry }} />
    );
  }
  const row = (entry: ServiceRow) => (
    <ServiceListRow
      key={entry.plugin.id}
      row={entry}
      connecting={connecting === entry.plugin.id}
      disabled={connecting !== null && connecting !== entry.plugin.id}
      onOpen={() => onOpen(entry.plugin)}
      onConnect={() => onConnect(entry.plugin)}
      onReconnect={onReconnect}
    />
  );
  return (
    <>
      <ListSection title="Mewla plugins">{rows.services.map(row)}</ListSection>
      {rows.custom.length ? (
        <ListSection title="Your own services" footer="Any MCP server or OpenAPI service, by its address.">
          {rows.custom.map(row)}
        </ListSection>
      ) : null}
    </>
  );
}

function ServiceListRow({ row, connecting, disabled, onOpen, onConnect, onReconnect }: {
  row: ServiceRow;
  connecting: boolean;
  disabled: boolean;
  onOpen(): void;
  onConnect(): void;
  onReconnect(account: PluginAccount): void;
}) {
  const { plugin, accounts, state } = row;
  const custom = plugin.id === "mcp" || plugin.id === "openapi";
  const form = connectsWithForm(plugin.id);
  const names = plugin.id === "aws" || plugin.id === "gcp" ? cloudProfilesSummary(accounts) : accounts.length > 2 ? `${accounts.length} accounts` : accounts.map((account) => account.name).join(" · ");
  // Every account turned off: the row stays open, only quieter.
  const off = serviceOff(accounts);
  const subtitle = state.kind === "unavailable" ? plugin.unavailable_reason || "Sign-in is not set up yet." : plugin.id === "gcp" && !accounts.length ? "Nothing connected yet" : names || plugin.description;
  let trailing: React.ReactNode;
  let label = plugin.name;
  if (state.kind === "connect") {
    // A custom service needs its address first, on its own page.
    trailing = <Button label={custom ? "Add" : "Connect"} size="sm" variant="tinted" loading={connecting} disabled={disabled} onPress={form ? onOpen : onConnect} accessibilityLabel={`${custom ? "Add" : "Connect"} ${plugin.name}`} />;
  } else if (state.kind === "reconnect") {
    label = `${plugin.name}, ${state.account.name} needs sign-in again`;
    trailing = <Button label="Reconnect" size="sm" variant="tinted" loading={connecting} disabled={disabled} onPress={() => onReconnect(state.account)} accessibilityLabel={`Reconnect ${state.account.name}`} />;
  } else if (state.kind === "status") {
    label = `${plugin.name}, ${names}, ${state.label}`;
    trailing = connecting ? <ActivityIndicator /> : <StatusPill label={state.label} tone={state.tone} />;
  } else {
    label = `${plugin.name}, ${state.label}. ${subtitle}`;
    trailing = <StatusPill label={state.label} tone="neutral" />;
  }
  return (
    <ListRow
      leading={<View style={off ? styles.muted : undefined}><ServiceGlyph id={plugin.id} /></View>}
      title={plugin.name}
      subtitle={subtitle}
      subtitleNumberOfLines={1}
      trailing={trailing}
      accessory={state.kind === "unavailable" ? undefined : "chevron"}
      numberOfLines={2}
      disabled={state.kind === "unavailable"}
      accessibilityLabel={label}
      accessibilityHint={state.kind === "unavailable" ? undefined : "Opens the service"}
      onPress={state.kind === "unavailable" ? undefined : onOpen}
    />
  );
}

/**
 * A service changed its tools: one notice per account, confirmed with one
 * tap. Unchanged tools keep working meanwhile.
 */
export function ToolUpdatesNotice({ account, serviceName, running, onConfirm }: {
  account: PluginAccount;
  serviceName: string;
  running: string | null;
  onConfirm(names: string[]): void;
}) {
  const updates = toolUpdates(account, serviceName);
  if (!updates) return null;
  const confirming = running === `updates:${account.id}`;
  return (
    <InlineNotice
      tone="accent"
      icon="sync"
      busy={confirming}
      title={updates.title}
      detail={updates.detail}
      action={{ label: "Confirm", disabled: running !== null, onPress: () => onConfirm(updates.names) }}
    />
  );
}

/** Service identity and its combined state. */
export function ServiceHeader({ plugin, state }: { plugin: PluginIntegration; state?: { label: string; tone: StatusTone } | null }) {
  return (
    <View style={styles.serviceHeader}>
      <ServiceGlyph id={plugin.id} size={52} />
      <View style={styles.serviceCopy}>
        <View style={styles.serviceTitleRow}>
          <AppText variant="title" accessibilityRole="header" numberOfLines={1} style={styles.flexShrink}>
            {plugin.name}
          </AppText>
          {state ? <StatusPill label={state.label} tone={state.tone} /> : null}
        </View>
        {plugin.description ? (
          <AppText variant="compact" tone="secondary" numberOfLines={3}>
            {plugin.description}
          </AppText>
        ) : null}
      </View>
    </View>
  );
}

/**
 * The one sign-in in progress, or how the last one ended, at the top of
 * every Plugins page. Each state has one next step.
 */
export function ConnectStatusCard({
  serviceName,
  phase,
  flow,
  error,
  connected,
  job,
  starting,
  allowing,
  onOpen,
  onCancel,
  onRetry,
  onDismiss,
  onAllowChanges,
  onChooseTools,
  onUseServerAccount,
  onImport,
  onSubmitAddress,
  onRename,
}: {
  /** Null for a return that names no service (a stale or replayed sign-in). */
  serviceName: string | null;
  phase: ConnectPhase;
  flow: PendingConnection | null;
  error: string;
  connected: PluginAccount | null;
  job: Job;
  /** The connect action for this service is starting. */
  starting: boolean;
  /** Changes are being allowed for the connected account. */
  allowing: boolean;
  onOpen(): void;
  onCancel(): void;
  onRetry(): void;
  onDismiss(): void;
  onAllowChanges(account: PluginAccount): void;
  onChooseTools(account: PluginAccount): void;
  onUseServerAccount?(): void;
  onImport(): void;
  /** The address a sign-in ended on, pasted back when the browser could not reach this server. */
  onSubmitAddress(address: string): Promise<void>;
  onRename?(account: PluginAccount, name: string): Promise<boolean>;
}) {
  const { colors } = useAppTheme();
  if (serviceName === null) {
    return error ? (
      <StatusCard glyph="info-fill" title="That sign-in already ended" detail={error}>
        <Button label="OK" variant="plain" block onPress={onDismiss} />
      </StatusCard>
    ) : null;
  }
  if (flow?.flow.install_required && flow.flow.status === "waiting") {
    const install = flow.flow;
    return <StatusCard title={install.message || `Install ${serviceName} CLI`} detail={install.message ? undefined : "Install the official CLI on this computer, then sign in."}>
      {install.install_manual && install.install_url ? <Button label={`Download ${serviceName} CLI`} icon="open-external" variant="plain" block onPress={() => void Linking.openURL(install.install_url!)} /> : null}
      <Button label={install.install_manual ? "Check again" : install.install_failed ? "Retry" : "Install and sign in"} variant="filled" block loading={starting || phase === "verifying"} onPress={() => void onSubmitAddress("")} />
      <Button label="Cancel" variant="plain" block onPress={onCancel} />
    </StatusCard>;
  }
  if (flow?.flow.installing && flow.flow.status === "waiting") return <StatusCard busy title={`Installing ${serviceName} CLI…`}>
    <Button label="Cancel" variant="plain" block onPress={onCancel} />
  </StatusCard>;
  if (flow?.flow.code_required && (phase === "waiting" || phase === "verifying")) return <StatusCard title={`Sign in to ${serviceName}`} detail={`Open ${serviceName}, sign in, then paste the code shown there.`}>
    <CloudCodeEntry key={flow.flow.id} serviceName={serviceName} onSubmit={onSubmitAddress} />
    <Button label={`Open ${serviceName} again`} icon="open-external" variant="plain" block onPress={onOpen} />
    <Button label="Cancel" variant="plain" block onPress={onCancel} />
  </StatusCard>;
  const code = flow?.flow.user_code;
  const identity = flow?.flow.status === "confirm" ? flow.flow.identity || `${serviceName} account` : null;
  if (identity !== null) {
    return (
      <StatusCard title={`Use ${identity}?`} detail={`The GitHub login on this server. Mewla keeps its own copy; later sign-ins there don't change it.`} glyph="person-circle">
        <Button label={`Connect ${identity}`} variant="tinted" block loading={starting} onPress={onImport} />
        <Button label="Cancel" variant="plain" block onPress={onCancel} />
      </StatusCard>
    );
  }
  if (phase === "opening") return <StatusCard busy title={`Opening ${serviceName}…`} />;
  if ((phase === "waiting" || phase === "verifying") && flow?.flow.address_required) {
    return (
      <StatusCard title={`Finish signing in to ${serviceName}`} detail="After sign-in, a page starting with http://127.0.0.1 may fail to load on this computer. Copy its full address, paste it below, then choose Continue.">
        <SignInAddressEntry key={flow.flow.id} onSubmit={onSubmitAddress} />
        <Button label={`Open ${serviceName} again`} icon="open-external" variant="plain" block disabled={phase === "verifying"} onPress={onOpen} />
        <Button label="Cancel" variant="plain" block onPress={onCancel} />
      </StatusCard>
    );
  }
  if (phase === "verifying") return <StatusCard busy title={`Checking your ${serviceName} account…`} detail="This takes a moment." />;
  if (phase === "waiting" && code) {
    return (
      <StatusCard busy title={`Enter this code on ${serviceName}`} detail="Mewla copies it for you. This updates when you're done.">
        <AppText variant="monoStrong" selectable style={[styles.code, { color: colors.textPrimary }]} accessibilityLabel={`Code ${code.split("").join(" ")}`}>
          {code}
        </AppText>
        <Button label={`Copy code and open ${serviceName}`} icon="open-external" variant="filled" block onPress={onOpen} />
        {onUseServerAccount ? <Button label="Use the GitHub login on this server" variant="plain" block onPress={onUseServerAccount} /> : null}
        <Button label="Cancel" variant="plain" block onPress={onCancel} />
      </StatusCard>
    );
  }
  if (phase === "waiting") {
    return (
      <StatusCard busy title={`Waiting for ${serviceName}`} detail={flow?.flow.message || error || undefined}>
        <Button label={`Open ${serviceName} again`} icon="open-external" variant="tinted" block onPress={onOpen} />
        <Button label="Cancel" variant="plain" block onPress={onCancel} />
      </StatusCard>
    );
  }
  if (phase === "connected" && connected && connected.access?.read === "none" && connected.access.write === "none") {
    // A custom service: nothing is allowed until its tools are chosen.
    return (
      <StatusCard glyph="check-circle-fill" glyphColor={colors.success} title={`${connected.name} connected`} detail="Choose which of its tools Brain may use. None are allowed yet.">
        <Button label="Choose tools" variant="tinted" block onPress={() => onChooseTools(connected)} />
        <Button label="Later" variant="plain" block onPress={onDismiss} />
      </StatusCard>
    );
  }
  if (phase === "connected" && connected) {
    const changes = accessControl(connected, "write");
    const changesOn = changes.kind === "switch" && changes.on;
    return (
      <StatusCard glyph="check-circle-fill" glyphColor={colors.success} title={`${serviceName} connected`} detail={`${connected.access?.read === "none" ? `Brain can use ${connected.name} once you allow changes.` : brainCanNow(job?.read, connected.name)}${changes.kind === "consent" ? ` To let it make changes too, ${serviceName} asks you once more.` : ""}`}>
        {changes.kind !== "none" && !changesOn ? (
          <Button label="Allow changes too" variant="tinted" block loading={allowing} onPress={() => onAllowChanges(connected)} accessibilityHint={job?.write} />
        ) : null}
        {connected.integration === "aws" && connected.profile && onRename ? <CloudProfileNameEntry key={connected.id} account={connected} suggest onSave={async (name) => { const saved = await onRename(connected, name); if (saved) onDismiss(); return saved; }} /> : null}
        <Button label="Done" variant="plain" block onPress={onDismiss} />
      </StatusCard>
    );
  }
  if ((phase === "failed" || phase === "cancelled") && error) {
    return (
      <StatusCard glyph={phase === "failed" ? "alert-circle-fill" : "info-fill"} glyphColor={phase === "failed" ? colors.dangerText : colors.textSecondary} title={phase === "failed" ? `Couldn't connect ${serviceName}` : "Sign-in cancelled"} detail={error}>
        <Button label="Start again" icon="refresh" variant="tinted" block loading={starting} onPress={onRetry} />
        <Button label="Dismiss" variant="plain" block onPress={onDismiss} />
      </StatusCard>
    );
  }
  return null;
}

function StatusCard({ title, detail, busy, glyph, glyphColor, children }: {
  title: string;
  detail?: string;
  busy?: boolean;
  glyph?: IconName;
  glyphColor?: string;
  children?: React.ReactNode;
}) {
  const { colors } = useAppTheme();
  return (
    <ListSection>
      <View style={styles.card} accessibilityLiveRegion="polite">
        <View style={styles.cardTitle}>
          {busy ? <ActivityIndicator color={colors.textSecondary} /> : glyph ? <Icon name={glyph} size={20} color={glyphColor ?? colors.textSecondary} /> : null}
          <AppText variant="heading" accessibilityRole="header" style={styles.flexShrink}>{title}</AppText>
        </View>
        {detail ? <AppText variant="compact" tone="secondary">{detail}</AppText> : null}
        {children ? <View style={styles.cardActions}>{children}</View> : null}
      </View>
    </ListSection>
  );
}

/** A service with nothing connected: what Brain gets, and the one way to connect. */
export function ConnectOffer({ plugin, job, serverName, connecting, disabled, onConnect, onUseServerAccount }: {
  plugin: PluginIntegration;
  job: Job;
  serverName: string;
  connecting: boolean;
  disabled: boolean;
  onConnect(): void;
  onUseServerAccount?(): void;
}) {
  if (!plugin.available) {
    return (
      <ListSection>
        <ListRow icon="info" title="Not yet available" subtitle={plugin.unavailable_reason || "Sign-in is not set up yet."} numberOfLines={3} />
      </ListSection>
    );
  }
  return (
    <>
      <ListSection title="What Brain can do" footer={`Your sign-in stays on ${serverName}. Brain only acts on tasks you give it.`}>
        <ListRow icon="eye" title="Read and search" subtitle={job?.read} numberOfLines={3} value="On" />
        <ListRow icon="edit" title="Make changes when asked" subtitle={job ? `${job.write}. Off until you allow it.` : "Off until you allow it."} numberOfLines={3} value="Off" />
      </ListSection>
      <View style={styles.actions}>
        <Button label={connectLabel(plugin)} variant="filled" size="lg" block loading={connecting} disabled={disabled} onPress={onConnect} />
        {onUseServerAccount ? <Button label="Use the GitHub login on this server" variant="plain" block disabled={disabled || connecting} onPress={onUseServerAccount} /> : null}
      </View>
    </>
  );
}

/**
 * One connected account: its state and on/off switch, the one fix if it
 * needs one, what Brain may do, its tools, and Disconnect with a single
 * inline confirm.
 */
export function AccountCard({
  plugin,
  account,
  job,
  running,
  onAccess,
  onRecover,
  onSetEnabled,
  onDisconnect,
  onRemoveKey,
  onOpenTools,
  onRename,
}: {
  plugin: PluginIntegration;
  account: PluginAccount;
  job: Job;
  running: string | null;
  onAccess(group: "read" | "write", allowed: boolean): void;
  onRecover(action: AccountRecoveryAction): void;
  /** Off hides every tool from Brain and Workers; the sign-in and tool choices stay. */
  onSetEnabled(on: boolean): void;
  onDisconnect(): Promise<boolean>;
  /** Hyperliquid: delete the saved API wallet key; the account stays, read only. */
  onRemoveKey?(): Promise<boolean>;
  onOpenTools(): void;
  onRename?(name: string): Promise<boolean>;
}) {
  const [renaming, setRenaming] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [confirmingKey, setConfirmingKey] = useState(false);
  const [showDetails, setShowDetails] = useState(false);
  const [copied, setCopied] = useState<string | null>(null);
  const aws = awsAccountSummary(account);
  const network = account.network === "testnet" ? networkLabel(account.network) : null;
  const removingKey = running === `key:${account.id}`;
  const recovery = accountRecovery(account, plugin.name);
  const tools = allowedToolCount(account);
  const custom = plugin.id === "mcp" || plugin.id === "openapi";
  const busy = running !== null;
  const recovering = running === `recover:${account.id}` || recovery?.action === "reconnect" && running === `connect:${plugin.id}` || recovery?.action === "disconnect" && running === `disconnect:${account.id}`;
  const removing = running === `disconnect:${account.id}`;
  const group = (name: "read" | "write") => {
    const control = accessControl(account, name);
    const title = name === "read" ? "Read and search" : "Make changes when asked";
    const describe = name === "read" ? job?.read : job?.write;
    if (control.kind === "none") return null;
    const pending = running === `${name}:${account.id}` || name === "write" && control.kind === "consent" && running === `connect:${plugin.id}`;
    const trailing = pending ? <ActivityIndicator /> : control.kind === "consent" ? (
      <Button label="Allow" size="sm" variant="tinted" disabled={busy} onPress={() => onAccess(name, true)} accessibilityLabel={`Allow changes in ${plugin.name}`} />
    ) : (
      <InkSwitch accessibilityLabel={title} value={control.on} disabled={busy} onValueChange={(allowed) => onAccess(name, allowed)} />
    );
    const note = control.kind === "consent" ? `${plugin.name} asks you to approve this once.` : control.note;
    return <ListRow key={name} icon={name === "read" ? "eye" : "edit"} title={title} subtitle={[describe, note].filter(Boolean).join(". ")} numberOfLines={4} trailing={trailing} />;
  };
  return (
    <ListSection>
      <ListRow
        icon="person-circle"
        title={account.name}
        subtitle={account.profile ? [account.profile_kind, account.profile_status, account.active ? "Active" : "", account.project, account.integration === "gcp" ? account.identity : "", account.aws?.account_id, account.aws?.account_name].filter(Boolean).join(" · ") : aws ? aws.subtitle || undefined : [custom ? endpointHost(account.endpoint) : account.identity !== account.name ? account.identity : "", account.verified_at ? `Checked ${formatDate(account.verified_at, DateStyle.dateTime)}` : ""].filter(Boolean).join(" · ") || undefined}
        numberOfLines={2}
        disabled={!account.enabled}
        trailing={
          <View style={styles.accountControls}>
            {/* A fix row below names the state itself; the pill would repeat it. */}
            {/* Mainnet is the default and needs no tag; testnet always has one, so it is never taken for mainnet. */}
            {network ? <StatusPill label={network} tone={account.network === "testnet" ? "warning" : "accent"} /> : null}
            {recovery ? null : <StatusPill label={accountStatus(account)} tone={accountTone(account)} />}
            {running === `enabled:${account.id}` ? <ActivityIndicator /> : (
              <InkSwitch accessibilityLabel={`${plugin.name} ${account.name} on`} value={account.enabled} disabled={busy} onValueChange={onSetEnabled} />
            )}
          </View>
        }
      />
      {account.integration === "aws" && account.profile && onRename ? renaming ? <CloudProfileNameEntry key={account.id} account={account} onSave={async (name) => { const saved = await onRename(name); if (saved) setRenaming(false); return saved; }} /> : <ListRow title="Rename profile" icon="edit" onPress={() => setRenaming(true)} /> : null}
      {account.profile_sign_in_by ? <ListRow icon="time" title="Sign in again by" subtitle={`${formatDate(account.profile_sign_in_by, DateStyle.fullDateTime, { timeZoneName: "short" })} · ${account.profile_sign_in_limit === "maximum" ? "At latest · AWS allows up to 12h; your session may end sooner." : "SSO session"}`} numberOfLines={3} /> : null}
      {account.profile ? <ListRow icon="person-circle" title="Sign in again" subtitle={`Use ${plugin.name} remote sign-in for this ${account.integration === "gcp" ? "account" : "profile"}.`} trailing={<Button label="Sign in again" variant="tinted" size="sm" disabled={busy} onPress={() => onRecover("reconnect")} />} /> : null}
      {!account.profile && recovery ? (
        <ListRow
          icon={recovery.tone === "neutral" ? "pause-circle" : "alert-circle"}
          iconColor={undefined}
          title={recovery.title}
          subtitle={recovery.detail}
          numberOfLines={3}
          trailing={<Button label={recovery.actionLabel} size="sm" variant="tinted" loading={recovering} disabled={busy && !recovering} onPress={() => onRecover(recovery.action)} />}
        />
      ) : null}
      {aws ? (
        <>
          <ListRow icon="info" title="Details" accessory={showDetails ? undefined : "chevron"} onPress={() => setShowDetails(!showDetails)} />
          {showDetails ? aws.details.map((row) => (
            <ListRow
              key={row.label}
              title={row.label}
              subtitle={row.value}
              numberOfLines={3}
              value={copied === row.label ? "Copied" : undefined}
              onPress={row.copy ? () => { void Clipboard.setStringAsync(row.value); setCopied(row.label); } : undefined}
              accessibilityLabel={row.copy ? `Copy ${row.label}` : undefined}
            />
          )) : null}
        </>
      ) : null}
      {custom ? null : [group("read"), group("write")]}
      <ListRow
        icon="list"
        title={custom ? `${tools.allowed} of ${tools.total} tools allowed` : "Tools & activity"}
        subtitle={custom && tools.allowed === 0 ? "Choose which tools Brain may use." : undefined}
        value={custom || !tools.total ? undefined : `${tools.allowed}/${tools.total}`}
        accessory="chevron"
        onPress={onOpenTools}
      />
      {onRemoveKey && account.signer ? confirmingKey ? (
        <View style={styles.confirm}>
          <AppText variant="compact" tone="secondary">
            Remove the trading key from {account.name}? Brain can still read it, and can no longer place or cancel orders. Mewla deletes the saved key.
          </AppText>
          <View style={styles.confirmActions}>
            <Button label="Keep" variant="plain" size="sm" disabled={removingKey} onPress={() => setConfirmingKey(false)} />
            <Button label="Remove key" variant="destructive" size="sm" loading={removingKey} disabled={busy && !removingKey} onPress={() => void onRemoveKey().then(() => setConfirmingKey(false))} />
          </View>
        </View>
      ) : (
        <ListRow icon="key" title="Remove trading key" subtitle={`API wallet ${account.signer.slice(0, 6)}…${account.signer.slice(-4)}`} destructive disabled={busy} onPress={() => setConfirmingKey(true)} />
      ) : null}
      {confirming ? (
        <BottomSheetFrame visible onClose={() => { if (!removing) setConfirming(false); }}><View style={styles.confirm}>
          <AppText variant="compact" tone="secondary">
            {account.integration === "gcp" ? `Delete ${account.identity || account.name} from this computer? This signs it out of gcloud and removes its configurations. Other accounts are kept.` : account.profile ? `Delete profile ${account.profile}${account.aws?.account_id ? ` (${account.aws.account_id})` : ""} from this computer? It will also disappear from the AWS CLI. A backup is kept.` : `Disconnect ${account.name}? Brain loses access right away, and Mewla removes its saved sign-in.`}
          </AppText>
          <View style={styles.confirmActions}>
            <Button label="Keep" variant="plain" size="sm" disabled={removing} onPress={() => setConfirming(false)} />
            <Button label={account.integration === "gcp" ? "Delete account" : account.profile ? "Delete profile" : "Disconnect"} variant="destructive" size="sm" loading={removing} disabled={busy && !removing} onPress={() => void onDisconnect().then(() => setConfirming(false))} />
          </View>
        </View></BottomSheetFrame>
      ) : !account.profile && !canDisconnect(account) ? null : (
        <ListRow icon="remove-circle" title={account.integration === "gcp" ? "Delete account" : account.profile ? "Delete profile" : "Disconnect"} destructive disabled={busy} onPress={() => setConfirming(true)} />
      )}
    </ListSection>
  );
}

/**
 * An account's tools: its on/off switch, then one list of the tools led by a
 * switch over all of them, each group under a row with its own switch. Every
 * switch sits in one column and takes effect on the tap, with no confirm.
 */
export function ToolsView({
  account,
  running,
  onToggleEnabled,
  onRefresh,
  onSetTools,
  onAllowChanges,
}: {
  account: PluginAccount;
  running: string | null;
  onToggleEnabled(enabled: boolean): void;
  onRefresh(): void;
  /** One request for one tool or many; the server applies all of them or none. */
  onSetTools(names: string[], allowed: boolean): void;
  /** Changes the service has to approve go through its sign-in. */
  onAllowChanges(): void;
}) {
  const tools = account.tools ?? [];
  const busy = running !== null;
  // A tool request in flight already shows its result, so it doesn't grey the switches out.
  const locked = busy && running !== `tools:${account.id}`;
  const sections = toolSections(account);
  const allowed = tools.filter((tool) => tool.allowed).length;
  const all = toolsSwitch(account, tools);
  const flip = (state: ToolsSwitch) => {
    if (state.tap === "set") onSetTools(state.names, !state.on);
    else if (state.tap === "consent") onAllowChanges();
  };
  return (
    <>
      <ListSection title={account.name}>
        <ListRow
          icon="power"
          title="On"
          subtitle="Brain and Workers can use this account."
          trailing={running === `enabled:${account.id}` ? <ActivityIndicator /> : <InkSwitch accessibilityLabel="Account on" value={account.enabled} disabled={busy} onValueChange={onToggleEnabled} />}
        />
        <ListRow
          icon="pulse"
          title="Check service and tools"
          subtitle={account.verified_at ? `Last checked ${formatDate(account.verified_at, DateStyle.dateTime)}` : undefined}
          loading={running === `recover:${account.id}`}
          disabled={busy}
          onPress={onRefresh}
        />
      </ListSection>
      {tools.length ? (
        <ListSection title="Tools">
          <ListRow
            title="All tools"
            subtitle={`${allowed} of ${tools.length} allowed`}
            leading={<View style={styles.disclosureSpace} />}
            trailing={<InkSwitch accessibilityLabel="All tools" value={all.on} disabled={locked || all.tap === "none"} onValueChange={() => flip(all)} />}
          />
          {sections.flatMap((section) => {
            const state = toolsSwitch(account, section.tools, section.consent);
            const rows = section.tools.map((tool) => <ToolRow key={tool.name} tool={tool} busy={locked} onToggle={(value) => onSetTools([tool.name], value)} />);
            // A single group is the whole list; All tools already covers it.
            return sections.length > 1 ? [
              <GroupRow key={section.key} title={section.title} count={`${section.allowed}/${section.tools.length}`}>
                <InkSwitch accessibilityLabel={`All of ${section.title}`} value={state.on} disabled={locked || state.tap === "none"} onValueChange={() => flip(state)} />
              </GroupRow>,
              ...rows,
            ] : rows;
          })}
        </ListSection>
      ) : (
        <ListSection title="Tools">
          <ListRow title="No tools reported" />
        </ListSection>
      )}
      <ListSection title="Recent calls">
        {account.history.length ? (
          account.history.map((event, index) => (
            <ListRow
              key={`${event.at}-${index}`}
              title={event.tool}
              subtitle={event.message || formatDate(event.at, DateStyle.dateTime)}
              trailing={<StatusPill label={event.status} tone={callTone(event.status)} />}
              numberOfLines={2}
            />
          ))
        ) : (
          <ListRow title="No calls yet" />
        )}
      </ListSection>
    </>
  );
}

/** A group's head inside the tools list: its name, count and switch, in the tools' column. */
function GroupRow({ title, count, children }: { title: string; count: string; children: React.ReactNode }) {
  return (
    <View style={styles.groupRow} accessible={false}>
      <View style={styles.disclosureSpace} />
      <AppText variant="label" tone="tertiary" accessibilityRole="header" numberOfLines={1} style={styles.flex}>{`${title} · ${count}`}</AppText>
      {children}
    </View>
  );
}

/** A custom service is known by its address; its account has no identity to show. */
function endpointHost(endpoint?: string): string {
  try { return endpoint ? new URL(endpoint).host : ""; } catch { return ""; }
}

function callTone(status: string): StatusTone {
  const value = status.toLowerCase();
  if (value === "ok" || value === "success" || value === "succeeded") return "success";
  if (value.includes("fail") || value.includes("error") || value.includes("denied")) return "danger";
  return "neutral";
}

type ToolInput = { name: string; type: string; required: boolean; description?: string };

/** The schema's top-level properties, in declared order, as one line each. */
function toolInputs(schema: Record<string, unknown> | undefined): ToolInput[] {
  const properties = schema?.properties;
  if (!properties || typeof properties !== "object") return [];
  const required = new Set(Array.isArray(schema?.required) ? (schema.required as string[]) : []);
  return Object.entries(properties as Record<string, Record<string, unknown>>).map(([name, prop]) => {
    const type = Array.isArray(prop?.enum) ? (prop.enum as unknown[]).map(String).join(" | ") : Array.isArray(prop?.type) ? (prop.type as string[]).join(" | ") : typeof prop?.type === "string" ? prop.type : "any";
    return { name, type, required: required.has(name), description: typeof prop?.description === "string" ? prop.description : undefined };
  });
}

// The row itself discloses its inputs; the switch stays the only control on it.
function ToolRow({ tool, busy, onToggle }: { tool: NonNullable<PluginAccount["tools"]>[number]; busy: boolean; onToggle(allowed: boolean): void }) {
  const { colors } = useAppTheme();
  const [expanded, setExpanded] = useState(false);
  const inputs = toolInputs(tool.input_schema);
  const description = tool.description.replace(/\s+/g, " ").trim();
  // The row clamps to two lines; expanding is how the full text is read.
  const expandable = inputs.length > 0 || description.length > 120;
  return (
    <View>
      <ListRow
        title={tool.name}
        // One paragraph, so a description's own line breaks don't grow the row.
        subtitle={`${tool.updated ? "Updated, waits for your confirmation · " : ""}${description}`}
        numberOfLines={expanded ? undefined : 2}
        leading={expandable ? <Icon name={expanded ? "chevron-down" : "chevron-right"} size={15} color={colors.textTertiary} /> : <View style={styles.disclosureSpace} />}
        onPress={expandable ? () => setExpanded(!expanded) : undefined}
        accessibilityLabel={expandable ? `${tool.name}, ${inputs.length} input${inputs.length === 1 ? "" : "s"}` : undefined}
        accessibilityHint={expandable ? (expanded ? "Hides details" : "Shows details") : undefined}
        trailing={<InkSwitch accessibilityLabel={`Allow ${tool.name}`} value={tool.allowed} disabled={busy} onValueChange={onToggle} />}
      />
      {expanded ? (
        <View style={[styles.inputs, { borderColor: colors.borderSubtle }]}>
          {inputs.map((input) => (
            <View key={input.name} style={styles.input}>
              <View style={styles.inputHead}>
                <AppText variant="monoStrong" selectable>{input.name}</AppText>
                <AppText variant="mono" tone="tertiary" numberOfLines={1} style={styles.flexShrink}>{input.type}</AppText>
                {input.required ? <AppText variant="caption" tone="secondary">required</AppText> : null}
              </View>
              {input.description ? <AppText variant="caption" tone="tertiary" selectable>{input.description}</AppText> : null}
            </View>
          ))}
        </View>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  glyph: { alignItems: "center", justifyContent: "center", ...ContinuousCorners },
  serviceHeader: { flexDirection: "row", alignItems: "center", gap: 14, paddingHorizontal: 4 },
  serviceCopy: { flex: 1, minWidth: 0, gap: 4 },
  serviceTitleRow: { flexDirection: "row", alignItems: "center", gap: 10, flexWrap: "wrap" },
  flexShrink: { flexShrink: 1 },
  muted: { opacity: 0.45 },
  accountControls: { flexDirection: "row", alignItems: "center", gap: 10 },
  card: { paddingHorizontal: 16, paddingVertical: 14, gap: 8 },
  cardTitle: { flexDirection: "row", alignItems: "center", gap: 10 },
  cardActions: { gap: 4, paddingTop: 4 },
  code: { fontSize: 26, lineHeight: 34, letterSpacing: 2, paddingVertical: 4 },
  actions: { gap: 6, marginBottom: 26 },
  confirm: { paddingHorizontal: 16, paddingVertical: 12, gap: 8 },
  confirmActions: { flexDirection: "row", justifyContent: "flex-end", gap: 8 },
  disclosureSpace: { width: 15 },
  flex: { flex: 1, minWidth: 0 },
  groupRow: { flexDirection: "row", alignItems: "center", gap: 12, paddingHorizontal: 16, paddingTop: 14, paddingBottom: 8 },
  inputs: { marginLeft: 43, marginRight: 16, marginBottom: 12, paddingLeft: 12, gap: 10, borderLeftWidth: StyleSheet.hairlineWidth },
  input: { gap: 2 },
  inputHead: { flexDirection: "row", alignItems: "baseline", gap: 8, flexWrap: "wrap" },
});
