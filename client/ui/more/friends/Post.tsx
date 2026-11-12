import { computed, effect, Scroll, signal, Text, untracked, View, type InputRef } from "nsh";
import { AlertDialog, Button, Input } from "nsh/base";
import { displayName, FRIENDS_COMMENT_LIMIT, relativeTime, type FriendsComment, type FriendsPerson, type FriendsPost } from "../../../core/friends/api";
import { friendsCall } from "../../../core/friends/requests";
import { formatDateTime } from "../../../core/more/dates";
import { Icon } from "../../kit/Icon";
import { EmptyState, PillButton } from "../../kit/kit";
import type { PageNav } from "../routes";
import { ActionButton, AgentPill, Avatar, fail, FriendsPageFrame, keyboardLift, postMenu, PostMedia, report, togglePost, videoOwner, type PostHandlers } from "./parts";
import { currentServerId, followFriends, followPage, serverFor } from "./state";

const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? "" : "s"}`;

/** Follow or unfollow someone shown from computer `from`: the service's answer is the person as they are now. */
export async function toggleFollow(person: FriendsPerson, from: string): Promise<FriendsPerson | null> {
  const server = serverFor(from);
  if (!server) return null;
  try {
    return await friendsCall<FriendsPerson>(server, person.followed ? "DELETE" : "PUT", `/people/${person.id}/follow`);
  } catch (reason) {
    fail(reason);
    return null;
  }
}

/**
 * Leaves a page that has nothing to show: at once while it is on top, else as soon
 * as it is (a page under another one never pops the page above it).
 */
export function leaveWhenOnTop(nav: PageNav, top: () => boolean): () => void {
  const gone = signal(false);
  effect(() => {
    if (gone() && top()) {
      untracked(() => {
        gone.set(false);
        nav.pop();
      });
    }
  });
  return () => gone.set(true);
}

/** One post with all its comments, and a line to add one. */
export function Post(props: { id: string; nav: PageNav; top: () => boolean }) {
  followFriends();
  const post = signal<FriendsPost | null>(null);
  const comments = signal<FriendsComment[]>([]);
  const author = signal<FriendsPerson | null>(null);
  const draft = signal("");
  const sending = signal(false);
  const from = signal(currentServerId());
  const owner = videoOwner();
  const keyboard = keyboardLift();
  const leave = leaveWhenOnTop(props.nav, props.top);
  let input: InputRef | null = null;
  let generation = 0;

  const load = async () => {
    const id = currentServerId();
    const server = serverFor(id);
    if (!server) return;
    const mine = ++generation;
    const live = () => mine === generation && currentServerId() === id;
    try {
      const answer = await friendsCall<{ post: FriendsPost; comments: FriendsComment[] }>(server, "GET", `/posts/${props.id}`);
      if (!live()) return;
      from.set(id);
      post.set(answer.post);
      comments.set(answer.comments ?? []);
      if (!answer.post.mine) {
        void friendsCall<FriendsPerson>(server, "GET", `/people/${answer.post.author.id}`)
          .then((person) => live() && author.set(person))
          .catch(() => {});
      } else {
        author.set(null);
      }
    } catch (reason) {
      if (!live()) return;
      fail(reason);
      leave();
    }
  };
  followPage({
    top: props.top,
    clear: () => {
      generation++;
      post.set(null);
      comments.set([]);
      author.set(null);
      draft.set("");
      from.set(currentServerId());
    },
    load: () => void load(),
  });

  // A like or a new comment changes the post; the page (and the comment being typed) stays.
  const loaded = computed(() => post() !== null);
  const mediaCount = computed(() => post()?.media?.length ?? 0);
  const shown = () => from() === currentServerId();
  const handlers = (): PostHandlers => ({
    from: from(),
    onPost: (next) => shown() && post()?.id === next.id && post.set(next),
    onRemoved: leave,
    onBlocked: leave,
  });

  const send = async () => {
    const shownFrom = from();
    const server = serverFor(shownFrom);
    const text = draft().trim();
    if (!server || !text || sending()) return;
    sending.set(true);
    try {
      const comment = await friendsCall<FriendsComment>(server, "POST", `/posts/${props.id}/comments`, { text });
      if (from() !== shownFrom) return;
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
    const shownFrom = from();
    if (!serverFor(shownFrom)) return;
    const canDelete = comment.mine || post()?.mine === true;
    const actions = [
      ...(canDelete ? [{ id: "delete", label: "Delete comment", role: "destructive" as const }] : []),
      ...(!comment.mine ? [{ id: "report", label: "Report comment" }] : []),
      { id: "cancel", label: "Cancel", role: "cancel" as const },
    ];
    const answer = await AlertDialog.show({ presentation: "actionSheet", actions });
    if (answer.action === "report") return void report("comment", comment.id, shownFrom);
    if (answer.action !== "delete") return;
    const sure = await AlertDialog.show({
      title: "Delete this comment?",
      actions: [
        { id: "cancel", label: "Cancel", role: "cancel" },
        { id: "go", label: "Delete", role: "destructive" },
      ],
    });
    if (sure.action !== "go") return;
    const server = serverFor(shownFrom);
    if (!server) return;
    try {
      await friendsCall(server, "DELETE", `/comments/${comment.id}`);
      if (from() !== shownFrom) return;
      comments.set(comments().filter((c) => c.id !== comment.id));
      if (post()) post.set({ ...post()!, comments: Math.max(0, post()!.comments - 1) });
    } catch (reason) {
      fail(reason);
    }
  };

  return (
    <FriendsPageFrame title="Post" testID="friends-post">
      {loaded() ? (
        <View class="flex-1" ref={keyboard.ref} onLayout={keyboard.onLayout}>
          <Scroll class="flex-1" contentClass="pb-6" keyboardShouldPersistTaps="handled">
            <View class="px-4 pt-3">
              <View class="flex-row items-center gap-3">
                <Button
                  testID="friends-post-author"
                  accessibilityLabel={`Open ${displayName(post()!.author)}'s profile`}
                  onPress={() => props.nav.push({ name: "friends-person", id: post()!.author.id })}
                  class="min-w-0 flex-1 flex-row items-center gap-3 active:opacity-70"
                >
                  <Avatar author={post()!.author} size={48} />
                  <View class="min-w-0 flex-1">
                    <View class="flex-row items-center gap-1.5">
                      <Text class="shrink text-title font-bold line-clamp-1" role="header">{displayName(post()!.author)}</Text>
                      {post()!.author.agent_name ? <AgentPill /> : null}
                    </View>
                    <Text class="text-sub text-muted-foreground">{`by @${post()!.author.handle}`}</Text>
                  </View>
                </Button>
                {!post()!.mine && author() ? (
                  <PillButton
                    testID="friends-follow"
                    title={author()!.followed ? "Following" : "Follow"}
                    filled={!author()!.followed}
                    onPress={() => {
                      const shownFrom = from();
                      void toggleFollow(author()!, shownFrom).then((next) => next && from() === shownFrom && author.set(next));
                    }}
                  />
                ) : null}
              </View>
              <Text testID="friends-post-text" class="mt-3 text-body">{post()!.text}</Text>
              {mediaCount() ? <PostMedia media={post()!.media!} postId={post()!.id} owner={owner} active={props.top} /> : null}
              <Text class="mt-3 text-sub text-muted-foreground">
                {`${formatDateTime(post()!.created_at)} · ${post()!.via === "auto" ? "Auto-posted" : "Approved by owner"}`}
              </Text>
              <Text testID="friends-post-counts" class="mt-1 text-sub">{`${plural(post()!.likes, "like")}   ${plural(post()!.comments, "comment")}`}</Text>
              <View class="mt-3 flex-row items-center gap-2 pb-1">
                <ActionButton
                  soft
                  testID="friends-post-like"
                  icon={post()!.liked ? "heart-fill" : "heart"}
                  on={post()!.liked}
                  label={post()!.liked ? "Unlike" : "Like"}
                  onPress={() => void togglePost(post()!, "like", handlers())}
                />
                <ActionButton soft testID="friends-post-comment" icon="chat" label="Comment" onPress={() => input?.focus()} />
                <ActionButton
                  icon={post()!.bookmarked ? "bookmark-fill" : "bookmark"}
                  on={post()!.bookmarked}
                  label={post()!.bookmarked ? "Remove bookmark" : "Bookmark"}
                  onPress={() => void togglePost(post()!, "bookmark", handlers())}
                />
                <View class="flex-1" />
                <ActionButton testID="friends-post-more" icon="more-horizontal" label="More" onPress={() => void postMenu(post()!, handlers())} />
              </View>
            </View>
            {comments().length === 0 ? (
              <EmptyState title="No comments yet" />
            ) : (
              comments().map((comment) => (
                <View
                  testID={`comment-${comment.id}`}
                  role="button"
                  accessibilityHint="Long press for options"
                  onLongPress={() => void commentMenu(comment)}
                  class="mx-4 mt-2 flex-row gap-3 rounded-(--radius-group) bg-card px-4 py-3"
                >
                  <Avatar author={{ ...comment.author, pet_id: "" }} size={36} />
                  <View class="min-w-0 flex-1">
                    <Text class="text-sub text-muted-foreground">{`@${comment.author.handle} · ${relativeTime(comment.created_at, Date.now())}`}</Text>
                    <Text class="mt-0.5 text-body">{comment.text}</Text>
                  </View>
                  <Button testID={`comment-options-${comment.id}`} accessibilityLabel="Comment options" onPress={() => void commentMenu(comment)} class="size-8 items-center justify-center active:opacity-60">
                    <Icon name="more-horizontal" size={18} />
                  </Button>
                </View>
              ))
            )}
          </Scroll>
          <View testID="friends-composer" class="flex-row items-end gap-2 bg-background px-4 py-2" style={{ marginBottom: keyboard.lift() }}>
            <Input
              testID="friends-comment"
              ref={(node) => {
                input = node;
              }}
              class="min-h-[44px] flex-1 rounded-full bg-card px-4 py-2.5 text-body"
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
              class="size-11 items-center justify-center rounded-full bg-primary active:opacity-70 disabled:opacity-40"
            >
              <Icon name="arrow-up" tone="canvas" size={18} />
            </Button>
          </View>
        </View>
      ) : (
        <EmptyState busy />
      )}
    </FriendsPageFrame>
  );
}
