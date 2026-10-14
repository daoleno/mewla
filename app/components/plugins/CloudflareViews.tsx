import React, { useEffect, useState } from "react";
import { ActivityIndicator, Platform, StyleSheet, View } from "react-native";
import * as Clipboard from "expo-clipboard";
import { useAppTheme } from "../../constants/tokens";
import { openPluginAuthorization } from "../../services/pluginBrowser";
import {
  CLOUDFLARE_DESCRIPTION,
  cloudflareIdentity,
  cloudflareOutdated,
  cloudflareRow,
  cloudflareSecondsLeft,
  cloudflareStep,
  type CloudflareAction,
  type CloudflareLogin,
  type CloudflareStatus,
} from "../../services/cloudflareCli";
import { AppText, Button, InlineNotice, ListRow, ListSection, StatusPill } from "../ui";
import { ServiceGlyph, StatusCard } from "./PluginConnectionViews";

/** Cloudflare's row on Extensions, after the plugin services. */
export function CloudflareRow({ status, onOpen }: { status: CloudflareStatus | null; onOpen(): void }) {
  const row = cloudflareRow(status);
  return (
    <ListRow
      leading={<ServiceGlyph id="cloudflare" />}
      title="Cloudflare"
      subtitle={row.subtitle}
      // Nothing to offer until the server has said whether cf is there.
      trailing={!status ? undefined : row.label ? <StatusPill label={row.label} tone={row.tone} /> : <Button label="Set up" size="sm" variant="tinted" onPress={onOpen} accessibilityLabel="Set up Cloudflare" />}
      accessory="chevron"
      numberOfLines={2}
      accessibilityLabel={row.label ? `Cloudflare, ${row.label}. ${row.subtitle}` : `Cloudflare. ${row.subtitle}`}
      accessibilityHint="Opens Cloudflare"
      onPress={onOpen}
    />
  );
}

/**
 * The Cloudflare page: install cf on the server, sign it in with a code that
 * can be approved from any device, and sign it out. What agents may then do
 * is stated, not switched: Mewla doesn't see single cf commands.
 */
export function CloudflarePanel({ status, running, error, serverName, onAction }: {
  status: CloudflareStatus | null;
  running: CloudflareAction | null;
  error: string;
  serverName: string;
  onAction(action: CloudflareAction): void;
}) {
  const step = cloudflareStep(status);
  const busy = running !== null;
  return (
    <>
      <View style={styles.header}>
        <ServiceGlyph id="cloudflare" size={52} />
        <View style={styles.headerCopy}>
          <AppText variant="title" accessibilityRole="header">Cloudflare</AppText>
          <AppText variant="compact" tone="secondary">{CLOUDFLARE_DESCRIPTION}</AppText>
        </View>
      </View>
      {error ? <InlineNotice tone="danger" title="That didn't go through" detail={error} /> : null}
      {status?.message && step.kind !== "code" && step.kind !== "installing" ? <InlineNotice title={status.message} /> : null}
      {step.kind === "loading" ? <StatusCard busy title={`Checking cf on ${serverName}…`} /> : null}
      {step.kind === "unavailable" ? <InlineNotice tone="warning" icon="warning" title="Node.js needed" detail={`${step.detail} Install it there, then come back.`} /> : null}
      {step.kind === "install" ? (
        <View style={styles.actions}>
          <Button label={`Install cf ${status!.pinned}`} icon="cloud-download" variant="filled" size="lg" block loading={running === "cloudflare_install"} disabled={busy} onPress={() => onAction("cloudflare_install")} />
          <AppText variant="caption" tone="secondary" style={styles.centered}>Installs Cloudflare's cf CLI on {serverName} with npm.</AppText>
        </View>
      ) : null}
      {step.kind === "installing" ? <StatusCard busy title={`Installing cf ${status!.pinned}…`} detail={`npm is installing it on ${serverName}. This takes a minute.`} /> : null}
      {step.kind === "sign_in" ? (
        <View style={styles.actions}>
          <Button label="Sign in to Cloudflare" variant="filled" size="lg" block loading={running === "cloudflare_login"} disabled={busy} onPress={() => onAction("cloudflare_login")} />
          <AppText variant="caption" tone="secondary" style={styles.centered}>You get a code to approve on Cloudflare, from this device or any other.</AppText>
        </View>
      ) : null}
      {step.kind === "code" ? <CloudflareCode login={step.login} cancelling={running === "cloudflare_cancel"} onCancel={() => onAction("cloudflare_cancel")} /> : null}
      {step.kind === "signed_in" ? <SignedIn status={status!} serverName={serverName} busy={busy} signingOut={running === "cloudflare_logout"} onSignOut={() => onAction("cloudflare_logout")} /> : null}
      {status?.installed ? <CfInstall status={status} serverName={serverName} busy={busy} installing={running === "cloudflare_install" || !!status.installing} onInstall={() => onAction("cloudflare_install")} /> : null}
      <ListSection title="What Brain and Workers can do" footer="cf asks Cloudflare for its full default set of permissions. To ask for less, sign in on the server with cf auth login --scopes.">
        <ListRow icon="terminal" title="Use cf in their shells" subtitle={`Every agent on ${serverName} runs cf with this one sign-in: Workers, R2, D1, KV, DNS and the rest of the API.`} numberOfLines={4} />
        <ListRow icon="shield-check" title="Ask before changing what's live" subtitle="Mewla doesn't approve single cf commands. Agents need your go-ahead in the task before they delete anything, change DNS, or create or revoke tokens." numberOfLines={4} />
      </ListSection>
    </>
  );
}

/** The device code to approve, with a countdown. */
function CloudflareCode({ login, cancelling, onCancel }: { login: CloudflareLogin; cancelling: boolean; onCancel(): void }) {
  const { colors } = useAppTheme();
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  const left = cloudflareSecondsLeft(login, now);
  const open = async () => {
    await Clipboard.setStringAsync(login.code);
    await openPluginAuthorization(login.url, true, Platform.OS === "web" ? "new-tab" : undefined);
  };
  return (
    <StatusCard busy title="Enter this code on Cloudflare" detail={`Sign in to Cloudflare, enter the code and approve. ${left ? `${Math.floor(left / 60)}:${String(left % 60).padStart(2, "0")} left.` : "The code has expired."} This updates when you're done.`}>
      <AppText variant="monoStrong" selectable style={[styles.code, { color: colors.textPrimary }]} accessibilityLabel={`Code ${login.code.split("").join(" ")}`}>
        {login.code}
      </AppText>
      <AppText variant="caption" tone="secondary" selectable>{login.url}</AppText>
      <Button label="Copy code and open Cloudflare" icon="open-external" variant="filled" block disabled={!left} onPress={() => void open()} />
      <Button label="Cancel" variant="plain" block loading={cancelling} onPress={onCancel} />
    </StatusCard>
  );
}

function SignedIn({ status, serverName, busy, signingOut, onSignOut }: {
  status: CloudflareStatus;
  serverName: string;
  busy: boolean;
  signingOut: boolean;
  onSignOut(): void;
}) {
  const [confirming, setConfirming] = useState(false);
  const accounts = status.accounts ?? [];
  return (
    <ListSection title="Signed in" footer={status.token ? `cf uses the CLOUDFLARE_API_TOKEN set in ${serverName}'s environment. Remove it there to sign out.` : undefined}>
      <ListRow icon="person-circle" title={cloudflareIdentity(status)} subtitle={status.token ? "cf prefers this token over a sign-in" : `Every agent on ${serverName} acts as this account`} trailing={<StatusPill label="Signed in" tone="success" />} numberOfLines={2} />
      {accounts.map((account) => <ListRow key={account.id || account.name} icon="cube" title={account.name || account.id} subtitle={account.name ? account.id : undefined} />)}
      {status.token ? null : confirming ? (
        <View style={styles.confirm}>
          <AppText variant="compact" tone="secondary">Sign cf out on {serverName}? Brain and Workers lose Cloudflare access right away.</AppText>
          <View style={styles.confirmActions}>
            <Button label="Keep" variant="plain" size="sm" disabled={signingOut} onPress={() => setConfirming(false)} />
            <Button label="Sign out" variant="destructive" size="sm" loading={signingOut} disabled={busy && !signingOut} onPress={() => { setConfirming(false); onSignOut(); }} />
          </View>
        </View>
      ) : (
        <ListRow icon="exit" title="Sign out" destructive disabled={busy} onPress={() => setConfirming(true)} />
      )}
    </ListSection>
  );
}

/** Which cf is installed, where, and whether agents' shells find it. */
function CfInstall({ status, serverName, busy, installing, onInstall }: {
  status: CloudflareStatus;
  serverName: string;
  busy: boolean;
  installing: boolean;
  onInstall(): void;
}) {
  const outdated = cloudflareOutdated(status);
  return (
    <ListSection title={`cf on ${serverName}`} footer={status.on_path ? undefined : `npm put cf in a folder the server's PATH doesn't include, so agents may not find it. Add ${status.path ? status.path.replace(/\/cf$/, "") : "npm's global bin folder"} to PATH.`}>
      <ListRow
        icon="terminal"
        title={`cf ${status.version ?? ""}`.trim()}
        subtitle={[status.path, outdated ? `Mewla installs ${status.pinned}` : ""].filter(Boolean).join(" · ") || undefined}
        numberOfLines={3}
        trailing={outdated ? (installing ? <ActivityIndicator /> : <Button label={`Install ${status.pinned}`} size="sm" variant="tinted" disabled={busy} onPress={onInstall} />) : undefined}
      />
    </ListSection>
  );
}

const styles = StyleSheet.create({
  header: { flexDirection: "row", alignItems: "center", gap: 14, paddingHorizontal: 4 },
  headerCopy: { flex: 1, minWidth: 0, gap: 4 },
  actions: { gap: 8 },
  centered: { textAlign: "center" },
  code: { fontSize: 26, lineHeight: 34, letterSpacing: 2, paddingVertical: 4 },
  confirm: { paddingHorizontal: 16, paddingVertical: 12, gap: 8 },
  confirmActions: { flexDirection: "row", justifyContent: "flex-end", gap: 8 },
});
