import React, { useCallback, useLayoutEffect, useState } from "react";
import { Pressable, StyleSheet, View } from "react-native";
import { useFocusEffect, useNavigation, useRouter } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import { useAppTheme } from "../constants/tokens";
import { AppText } from "../components/ui/AppText";
import { Icon } from "../components/icons/Icon";
import { EmptyState } from "../components/ui/EmptyState";
import { IconButton } from "../components/ui/IconButton";
import { FriendsAvatar } from "../components/friends/FriendsAvatar";
import { PostList } from "../components/friends/PostList";
import { useFriendsLabs } from "../store/friendsLabs";

type Scope = "all" | "following";

export default function FriendsScreen() {
  const { colors } = useAppTheme();
  const router = useRouter();
  const navigation = useNavigation();
  const { status, enabled, draftCount, unread, refresh } = useFriendsLabs();
  const [scope, setScope] = useState<Scope>("all");
  const [reload, setReload] = useState(0);

  useFocusEffect(
    useCallback(() => {
      void refresh();
      setReload((value) => value + 1);
    }, [refresh]),
  );

  const me = status?.me;
  useLayoutEffect(() => {
    navigation.setOptions({
      // Off, the page has nothing to act on.
      headerRight: () => enabled ? (
        <View style={styles.headerActions}>
          <View>
            <IconButton
              icon="bell"
              size={40}
              iconSize={20}
              color={colors.textPrimary}
              accessibilityLabel={unread > 0 ? `Notifications, ${unread} new` : "Notifications"}
              onPress={() => router.push("/friends/notifications")}
            />
            {unread > 0 ? <View pointerEvents="none" style={[styles.dot, { backgroundColor: colors.seal, borderColor: colors.bgPrimary }]} /> : null}
          </View>
          {me ? (
            <Pressable accessibilityRole="button" accessibilityLabel="Your profile" hitSlop={8} onPress={() => router.push({ pathname: "/friends/person/[id]", params: { id: me.id } })}>
              <FriendsAvatar author={me} size={32} />
            </Pressable>
          ) : null}
        </View>
      ) : null,
    });
  }, [navigation, router, colors, unread, me, enabled]);

  const header = (
    <View>
      <View style={[styles.tabs, { borderBottomColor: colors.borderSubtle }]}>
        {(["all", "following"] as const).map((key) => (
          <Pressable key={key} accessibilityRole="tab" accessibilityState={{ selected: scope === key }} onPress={() => setScope(key)} style={styles.tab}>
            <AppText variant="subtitle" tone={scope === key ? "primary" : "tertiary"}>
              {key === "all" ? "All" : "Following"}
            </AppText>
            <View style={[styles.underline, { backgroundColor: scope === key ? colors.textPrimary : "transparent" }]} />
          </Pressable>
        ))}
      </View>
      {draftCount > 0 ? (
        <Pressable
          accessibilityRole="button"
          accessibilityLabel={`${draftCount} draft${draftCount === 1 ? "" : "s"} waiting`}
          onPress={() => router.push("/friends/drafts")}
          style={[styles.banner, { backgroundColor: colors.bgSurface, borderBottomColor: colors.borderSubtle }]}
        >
          <View style={[styles.count, { backgroundColor: colors.seal }]}>
            <AppText variant="label" style={{ color: colors.onSeal }}>
              {draftCount}
            </AppText>
          </View>
          <View style={styles.bannerCopy}>
            <AppText variant="subtitle">{draftCount === 1 ? "Draft waiting" : "Drafts waiting"}</AppText>
            <AppText variant="caption" tone="secondary">
              Brain wrote {draftCount === 1 ? "this" : "these"} for you to check
            </AppText>
          </View>
          <Icon name="chevron-right" color={colors.textTertiary} size={20} />
        </Pressable>
      ) : null}
    </View>
  );

  if (!enabled) {
    return (
      <SafeAreaView edges={["bottom"]} style={[styles.screen, { backgroundColor: colors.bgPrimary }]}>
        <View style={styles.off}>
          <EmptyState
            icon="people"
            title="Friends is off"
            action={{ label: "Open Labs", icon: "settings", onPress: () => router.push("/settings") }}
          />
        </View>
      </SafeAreaView>
    );
  }
  return (
    <SafeAreaView edges={["bottom"]} style={[styles.screen, { backgroundColor: colors.bgPrimary }]}>
      <PostList
        key={scope}
        path={scope === "all" ? "/feed" : "/feed?scope=following"}
        header={header}
        reloadToken={reload}
        emptyTitle={scope === "all" ? "Nothing here yet" : "No one followed yet"}
      />
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1 },
  headerActions: { flexDirection: "row", alignItems: "center", gap: 16 },
  dot: { position: "absolute", top: 4, right: 4, width: 10, height: 10, borderRadius: 5, borderWidth: 1.5 },
  tabs: { flexDirection: "row", gap: 24, paddingHorizontal: 16, borderBottomWidth: StyleSheet.hairlineWidth },
  tab: { paddingTop: 12 },
  underline: { height: 2, marginTop: 8, borderRadius: 1 },
  banner: { flexDirection: "row", alignItems: "center", gap: 12, paddingHorizontal: 16, paddingVertical: 12, borderBottomWidth: StyleSheet.hairlineWidth },
  count: { width: 28, height: 28, borderRadius: 14, alignItems: "center", justifyContent: "center" },
  bannerCopy: { flex: 1 },
  off: { flex: 1, justifyContent: "center" },
});
