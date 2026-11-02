import React, { useState } from "react";
import { Pressable, StyleSheet, TextInput, View } from "react-native";
import type { PluginAccount } from "../../services/connections";
import { cloudProfileGroups, cloudProfileIdentity, cloudProfileStatus } from "../../services/cloudProfilesModel";
import { useAppTheme } from "../../constants/tokens";
import { AppText, ListSection, StatusPill } from "../ui";
import { Icon } from "../icons/Icon";

/** Shared vendor-CLI list. Sign-in status is independent of agent permission. */
export function CloudProfileList({ accounts, renderDetails }: {
  accounts: PluginAccount[];
  renderDetails(account: PluginAccount, close: () => void): React.ReactNode;
}) {
  const { colors } = useAppTheme();
  const [query, setQuery] = useState("");
  const [expanded, setExpanded] = useState(false);
  const [selected, setSelected] = useState<string | null>(null);
  const groups = cloudProfileGroups(accounts, accounts.length > 8 ? query : "");
  const showExpired = expanded || (groups.filterVisible && !!query.trim());
  const rows = (items: PluginAccount[]) => items.map(account => {
    const status = cloudProfileStatus(account);
    const name = account.profile || account.name;
    const identity = cloudProfileIdentity(account);
    const open = selected === account.id;
    return <View key={account.id}>
      <Pressable accessibilityRole="button" accessibilityLabel={[name, identity, status.label].filter(Boolean).join(", ")} accessibilityHint="Show account actions" accessibilityState={{ expanded: open }} onPress={() => setSelected(open ? null : account.id)} style={({ pressed }) => [styles.row, { opacity: pressed ? 0.65 : 1 }]}>
        <AppText variant="compact" numberOfLines={1} style={styles.name}>{name}</AppText>
        <AppText variant="caption" tone="secondary" numberOfLines={1} style={styles.identity}>{identity}</AppText>
        <StatusPill label={status.label} tone={status.tone} />
        <Icon name={open ? "chevron-down" : "chevron-right"} size={16} color={colors.textTertiary} />
      </Pressable>
      {open ? <View style={styles.details}>{renderDetails(account, () => setSelected(null))}</View> : null}
    </View>;
  });
  return <>
    {groups.filterVisible ? <TextInput accessibilityLabel="Filter profiles" placeholder="Filter profiles" placeholderTextColor={colors.textTertiary} value={query} onChangeText={setQuery} autoCapitalize="none" autoCorrect={false} clearButtonMode="while-editing" style={[styles.filter, { color: colors.textPrimary, borderColor: colors.border, backgroundColor: colors.bgSurface }]} /> : null}
    {groups.signedIn.length || groups.unchecked.length ? <ListSection>{rows([...groups.signedIn, ...groups.unchecked])}</ListSection> : null}
    {groups.expired.length ? <ListSection>
      <Pressable accessibilityRole="button" accessibilityLabel={`Expired (${groups.expired.length})`} accessibilityState={{ expanded: showExpired }} onPress={() => setExpanded(!expanded)} style={styles.row}>
        <AppText variant="compact" tone="secondary" style={styles.group}>Expired ({groups.expired.length})</AppText>
        <Icon name={showExpired ? "chevron-down" : "chevron-right"} size={16} color={colors.textTertiary} />
      </Pressable>
      {showExpired ? rows(groups.expired) : null}
    </ListSection> : null}
    {!groups.signedIn.length && !groups.unchecked.length && !groups.expired.length && query ? <AppText variant="compact" tone="secondary">No matching profiles</AppText> : null}
  </>;
}
const styles = StyleSheet.create({
  row: { minHeight: 52, paddingHorizontal: 14, paddingVertical: 10, flexDirection: "row", alignItems: "center", gap: 8 },
  name: { flex: 1, minWidth: 0 },
  identity: { flex: 1, minWidth: 0, textAlign: "right" },
  group: { flex: 1 },
  details: { paddingHorizontal: 8, paddingBottom: 8 },
  filter: { minHeight: 44, paddingHorizontal: 14, borderWidth: 1, borderRadius: 12, fontSize: 16 },
});
