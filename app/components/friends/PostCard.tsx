import React, { useState } from "react";
import { Pressable, StyleSheet, View } from "react-native";
import * as Haptics from "expo-haptics";
import { useAppTheme } from "../../constants/tokens";
import { AppText } from "../ui/AppText";
import { ActionMenu, type ActionMenuItem } from "../ui/ActionMenu";
import { Icon } from "../icons/Icon";
import { FriendsAvatar } from "./FriendsAvatar";
import { displayName, relativeTime, type FriendsPost } from "../../services/friendsApi";

interface PostCardProps {
  post: FriendsPost;
  now: number;
  onOpen(post: FriendsPost): void;
  onOpenPerson(personId: string): void;
  onLike(post: FriendsPost): void;
  onBookmark(post: FriendsPost): void;
  /** Items of the "more" menu: delete for your own post, report and block for others'. */
  menu(post: FriendsPost): ActionMenuItem[];
}

/** One post in a timeline: avatar, who and when, the text, and the quiet action row. */
export const PostCard = React.memo(function PostCard({ post, now, onOpen, onOpenPerson, onLike, onBookmark, menu }: PostCardProps) {
  const { colors } = useAppTheme();
  const [menuOpen, setMenuOpen] = useState(false);
  const name = displayName(post.author);
  return (
    <View style={[styles.card, { borderBottomColor: colors.borderSubtle }]}>
      <Pressable accessibilityRole="button" accessibilityLabel={`Open ${name}'s profile`} onPress={() => onOpenPerson(post.author.id)}>
        <FriendsAvatar author={post.author} />
      </Pressable>
      <View style={styles.body}>
        <Pressable accessibilityRole="button" accessibilityLabel={`Open post by ${name}`} onPress={() => onOpen(post)}>
          <View style={styles.head}>
            <AppText variant="subtitle" numberOfLines={1} style={styles.name}>
              {name}
            </AppText>
            {post.author.agent_name ? (
              <View style={[styles.tag, { backgroundColor: colors.surfaceSubtle, borderColor: colors.borderSubtle }]}>
                <AppText variant="micro" tone="secondary">
                  Agent
                </AppText>
              </View>
            ) : null}
            <AppText variant="caption" tone="tertiary" numberOfLines={1} style={styles.meta}>
              @{post.author.handle} · {relativeTime(post.created_at, now)}
            </AppText>
          </View>
          <AppText variant="body" style={styles.text}>
            {post.text}
          </AppText>
        </Pressable>
        <View style={styles.actions}>
          <Pressable style={styles.action} accessibilityRole="button" accessibilityLabel={`${post.comments} comments`} onPress={() => onOpen(post)}>
            <Icon name="chat" color={colors.textSecondary} size={20} />
            <AppText variant="compact" tone="secondary">
              {post.comments}
            </AppText>
          </Pressable>
          <Pressable
            style={styles.action}
            accessibilityRole="button"
            accessibilityState={{ selected: post.liked }}
            accessibilityLabel={post.liked ? `Unlike, ${post.likes} likes` : `Like, ${post.likes} likes`}
            onPress={() => {
              void Haptics.selectionAsync();
              onLike(post);
            }}
          >
            <Icon name={post.liked ? "heart-fill" : "heart"} color={post.liked ? colors.textPrimary : colors.textSecondary} size={20} />
            <AppText variant="compact" tone={post.liked ? "primary" : "secondary"}>
              {post.likes}
            </AppText>
          </Pressable>
          <Pressable
            style={styles.action}
            accessibilityRole="button"
            accessibilityState={{ selected: post.bookmarked }}
            accessibilityLabel={post.bookmarked ? "Remove bookmark" : "Bookmark"}
            onPress={() => {
              void Haptics.selectionAsync();
              onBookmark(post);
            }}
          >
            <Icon name={post.bookmarked ? "bookmark-fill" : "bookmark"} color={post.bookmarked ? colors.textPrimary : colors.textSecondary} size={20} />
          </Pressable>
          <View style={styles.spacer} />
          <Pressable style={styles.action} accessibilityRole="button" accessibilityLabel="More" onPress={() => setMenuOpen(true)}>
            <Icon name="more-horizontal" color={colors.textSecondary} size={20} />
          </Pressable>
        </View>
      </View>
      <ActionMenu visible={menuOpen} items={menu(post)} onClose={() => setMenuOpen(false)} />
    </View>
  );
});

const styles = StyleSheet.create({
  card: { flexDirection: "row", gap: 12, paddingHorizontal: 16, paddingTop: 14, paddingBottom: 4, borderBottomWidth: StyleSheet.hairlineWidth },
  body: { flex: 1, minWidth: 0 },
  head: { flexDirection: "row", alignItems: "center", gap: 6 },
  name: { flexShrink: 1 },
  tag: { borderWidth: StyleSheet.hairlineWidth, borderRadius: 6, paddingHorizontal: 6, paddingVertical: 1 },
  meta: { flexShrink: 1 },
  text: { marginTop: 4 },
  actions: { flexDirection: "row", alignItems: "center", marginTop: 2, marginLeft: -6 },
  action: { flexDirection: "row", alignItems: "center", gap: 6, minHeight: 44, minWidth: 44, paddingHorizontal: 6, marginRight: 14 },
  spacer: { flex: 1 },
});
