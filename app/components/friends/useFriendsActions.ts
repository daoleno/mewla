import { useCallback } from "react";
import { Alert } from "react-native";
import { useRouter } from "expo-router";
import { confirmDestructive } from "../ui/confirmDestructive";
import type { ActionMenuItem } from "../ui/ActionMenu";
import { friendsCall } from "../../services/friendsAccount";
import type { FriendsPost } from "../../services/friendsApi";
import { useCurrentServer } from "../../store/currentServer";

/**
 * What a post's "more" menu does, and likes and bookmarks, through the current
 * computer. Every action reports back so a screen can update its list.
 */
export function useFriendsActions(handlers: { onPost(post: FriendsPost): void; onRemoved(postId: string): void; onBlocked(personId: string): void }) {
  const { currentServer } = useCurrentServer();
  const router = useRouter();
  const { onPost, onRemoved, onBlocked } = handlers;

  const fail = useCallback((reason: unknown) => {
    Alert.alert("Friends", reason instanceof Error ? reason.message : "That did not work");
  }, []);

  const toggle = useCallback(
    async (post: FriendsPost, field: "like" | "bookmark") => {
      if (!currentServer) return;
      const on = field === "like" ? post.liked : post.bookmarked;
      // Show the change at once; the service's answer replaces it.
      const optimistic: FriendsPost =
        field === "like"
          ? { ...post, liked: !on, likes: post.likes + (on ? -1 : 1) }
          : { ...post, bookmarked: !on };
      onPost(optimistic);
      try {
        const answer = await friendsCall<FriendsPost | { ok: boolean }>(currentServer, on ? "DELETE" : "PUT", `/posts/${post.id}/${field}`);
        if ("id" in answer) onPost(answer);
      } catch (reason) {
        onPost(post);
        fail(reason);
      }
    },
    [currentServer, onPost, fail],
  );

  const menu = useCallback(
    (post: FriendsPost): ActionMenuItem[] => {
      if (!currentServer) return [];
      if (post.mine) {
        return [
          {
            key: "delete",
            label: "Delete post",
            icon: "trash",
            destructive: true,
            onPress: () =>
              confirmDestructive({
                title: "Delete this post?",
                message: "It disappears from Friends for everyone.",
                confirmLabel: "Delete",
                onConfirm: async () => {
                  try {
                    await friendsCall(currentServer, "DELETE", `/posts/${post.id}`);
                    onRemoved(post.id);
                  } catch (reason) {
                    fail(reason);
                  }
                },
              }),
          },
        ];
      }
      return [
        {
          key: "report",
          label: "Report post",
          icon: "flag",
          onPress: () => {
            void friendsCall(currentServer, "POST", "/reports", { kind: "post", target_id: post.id, reason: "reported" })
              .then(() => Alert.alert("Reported", "Thanks. Three reports from different people hide a post."))
              .catch(fail);
          },
        },
        {
          key: "block",
          label: `Block @${post.author.handle}`,
          icon: "person-remove",
          destructive: true,
          detail: "You stop seeing each other",
          onPress: () =>
            confirmDestructive({
              title: `Block @${post.author.handle}?`,
              message: "Neither of you will see the other's posts, comments or profile.",
              confirmLabel: "Block",
              onConfirm: async () => {
                try {
                  await friendsCall(currentServer, "PUT", `/people/${post.author.id}/block`);
                  onBlocked(post.author.id);
                } catch (reason) {
                  fail(reason);
                }
              },
            }),
        },
      ];
    },
    [currentServer, onRemoved, onBlocked, fail],
  );

  const openPost = useCallback((post: FriendsPost) => router.push({ pathname: "/friends/post/[id]", params: { id: post.id } }), [router]);
  const openPerson = useCallback((id: string) => router.push({ pathname: "/friends/person/[id]", params: { id } }), [router]);

  return { toggle, menu, openPost, openPerson, fail };
}
