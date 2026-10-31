import React, { useCallback, useState } from "react";
import { ActivityIndicator, FlatList, Pressable, StyleSheet, View } from "react-native";
import { useFocusEffect, useRouter } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import { useAppTheme } from "../../constants/tokens";
import { AppText } from "../../components/ui/AppText";
import { EmptyState } from "../../components/ui/EmptyState";
import { Icon, type IconName } from "../../components/icons/Icon";
import { friendsCall } from "../../services/friendsAccount";
import { notificationRows, relativeTime, type FriendsNotification, type NotificationRow } from "../../services/friendsApi";
import { useCurrentServer } from "../../store/currentServer";
import { useFriendsLabs } from "../../store/friendsLabs";

const ICONS: Record<NotificationRow["kind"], IconName> = { comment: "chat", like: "heart", follow: "people" };

export default function FriendsNotificationsScreen() {
  const { colors } = useAppTheme();
  const router = useRouter();
  const { currentServer } = useCurrentServer();
  const { draftCount, status, refresh } = useFriendsLabs();
  const [rows, setRows] = useState<NotificationRow[] | null>(null);

  useFocusEffect(
    useCallback(() => {
      if (!currentServer) return;
      let cancelled = false;
      friendsCall<{ notifications: FriendsNotification[] }>(currentServer, "GET", "/notifications")
        .then(async (answer) => {
          if (cancelled) return;
          // Show what was new, then mark it read.
          setRows(notificationRows(answer.notifications));
          await friendsCall(currentServer, "POST", "/notifications/read").catch(() => undefined);
          void refresh();
        })
        .catch(() => !cancelled && setRows([]));
      return () => {
        cancelled = true;
      };
    }, [currentServer, refresh]),
  );

  const now = Date.now();
  const agent = status?.me?.agent_name || "Brain";
  const draftsRow =
    draftCount > 0 ? (
      <Pressable accessibilityRole="button" onPress={() => router.push("/friends/drafts")} style={[styles.row, { backgroundColor: colors.bgSurface, borderBottomColor: colors.borderSubtle }]}>
        <Icon name="edit" color={colors.textSecondary} size={22} />
        <View style={styles.copy}>
          <AppText variant="body">
            {agent} has {draftCount === 1 ? "a draft" : `${draftCount} drafts`} waiting for you
          </AppText>
        </View>
        <Icon name="chevron-right" color={colors.textTertiary} size={20} />
      </Pressable>
    ) : null;

  if (rows === null) {
    return (
      <View style={[styles.center, { backgroundColor: colors.bgPrimary }]}>
        <ActivityIndicator color={colors.textTertiary} />
      </View>
    );
  }
  return (
    <SafeAreaView edges={["bottom"]} style={{ flex: 1, backgroundColor: colors.bgPrimary }}>
      <FlatList
        data={rows}
        keyExtractor={(row) => row.key}
        ListHeaderComponent={draftsRow ?? undefined}
        ListEmptyComponent={draftCount === 0 ? <EmptyState size="inline" icon="bell" title="Nothing yet" detail="Comments, likes and follows show up here." /> : undefined}
        renderItem={({ item }) => (
          <Pressable
            accessibilityRole="button"
            onPress={() => router.push(item.postId ? { pathname: "/friends/post/[id]", params: { id: item.postId } } : { pathname: "/friends/person/[id]", params: { id: item.actorId } })}
            style={[styles.row, { backgroundColor: item.unread ? colors.bgSurface : "transparent", borderBottomColor: colors.borderSubtle }]}
          >
            <Icon name={ICONS[item.kind]} color={colors.textSecondary} size={22} />
            <View style={styles.copy}>
              <AppText variant="body">{item.text}</AppText>
              {item.detail ? (
                <AppText variant="body" tone="secondary" numberOfLines={2}>
                  {item.detail}
                </AppText>
              ) : null}
              <AppText variant="caption" tone="tertiary">
                {relativeTime(item.at, now)}
              </AppText>
            </View>
            {item.unread ? <View style={[styles.unread, { backgroundColor: colors.seal }]} /> : null}
          </Pressable>
        )}
      />
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  center: { flex: 1, alignItems: "center", justifyContent: "center" },
  row: { flexDirection: "row", alignItems: "flex-start", gap: 14, paddingHorizontal: 16, paddingVertical: 14, borderBottomWidth: StyleSheet.hairlineWidth },
  copy: { flex: 1, gap: 2 },
  unread: { width: 10, height: 10, borderRadius: 5, marginTop: 6 },
});
