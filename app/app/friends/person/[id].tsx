import React, { useCallback, useState } from "react";
import { Alert, Pressable, StyleSheet, View } from "react-native";
import { useFocusEffect, useLocalSearchParams, useRouter } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import { useAppTheme } from "../../../constants/tokens";
import { AppText } from "../../../components/ui/AppText";
import { ActionMenu } from "../../../components/ui/ActionMenu";
import { Button } from "../../../components/ui/Button";
import { confirmDestructive } from "../../../components/ui/confirmDestructive";
import { ListRow, ListSection } from "../../../components/ui";
import { Icon } from "../../../components/icons/Icon";
import { FriendsAvatar } from "../../../components/friends/FriendsAvatar";
import { PostList } from "../../../components/friends/PostList";
import { friendsCall } from "../../../services/friendsAccount";
import { displayName, type FriendsPerson } from "../../../services/friendsApi";
import { useCurrentServer } from "../../../store/currentServer";

export default function FriendsPersonScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const { colors } = useAppTheme();
  const router = useRouter();
  const { currentServer } = useCurrentServer();
  const [person, setPerson] = useState<FriendsPerson | null>(null);
  const [menu, setMenu] = useState(false);
  const [reload, setReload] = useState(0);

  const load = useCallback(() => {
    if (!currentServer || !id) return;
    friendsCall<FriendsPerson>(currentServer, "GET", `/people/${id}`)
      .then(setPerson)
      .catch(() => router.back());
    setReload((value) => value + 1);
  }, [currentServer, id, router]);
  useFocusEffect(load);

  if (!person) return <View style={{ flex: 1, backgroundColor: colors.bgPrimary }} />;

  const change = async (method: "PUT" | "DELETE", path: string) => {
    if (!currentServer) return;
    try {
      setPerson(await friendsCall<FriendsPerson>(currentServer, method, path));
    } catch (reason) {
      Alert.alert("Friends", reason instanceof Error ? reason.message : "That did not work");
    }
  };

  const header = (
    <View>
      <View style={styles.top}>
        <FriendsAvatar author={person} size={76} />
        <View style={styles.topCopy}>
          <AppText variant="display" numberOfLines={1}>
            {displayName(person)}
          </AppText>
          <View style={styles.byRow}>
            {person.agent_name ? (
              <View style={[styles.tag, { backgroundColor: colors.surfaceSubtle, borderColor: colors.borderSubtle }]}>
                <AppText variant="micro" tone="secondary">
                  Agent
                </AppText>
              </View>
            ) : null}
            <AppText variant="body" tone="secondary">
              by @{person.handle}
            </AppText>
          </View>
        </View>
      </View>
      {person.bio ? (
        <AppText variant="body" style={styles.bio}>
          {person.bio}
        </AppText>
      ) : null}
      <View style={styles.counts}>
        <AppText variant="body" tone="secondary">
          <AppText variant="subtitle">{person.posts}</AppText> posts
        </AppText>
        <AppText variant="body" tone="secondary">
          <AppText variant="subtitle">{person.following}</AppText> following
        </AppText>
        <AppText variant="body" tone="secondary">
          <AppText variant="subtitle">{person.followers}</AppText> followers
        </AppText>
      </View>
      {person.mine ? (
        <View style={styles.mine}>
          <ListSection>
            <ListRow title="Bookmarks" icon="bookmark" accessory="chevron" onPress={() => router.push("/friends/bookmarks")} />
            <ListRow title="Blocked people" icon="person-remove" accessory="chevron" onPress={() => router.push("/friends/blocked")} />
          </ListSection>
        </View>
      ) : (
        <View style={styles.buttons}>
          {person.blocked ? (
            <Button label="Unblock" variant="outlined" block style={styles.grow} onPress={() => void change("DELETE", `/people/${person.id}/block`)} />
          ) : (
            <Button
              label={person.followed ? "Following" : "Follow"}
              variant={person.followed ? "outlined" : "filled"}
              block
              style={styles.grow}
              onPress={() => void change(person.followed ? "DELETE" : "PUT", `/people/${person.id}/follow`)}
            />
          )}
          <Pressable
            accessibilityRole="button"
            accessibilityLabel="More"
            onPress={() => setMenu(true)}
            style={[styles.more, { borderColor: colors.border }]}
          >
            <Icon name="more-horizontal" color={colors.textPrimary} size={22} />
          </Pressable>
        </View>
      )}
    </View>
  );

  return (
    <SafeAreaView edges={["bottom"]} style={{ flex: 1, backgroundColor: colors.bgPrimary }}>
      <PostList
        path={`/people/${person.id}/posts`}
        header={header}
        reloadToken={reload}
        emptyTitle="No posts yet"
        emptyDetail={person.mine ? "What Brain posts for you shows up here." : undefined}
      />
      <ActionMenu
        visible={menu}
        onClose={() => setMenu(false)}
        items={[
          {
            key: "report",
            label: "Report",
            icon: "flag",
            onPress: () => {
              if (!currentServer) return;
              void friendsCall(currentServer, "POST", "/reports", { kind: "person", target_id: person.id, reason: "reported" })
                .then(() => Alert.alert("Reported", "Thanks. Three reports from different people hide a profile."))
                .catch((reason: unknown) => Alert.alert("Friends", reason instanceof Error ? reason.message : "That did not work"));
            },
          },
          {
            key: "block",
            label: person.blocked ? "Unblock" : `Block @${person.handle}`,
            icon: "person-remove",
            destructive: !person.blocked,
            onPress: () =>
              person.blocked
                ? void change("DELETE", `/people/${person.id}/block`)
                : confirmDestructive({
                    title: `Block @${person.handle}?`,
                    message: "Neither of you will see the other's posts, comments or profile.",
                    confirmLabel: "Block",
                    onConfirm: async () => {
                      await change("PUT", `/people/${person.id}/block`);
                      router.back();
                    },
                  }),
          },
        ]}
      />
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  top: { flexDirection: "row", alignItems: "center", gap: 16, paddingHorizontal: 16, paddingTop: 12 },
  topCopy: { flex: 1, gap: 6 },
  byRow: { flexDirection: "row", alignItems: "center", gap: 8 },
  tag: { borderWidth: StyleSheet.hairlineWidth, borderRadius: 6, paddingHorizontal: 6, paddingVertical: 1 },
  bio: { paddingHorizontal: 16, paddingTop: 14 },
  counts: { flexDirection: "row", gap: 20, paddingHorizontal: 16, paddingVertical: 14 },
  buttons: { flexDirection: "row", gap: 12, paddingHorizontal: 16, paddingBottom: 16 },
  grow: { flex: 1 },
  more: { width: 48, height: 48, borderRadius: 24, borderWidth: 1, alignItems: "center", justifyContent: "center" },
  mine: { paddingHorizontal: 16 },
});
