import React, { useState } from "react";
import { Ionicons } from "@expo/vector-icons";
import { ActivityIndicator, StyleSheet, Switch, View, type SwitchProps } from "react-native";
import { ContinuousCorners, Radii, useAppTheme } from "../../constants/tokens";
import { AppText, Button, EmptyState, ListRow, ListSection, StatusPill } from "../ui";
import type { StatusTone } from "../ui/StatusPill";
import { accountStatus, type PluginAccount, type PluginIntegration } from "../../services/connections";
import type { ConnectPhase, PendingConnection } from "../../services/pluginOnboarding";
import {
  accessControl,
  accountRecovery,
  accountTone,
  allowedToolCount,
  type AccountRecoveryAction,
  type PluginCatalogRows,
  type ServiceRow,
} from "../../services/pluginConnectionsModel";
import { Icon, type IconName } from "../icons/Icon";
import { ServiceMark, hasServiceMark } from "./ServiceMarks";

type Job = { read: string; write: string; example: string } | undefined;

// Product marks keep their own logos; everything else is the app's icon set.
const SERVICE_LOGOS: Record<string, React.ComponentProps<typeof Ionicons>["name"]> = {
  github: "logo-github",
  slack: "logo-slack",
  google: "logo-google",
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
      <ListSection title="Services">{rows.services.map(row)}</ListSection>
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
  const names = accounts.map((account) => account.name).join(" · ");
  const subtitle = state.kind === "unavailable" ? plugin.unavailable_reason || "Sign-in is not set up yet." : names || plugin.description;
  let trailing: React.ReactNode;
  let label = plugin.name;
  if (state.kind === "connect") {
    // A custom service needs its address first, on its own page.
    trailing = <Button label={custom ? "Add" : "Connect"} size="sm" variant="tinted" loading={connecting} disabled={disabled} onPress={custom ? onOpen : onConnect} accessibilityLabel={`${custom ? "Add" : "Connect"} ${plugin.name}`} />;
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
      leading={<ServiceGlyph id={plugin.id} />}
      title={plugin.name}
      subtitle={subtitle}
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
}) {
  const { colors } = useAppTheme();
  if (serviceName === null) {
    return error ? (
      <StatusCard glyph="info-fill" title="That sign-in already ended" detail={error}>
        <Button label="OK" variant="plain" block onPress={onDismiss} />
      </StatusCard>
    ) : null;
  }
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
      <StatusCard busy title={`Waiting for ${serviceName}`} detail={`Finish signing in on ${serviceName}'s page. This updates when you're back.`}>
        <Button label={`Open ${serviceName} again`} icon="open-external" variant="tinted" block onPress={onOpen} />
        <Button label="Cancel" variant="plain" block onPress={onCancel} />
      </StatusCard>
    );
  }
  if (phase === "connected" && connected && connected.access?.read === "none") {
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
      <StatusCard glyph="check-circle-fill" glyphColor={colors.success} title={`${serviceName} connected`} detail={`Brain can read and search ${connected.name}.${changes.kind === "consent" ? ` To let it make changes too, ${serviceName} asks you once more.` : ""}`}>
        {changes.kind !== "none" && !changesOn ? (
          <Button label="Allow changes too" variant="tinted" block loading={allowing} onPress={() => onAllowChanges(connected)} accessibilityHint={job?.write} />
        ) : null}
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
        <Button label={`Connect ${plugin.name}`} variant="filled" size="lg" block loading={connecting} disabled={disabled} onPress={onConnect} />
        {onUseServerAccount ? <Button label="Use the GitHub login on this server" variant="plain" block disabled={disabled || connecting} onPress={onUseServerAccount} /> : null}
      </View>
    </>
  );
}

/**
 * One connected account: its state and the one fix if it needs one, what
 * Brain may do, its tools, and Disconnect with a single inline confirm.
 */
export function AccountCard({
  plugin,
  account,
  job,
  running,
  onAccess,
  onRecover,
  onDisconnect,
  onOpenTools,
}: {
  plugin: PluginIntegration;
  account: PluginAccount;
  job: Job;
  running: string | null;
  onAccess(group: "read" | "write", allowed: boolean): void;
  onRecover(action: AccountRecoveryAction): void;
  onDisconnect(): Promise<boolean>;
  onOpenTools(): void;
}) {
  const [confirming, setConfirming] = useState(false);
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
        subtitle={[custom ? endpointHost(account.endpoint) : account.identity !== account.name ? account.identity : "", account.verified_at ? `Checked ${new Date(account.verified_at).toLocaleString()}` : ""].filter(Boolean).join(" · ") || undefined}
        numberOfLines={2}
        trailing={<StatusPill label={accountStatus(account)} tone={accountTone(account)} />}
      />
      {recovery ? (
        <ListRow
          icon={recovery.tone === "neutral" ? "pause-circle" : "alert-circle"}
          iconColor={undefined}
          title={recovery.title}
          subtitle={recovery.detail}
          numberOfLines={3}
          trailing={<Button label={recovery.actionLabel} size="sm" variant="tinted" loading={recovering} disabled={busy && !recovering} onPress={() => onRecover(recovery.action)} />}
        />
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
      {confirming ? (
        <View style={styles.confirm}>
          <AppText variant="compact" tone="secondary">
            Disconnect {account.name}? Brain loses access right away, and Mewla removes its saved sign-in.
          </AppText>
          <View style={styles.confirmActions}>
            <Button label="Keep" variant="plain" size="sm" disabled={removing} onPress={() => setConfirming(false)} />
            <Button label="Disconnect" variant="destructive" size="sm" loading={removing} disabled={busy && !removing} onPress={() => void onDisconnect().then((done) => { if (!done) setConfirming(false); })} />
          </View>
        </View>
      ) : (
        <ListRow icon="remove-circle" title="Disconnect" destructive disabled={busy} onPress={() => setConfirming(true)} />
      )}
    </ListSection>
  );
}

export function ToolsView({
  account,
  running,
  onToggleEnabled,
  onRefresh,
  onToggleTool,
}: {
  account: PluginAccount;
  running: string | null;
  onToggleEnabled(enabled: boolean): void;
  onRefresh(): void;
  onToggleTool(tool: NonNullable<PluginAccount["tools"]>[number], allowed: boolean): void;
}) {
  const tools = account.tools ?? [];
  const busy = running !== null;
  return (
    <>
      <ListSection title={account.name}>
        <ListRow
          icon="power"
          title="On"
          subtitle="Brain and Workers can use this account."
          trailing={running === `recover:${account.id}` ? <ActivityIndicator /> : <InkSwitch accessibilityLabel="Account on" value={account.enabled} disabled={busy} onValueChange={onToggleEnabled} />}
        />
        <ListRow
          icon="pulse"
          title="Check service and tools"
          subtitle={account.verified_at ? `Last checked ${new Date(account.verified_at).toLocaleString()}` : undefined}
          loading={running === `recover:${account.id}`}
          disabled={busy}
          onPress={onRefresh}
        />
      </ListSection>
      <ListSection title={`Tools (${tools.length})`} footer="A tool that changes needs permission again.">
        {tools.length ? (
          tools.map((tool) => <ToolRow key={tool.name} tool={tool} busy={busy} pending={running === `tool:${account.id}:${tool.name}`} onToggle={(allowed) => onToggleTool(tool, allowed)} />)
        ) : (
          <ListRow title="No tools reported" />
        )}
      </ListSection>
      <ListSection title="Recent calls">
        {account.history.length ? (
          account.history.map((event, index) => (
            <ListRow
              key={`${event.at}-${index}`}
              title={event.tool}
              subtitle={event.message || new Date(event.at).toLocaleString()}
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

function ToolRow({ tool, busy, pending, onToggle }: { tool: NonNullable<PluginAccount["tools"]>[number]; busy: boolean; pending: boolean; onToggle(allowed: boolean): void }) {
  const { colors } = useAppTheme();
  const [expanded, setExpanded] = useState(false);
  return (
    <View>
      <ListRow
        title={tool.name}
        subtitle={tool.description}
        numberOfLines={2}
        trailing={pending ? <ActivityIndicator /> : <InkSwitch accessibilityLabel={`Allow ${tool.name}`} value={tool.allowed} disabled={busy} onValueChange={onToggle} />}
      />
      <View style={styles.schema}>
        <Button
          label={expanded ? "Hide inputs" : "Inspect inputs"}
          accessibilityLabel={`${expanded ? "Hide" : "Inspect"} inputs for ${tool.name}`}
          accessibilityState={{ expanded }}
          size="sm"
          variant="plain"
          icon={expanded ? "chevron-up" : "chevron-down"}
          onPress={() => setExpanded(!expanded)}
        />
        {expanded ? (
          <View style={[styles.schemaBody, { backgroundColor: colors.surfaceSubtle }]}>
            <AppText variant="mono" selectable>
              {JSON.stringify(tool.input_schema ?? {}, null, 2)}
            </AppText>
          </View>
        ) : null}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  glyph: { alignItems: "center", justifyContent: "center", ...ContinuousCorners },
  serviceHeader: { flexDirection: "row", alignItems: "center", gap: 14, paddingHorizontal: 4 },
  serviceCopy: { flex: 1, minWidth: 0, gap: 4 },
  serviceTitleRow: { flexDirection: "row", alignItems: "center", gap: 10, flexWrap: "wrap" },
  flexShrink: { flexShrink: 1 },
  card: { paddingHorizontal: 16, paddingVertical: 14, gap: 8 },
  cardTitle: { flexDirection: "row", alignItems: "center", gap: 10 },
  cardActions: { gap: 4, paddingTop: 4 },
  code: { fontSize: 26, lineHeight: 34, letterSpacing: 2, paddingVertical: 4 },
  actions: { gap: 6, marginBottom: 26 },
  confirm: { paddingHorizontal: 16, paddingVertical: 12, gap: 8 },
  confirmActions: { flexDirection: "row", justifyContent: "flex-end", gap: 8 },
  schema: { paddingHorizontal: 8, paddingBottom: 8 },
  schemaBody: { marginHorizontal: 8, padding: 12, borderRadius: Radii.xs, ...ContinuousCorners },
});
