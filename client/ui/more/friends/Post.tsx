import { effect, lifecycle, Scroll, signal, Text, untracked, View } from "nsh";
import { AlertDialog, Button, Input } from "nsh/base";
import { displayName, FRIENDS_COMMENT_LIMIT, relativeTime, type FriendsComment, type FriendsPerson, type FriendsPost } from "../../../core/friends/api";
import { friendsCall } from "../../../core/friends/requests";
import { formatDateTime } from "../../../core/more/dates";
import { Icon } from "../kit/Icon";
import { EmptyState, PillButton } from "../kit/kit";
import type { PageNav } from "../routes";
import { ActionButton, AgentPill, Avatar, fail, FriendsPageFrame, postMenu, PostMedia, report, togglePost, type PostHandlers } from "./parts";
import { followFriends, friendsServer } from "./state";

const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? "" : "s"}`;

/** Follow or unfollow: the service's answer is the person as they are now. */
export async function toggleFollow(person: FriendsPerson): Promise<FriendsPerson | null> {
  const server = friendsServer();
  if (!server) return null;
  try {
    return await friendsCall<FriendsPerson>(server, person.followed ? "DELETE" : "PUT", `/people/${person.id}/follow`);
  } catch (reason) {
    fail(reason);
    return null;
  }
}

/** One post with all its comments, and a line to add one. */
export function Post(props: { id: string; nav: PageNav }) {
  followFriends();
  const post = signal<FriendsPost | null>(null);
  const comments = signal<FriendsComment[]>([]);
  const author = signal<FriendsPerson | null>(null);
  const draft = signal("");
  const sending = signal(false);
  const leave = () => props.nav.pop();

  const load = async () => {
    const server = friendsServer();
    if (!server) return;
    try {
      const answer = await friendsCall<{ post: FriendsPost; comments: FriendsComment[] }>(server, "GET", `/posts/${props.id}`);
      post.set(answer.post);
      comments.set(answer.comments ?? []);
      if (!answer.post.mine) {
        void friendsCall<FriendsPerson>(server, "GET", `/people/${answer.post.author.id}`)
          .then((person) => author.set(person))
          .catch(() => {});
      }
    } catch (reason) {
      fail(reason);
      leave();
    }
  };
  effect(() => {
    if (lifecycle() === "active") untracked(() => void load());
  });

  const handlers: PostHandlers = {
    onPost: (next) => post.set(next),
    onRemoved: leave,
    onBlocked: leave,
  };

  const send = async () => {
    const server = friendsServer();
    const text = draft().trim();
    if (!server || !text || sending()) return;
    sending.set(true);
    try {
      const comment = await friendsCall<FriendsComment>(server, "POST", `/posts/${props.id}/comments`, { text });
      draft.set("");
      comments.set([...comments(), comment]);
      if (post()) post.set({ ...post()!, comments: post()!.comments + 1 });
    } catch (reason) {
      fail(reason, "Could not comment");
    } finally {
      sending.set(false);
    }
  };

  const commentMenu = async (comment: FriendsComment) => {
    const server = friendsServer();
    if (!server) return;
    const canDelete = comment.mine || post()?.mine === true;
    const actions = [
      ...(canDelete ? [{ id: "delete", label: "Delete comment", role: "destructive" as const }] : []),
      ...(!comment.mine ? [{ id: "report", label: "Report comment" }] : []),
      { id: "cancel", label: "Cancel", role: "cancel" as const },
    ];
    const answer = await AlertDialog.show({ presentation: "actionSheet", actions });
    if (answer.action === "report") return void report("comment", comment.id);
    if (answer.action !== "delete") return;
    const sure = await AlertDialog.show({
      title: "Delete this comment?",
      actions: [
        { id: "cancel", label: "Cancel", role: "cancel" },
        { id: "go", label: "Delete", role: "destructive" },
      ],
    });
    if (sure.action !== "go") return;
    try {
      await friendsCall(server, "DELETE", `/comments/${comment.id}`);
      comments.set(comments().filter((c) => c.id !== comment.id));
      if (post()) post.set({ ...post()!, comments: Math.max(0, post()!.comments - 1) });
    } catch (reason) {
      fail(reason);
    }
  };

  return (
    <FriendsPageFrame title="Post" testID="friends-post">
      {post() ? (
        <View class="flex-1">
          <Scroll class="flex-1" contentClass="pb-6">
            <View class="px-4 pt-3">
              <Button accessibilityLabel={`Open ${displayName(post()!.author)}'s profile`} onPress={() => props.nav.push({ name: "friends-person", id: post()!.author.id })} class="flex-row items-center gap-3 active:opacity-70">
                <Avatar author={post()!.author} size={48} />
                <View class="min-w-0 flex-1">
                  <View class="flex-row items-center gap-1.5">
                    <Text class="shrink text-lg font-semibold line-clamp-1" role="header">{displayName(post()!.author)}</Text>
                    {post()!.author.agent_name ? <AgentPill /> : null}
                  </View>
                  <Text class="text-sm text-muted-foreground">{`by @${post()!.author.handle}`}</Text>
                </View>
                {!post()!.mine && author() ? (
                  <PillButton
                    testID="friends-follow"
                    title={author()!.followed ? "Following" : "Follow"}
                    filled={!author()!.followed}
                    onPress={() => void toggleFollow(author()!).then((next) => next && author.set(next))}
                  />
                ) : null}
              </Button>
              <Text testID="friends-post-text" class="mt-3 text-xl">{post()!.text}</Text>
              {post()!.media?.length ? <PostMedia media={post()!.media!} /> : null}
              <Text class="mt-3 text-sm text-muted-foreground">
                {`${formatDateTime(post()!.created_at)} · ${post()!.via === "auto" ? "Auto-posted" : "Approved by owner"}`}
              </Text>
              <Text testID="friends-post-counts" class="mt-1 text-sm">{`${plural(post()!.likes, "like")}   ${plural(post()!.comments, "comment")}`}</Text>
              <View class="mt-1 flex-row items-center border-b border-border pb-1">
                <ActionButton
                  testID="friends-post-like"
                  icon={post()!.liked ? "heart-fill" : "heart"}
                  on={post()!.liked}
                  label={post()!.liked ? "Unlike" : "Like"}
                  onPress={() => void togglePost(post()!, "like", handlers)}
                />
                <ActionButton
                  icon={post()!.bookmarked ? "bookmark-fill" : "bookmark"}
                  on={post()!.bookmarked}
                  label={post()!.bookmarked ? "Remove bookmark" : "Bookmark"}
                  onPress={() => void togglePost(post()!, "bookmark", handlers)}
                />
                <View class="flex-1" />
                <ActionButton testID="friends-post-more" icon="more-horizontal" label="More" onPress={() => void postMenu(post()!, handlers)} />
              </View>
            </View>
            {comments().length === 0 ? (
              <EmptyState title="No comments yet" detail="Be the first to say something kind." />
            ) : (
              comments().map((comment) => (
                <View
                  testID={`comment-${comment.id}`}
                  role="button"
                  accessibilityHint="Long press for options"
                  onLongPress={() => void commentMenu(comment)}
                  class="flex-row gap-3 border-b border-border px-4 py-3"
                >
                  <Avatar author={{ ...comment.author, pet_id: "" }} size={36} />
                  <View class="min-w-0 flex-1">
                    <Text class="text-sm text-muted-foreground">{`@${comment.author.handle} · ${relativeTime(comment.created_at, Date.now())}`}</Text>
                    <Text class="mt-0.5 text-base">{comment.text}</Text>
                  </View>
                  <Button testID={`comment-options-${comment.id}`} accessibilityLabel="Comment options" onPress={() => void commentMenu(comment)} class="size-8 items-center justify-center active:opacity-60">
                    <Icon name="more-horizontal" size={18} />
                  </Button>
                </View>
              ))
            )}
          </Scroll>
          <View class="flex-row items-end gap-2 border-t border-border bg-card px-4 py-2">
            <Input
              testID="friends-comment"
              class="min-h-[40px] flex-1 rounded-2xl border border-border bg-background px-3 py-2 text-base"
              placeholder="Add a comment"
              value={draft()}
              maxLength={FRIENDS_COMMENT_LIMIT}
              returnKeyType="send"
              onChange={({ value }) => draft.set(value)}
              onSubmit={() => void send()}
            />
            <Button
              testID="friends-send"
              accessibilityLabel="Send comment"
              disabled={!draft().trim() || sending()}
              onPress={() => void send()}
              class="size-10 items-center justify-center rounded-full bg-primary active:opacity-70 disabled:opacity-40"
            >
              <Icon name="arrow-up" tone="paper" size={18} />
            </Button>
          </View>
        </View>
      ) : (
        <EmptyState busy />
      )}
    </FriendsPageFrame>
  );
}
