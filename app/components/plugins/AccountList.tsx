import React, { useEffect, useState } from "react";
import { Pressable, StyleSheet, View } from "react-native";
import type { PluginAccount } from "../../services/connections";
import { cloudProfileGroups } from "../../services/cloudProfilesModel";
import { accountRow, openAccountId } from "../../services/accountListModel";
import { useAppTheme } from "../../constants/tokens";
import { AppText, ListSection, StatusPill } from "../ui";
import { MobileSingleLineInput } from "../ui/MobileSingleLineInput";
import { Icon } from "../icons/Icon";

/**
 * Every service lists its accounts the same way: one collapsed row each (name,
 * what tells it apart, status), the account's controls on tap. A lone account
 * starts open. Sign-in status is independent of agent permission.
 */
export function AccountList({ accounts, renderDetails }: {
  accounts: PluginAccount[];
  renderDetails(account: PluginAccount, close: () => void): React.ReactNode;
}) {
  const { colors } = useAppTheme();
  const [now, setNow] = useState(Date.now);
  useEffect(() => { const timer = setInterval(() => setNow(Date.now()), 60000); return () => clearInterval(timer); }, []);
  const [query, setQuery] = useState("");
  const [expanded, setExpanded] = useState(false);
  const [chosen, setChosen] = useState<string | null | undefined>(undefined);
  const selected = openAccountId(accounts, chosen);
  const groups = cloudProfileGroups(accounts, accounts.length > 8 ? query : "");
  const showExpired = expanded || (groups.filterVisible && !!query.trim());
  const rows = (items: PluginAccount[]) => items.map(account => {
    const { name, identity, status, expiry } = accountRow(account, now);
    const open = selected === account.id;
    return <View key={account.id}>
      <Pressable accessibilityRole="button" accessibilityLabel={[name, identity, status.label].filter(Boolean).join(", ")} accessibilityHint="Show account actions" accessibilityState={{ expanded: open }} onPress={() => setChosen(open ? null : account.id)} style={({ pressed }) => [styles.row, { opacity: pressed ? 0.65 : 1 }]}>
        <View style={styles.name}>
          <AppText variant="compact" numberOfLines={1}>{name}</AppText>
          {account.integration === "aws" && account.aws?.account_id ? <View style={styles.identity}>
            {account.aws.account_name ? <AppText variant="caption" tone="secondary" numberOfLines={1} style={styles.alias}>{account.aws.account_name}</AppText> : null}
            <AppText variant="caption" tone="secondary" numberOfLines={1} style={styles.accountID}>{account.aws.account_name ? " · " : ""}{account.aws.account_id}</AppText>
          </View> : identity ? <AppText variant="caption" tone="secondary" numberOfLines={1}>{identity}</AppText> : null}
        </View>
        <View style={styles.state}><StatusPill label={status.label} tone={status.tone} />{expiry ? <AppText variant="caption" tone={expiry.soon ? "secondary" : "tertiary"} numberOfLines={1}>{expiry.label}</AppText> : null}</View>
        <Icon name={open ? "chevron-down" : "chevron-right"} size={16} color={colors.textTertiary} />
      </Pressable>
      {open ? <View style={styles.details}>{renderDetails(account, () => setChosen(null))}</View> : null}
    </View>;
  });
  return <>
    {groups.filterVisible ? <MobileSingleLineInput framed accessibilityLabel="Filter accounts" placeholder="Filter accounts" placeholderTextColor={colors.textTertiary} value={query} onChangeText={setQuery} autoCapitalize="none" autoCorrect={false} clearButtonMode="while-editing" /> : null}
    {groups.signedIn.length || groups.unchecked.length ? <ListSection>{rows([...groups.signedIn, ...groups.unchecked])}</ListSection> : null}
    {groups.expired.length ? <ListSection>
      <Pressable accessibilityRole="button" accessibilityLabel={`Expired (${groups.expired.length})`} accessibilityState={{ expanded: showExpired }} onPress={() => setExpanded(!expanded)} style={styles.row}>
        <AppText variant="compact" tone="secondary" style={styles.group}>Expired ({groups.expired.length})</AppText>
        <Icon name={showExpired ? "chevron-down" : "chevron-right"} size={16} color={colors.textTertiary} />
      </Pressable>
      {showExpired ? rows(groups.expired) : null}
    </ListSection> : null}
    {!groups.signedIn.length && !groups.unchecked.length && !groups.expired.length && query ? <AppText variant="compact" tone="secondary">No matching accounts</AppText> : null}
  </>;
}
const styles = StyleSheet.create({
  row: { minHeight: 52, paddingHorizontal: 14, paddingVertical: 10, flexDirection: "row", alignItems: "center", gap: 8 },
  name: { flex: 1, minWidth: 0 },
  identity: { flexDirection: "row", alignItems: "center", minWidth: 0 },
  accountID: { flexShrink: 0 },
  alias: { flexShrink: 1, minWidth: 0 },
  state: { alignItems: "flex-end", gap: 3 },
  group: { flex: 1 },
  details: { paddingHorizontal: 8, paddingBottom: 8 },
});
