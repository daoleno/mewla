import { DateStyle, formatDate } from "../../../services/dateFormat";
import React, { useCallback, useEffect, useRef, useState } from "react";
import { ActivityIndicator, Alert, FlatList, KeyboardAvoidingView, Platform, Pressable, StyleSheet, TextInput, View, type TextInputInstance } from "react-native";
import { useFocusEffect, useLocalSearchParams, useRouter } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import { useAppTheme } from "../../../constants/tokens";
import { AppText } from "../../../components/ui/AppText";
import { ActionMenu, type ActionMenuItem } from "../../../components/ui/ActionMenu";
import { Button } from "../../../components/ui/Button";
import { EmptyState } from "../../../components/ui/EmptyState";
import { confirmDestructive } from "../../../components/ui/confirmDestructive";
import { Icon } from "../../../components/icons/Icon";
import { FriendsAvatar } from "../../../components/friends/FriendsAvatar";
import { PostMedia } from "../../../components/friends/PostMedia";
import { useFriendsActions } from "../../../components/friends/useFriendsActions";
import { friendsCall } from "../../../services/friendsAccount";
import {
  FRIENDS_COMMENT_LIMIT,
  displayName,
  relativeTime,
  type FriendsComment,
  type FriendsPerson,
  type FriendsPost,
} from "../../../services/friendsApi";
import { useCurrentServer } from "../../../store/currentServer";

interface Detail {
  post: FriendsPost;
  comments: FriendsComment[];
}

export default function FriendsPostScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const { colors } = useAppTheme();
  const router = useRouter();
  const { currentServer } = useCurrentServer();
  const [detail, setDetail] = useState<Detail | null>(null);
  const [gone, setGone] = useState(false);
  const [author, setAuthor] = useState<FriendsPerson | null>(null);
  const [draft, setDraft] = useState("");
  const [sending, setSending] = useState(false);
  const [commentMenu, setCommentMenu] = useState<FriendsComment | null>(null);
  const [postMenu, setPostMenu] = useState(false);
  const [videoPlaying, setVideoPlaying] = useState(false);
  const input = useRef<TextInputInstance>(null);

  const load = useCallback(() => {
    if (!currentServer || !id) return;
    friendsCall<Detail>(currentServer, "GET", `/posts/${id}`)
      .then((answer) => {
        setDetail(answer);
        if (!answer.post.mine) {
          friendsCall<FriendsPerson>(currentServer, "GET", `/people/${answer.post.author.id}`).then(setAuthor).catch(() => undefined);
        }
      })
      .catch(() => setGone(true));
  }, [currentServer, id]);
  useFocusEffect(load);

  const actions = useFriendsActions({
    onPost: (post) => setDetail((current) => (current ? { ...current, post } : current)),
    onRemoved: () => router.back(),
    onBlocked: () => router.back(),
  });

  useEffect(() => {
    if (gone) router.back();
  }, [gone, router]);

  if (!detail) {
    return (
      <View style={[styles.center, { backgroundColor: colors.bgPrimary }]}>
        <ActivityIndicator color={colors.textTertiary} />
      </View>
    );
  }
  const { post, comments } = detail;
  const now = Date.now();

  const send = async () => {
    const text = draft.trim();
    if (!currentServer || !text || sending) return;
    setSending(true);
    try {
      const comment = await friendsCall<FriendsComment>(currentServer, "POST", `/posts/${post.id}/comments`, { text });
      setDraft("");
      setDetail((current) => (current ? { post: { ...current.post, comments: current.post.comments + 1 }, comments: [...current.comments, comment] } : current));
    } catch (reason) {
      Alert.alert("Friends", reason instanceof Error ? reason.message : "Could not comment");
    } finally {
      setSending(false);
    }
  };

  const toggleFollow = async () => {
    if (!currentServer || !author) return;
    try {
      setAuthor(await friendsCall<FriendsPerson>(currentServer, author.followed ? "DELETE" : "PUT", `/people/${author.id}/follow`));
    } catch (reason) {
      actions.fail(reason);
    }
  };

  const commentItems = (comment: FriendsComment): ActionMenuItem[] => {
    const items: ActionMenuItem[] = [];
    if (comment.mine || post.mine) {
      items.push({
        key: "delete",
        label: "Delete comment",
        icon: "trash",
        destructive: true,
        onPress: () =>
          confirmDestructive({
            title: "Delete this comment?",
            confirmLabel: "Delete",
            onConfirm: async () => {
              if (!currentServer) return;
              try {
                await friendsCall(currentServer, "DELETE", `/comments/${comment.id}`);
                setDetail((current) =>
                  current ? { post: { ...current.post, comments: Math.max(0, current.post.comments - 1) }, comments: current.comments.filter((c) => c.id !== comment.id) } : current,
                );
              } catch (reason) {
                actions.fail(reason);
              }
            },
          }),
      });
    }
    if (!comment.mine) {
      items.push({
        key: "report",
        label: "Report comment",
        icon: "flag",
        onPress: () => {
          if (!currentServer) return;
          void friendsCall(currentServer, "POST", "/reports", { kind: "comment", target_id: comment.id, reason: "reported" })
            .then(() => Alert.alert("Reported", "Thanks. Three reports from different people hide a comment."))
            .catch(actions.fail);
        },
      });
    }
    return items;
  };

  const header = (
    <View>
      <View style={styles.authorRow}>
        <Pressable style={styles.authorMain} accessibilityRole="button" onPress={() => actions.openPerson(post.author.id)}>
          <FriendsAvatar author={post.author} size={48} />
          <View style={styles.authorCopy}>
            <View style={styles.nameRow}>
              <AppText variant="heading" numberOfLines={1} style={styles.shrink}>
                {displayName(post.author)}
              </AppText>
              {post.author.agent_name ? (
                <View style={[styles.tag, { backgroundColor: colors.surfaceSubtle, borderColor: colors.borderSubtle }]}>
                  <AppText variant="micro" tone="secondary">
                    Agent
                  </AppText>
                </View>
              ) : null}
            </View>
            <AppText variant="caption" tone="secondary">
              by @{post.author.handle}
            </AppText>
          </View>
        </Pressable>
        {!post.mine && author ? (
          <Button label={author.followed ? "Following" : "Follow"} size="sm" variant={author.followed ? "outlined" : "filled"} onPress={() => void toggleFollow()} />
        ) : null}
      </View>
      <AppText variant="title" style={styles.postText}>
        {post.text}
      </AppText>
      {post.media?.length ? <PostMedia media={post.media} active={videoPlaying} onPlay={() => setVideoPlaying(true)} onStop={() => setVideoPlaying(false)} /> : null}
      <AppText variant="caption" tone="tertiary" style={styles.when}>
        {formatDate(post.created_at, DateStyle.dateTime)} ·{" "}
        {post.via === "auto" ? "Auto-posted" : "Approved by owner"}
      </AppText>
      <View style={[styles.counts, { borderColor: colors.borderSubtle }]}>
        <AppText variant="body" tone="secondary">
          <AppText variant="subtitle">{post.likes}</AppText> {post.likes === 1 ? "like" : "likes"}
        </AppText>
        <AppText variant="body" tone="secondary">
          <AppText variant="subtitle">{post.comments}</AppText> {post.comments === 1 ? "comment" : "comments"}
        </AppText>
      </View>
      <View style={[styles.bar, { borderColor: colors.borderSubtle }]}>
        <Pressable style={styles.barButton} accessibilityRole="button" accessibilityLabel={post.liked ? "Unlike" : "Like"} onPress={() => void actions.toggle(post, "like")}>
          <Icon name={post.liked ? "heart-fill" : "heart"} color={post.liked ? colors.textPrimary : colors.textSecondary} size={24} />
        </Pressable>
        <Pressable style={styles.barButton} accessibilityRole="button" accessibilityLabel="Comment" onPress={() => input.current?.focus()}>
          <Icon name="chat" color={colors.textSecondary} size={24} />
        </Pressable>
        <Pressable style={styles.barButton} accessibilityRole="button" accessibilityLabel={post.bookmarked ? "Remove bookmark" : "Bookmark"} onPress={() => void actions.toggle(post, "bookmark")}>
          <Icon name={post.bookmarked ? "bookmark-fill" : "bookmark"} color={post.bookmarked ? colors.textPrimary : colors.textSecondary} size={24} />
        </Pressable>
        <Pressable style={styles.barButton} accessibilityRole="button" accessibilityLabel="More" onPress={() => setPostMenu(true)}>
          <Icon name="more-horizontal" color={colors.textSecondary} size={24} />
        </Pressable>
      </View>
    </View>
  );

  return (
    <SafeAreaView edges={["bottom"]} style={[styles.screen, { backgroundColor: colors.bgPrimary }]}>
      <KeyboardAvoidingView style={styles.screen} behavior={Platform.OS === "ios" ? "padding" : undefined} keyboardVerticalOffset={Platform.OS === "ios" ? 90 : 0}>
        <FlatList
          data={comments}
          keyExtractor={(comment) => comment.id}
          ListHeaderComponent={header}
          ListEmptyComponent={<EmptyState size="inline" title="No comments yet" detail="Be the first to say something kind." />}
          renderItem={({ item }) => (
            <Pressable
              onLongPress={() => setCommentMenu(item)}
              accessibilityRole="button"
              accessibilityHint="Long press for options"
              style={[styles.comment, { borderBottomColor: colors.borderSubtle }]}
            >
              <FriendsAvatar author={{ ...item.author, pet_id: "" }} size={36} />
              <View style={styles.shrink}>
                <AppText variant="subtitle">
                  @{item.author.handle}{" "}
                  <AppText variant="caption" tone="tertiary">
                    · {relativeTime(item.created_at, now)}
                  </AppText>
                </AppText>
                <AppText variant="body">{item.text}</AppText>
              </View>
              <Pressable hitSlop={8} accessibilityRole="button" accessibilityLabel="Comment options" onPress={() => setCommentMenu(item)}>
                <Icon name="more-horizontal" color={colors.textTertiary} size={20} />
              </Pressable>
            </Pressable>
          )}
        />
        <View style={[styles.composer, { borderTopColor: colors.borderSubtle, backgroundColor: colors.bgPrimary }]}>
          <TextInput
            ref={input}
            value={draft}
            onChangeText={setDraft}
            placeholder="Add a comment"
            placeholderTextColor={colors.textTertiary}
            maxLength={FRIENDS_COMMENT_LIMIT}
            style={[styles.input, { borderColor: colors.border, backgroundColor: colors.inputBackground, color: colors.textPrimary }]}
            returnKeyType="send"
            onSubmitEditing={() => void send()}
          />
          <Pressable
            accessibilityRole="button"
            accessibilityLabel="Send comment"
            disabled={!draft.trim() || sending}
            onPress={() => void send()}
            style={[styles.send, { backgroundColor: draft.trim() ? colors.seal : colors.disabledSurface }]}
          >
            <Icon name="paper-plane" color={draft.trim() ? colors.onSeal : colors.disabledText} size={22} />
          </Pressable>
        </View>
      </KeyboardAvoidingView>
      <ActionMenu visible={commentMenu !== null} items={commentMenu ? commentItems(commentMenu) : []} onClose={() => setCommentMenu(null)} />
      <ActionMenu visible={postMenu} items={actions.menu(post)} onClose={() => setPostMenu(false)} />
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1 },
  center: { flex: 1, alignItems: "center", justifyContent: "center" },
  shrink: { flexShrink: 1, flex: 1 },
  authorRow: { flexDirection: "row", alignItems: "center", gap: 12, paddingHorizontal: 16, paddingTop: 12 },
  authorMain: { flex: 1, flexDirection: "row", alignItems: "center", gap: 12 },
  authorCopy: { flex: 1 },
  nameRow: { flexDirection: "row", alignItems: "center", gap: 8 },
  tag: { borderWidth: StyleSheet.hairlineWidth, borderRadius: 6, paddingHorizontal: 6, paddingVertical: 1 },
  postText: { paddingHorizontal: 16, paddingTop: 14 },
  when: { paddingHorizontal: 16, paddingTop: 10, paddingBottom: 14 },
  counts: { flexDirection: "row", gap: 20, paddingHorizontal: 16, paddingVertical: 12, borderTopWidth: StyleSheet.hairlineWidth },
  bar: { flexDirection: "row", justifyContent: "space-around", borderTopWidth: StyleSheet.hairlineWidth, borderBottomWidth: StyleSheet.hairlineWidth },
  barButton: { minHeight: 48, minWidth: 48, alignItems: "center", justifyContent: "center" },
  comment: { flexDirection: "row", gap: 12, paddingHorizontal: 16, paddingVertical: 12, borderBottomWidth: StyleSheet.hairlineWidth },
  composer: { flexDirection: "row", alignItems: "center", gap: 10, paddingHorizontal: 12, paddingVertical: 8, borderTopWidth: StyleSheet.hairlineWidth },
  input: { flex: 1, minHeight: 44, borderRadius: 22, borderWidth: 1, paddingHorizontal: 16, fontSize: 16 },
  send: { width: 44, height: 44, borderRadius: 22, alignItems: "center", justifyContent: "center" },
});
