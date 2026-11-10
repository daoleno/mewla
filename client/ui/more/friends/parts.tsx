import type { JSX } from "nsh";
import { Image, List, signal, Text, Video, View } from "nsh";
import { AlertDialog, Button, NavPage } from "nsh/base";
import {
  displayName,
  durationLabel,
  initials,
  mediaLayout,
  relativeTime,
  singleMediaAspect,
  type FriendsAuthor,
  type FriendsMedia,
  type FriendsPost,
} from "../../../core/friends/api";
import { appendPage, replacePost, toggled, withCursor, type PostPage } from "../../../core/friends/paging";
import { friendsCall } from "../../../core/friends/requests";
import { Icon, type IconName } from "../kit/Icon";
import { EmptyState, Spinner } from "../kit/kit";
import { findPet } from "../pet/pets";
import type { PageNav } from "../routes";
import { friendsServer } from "./state";

/** A person's face: their pet's portrait in a rounded square, else their initials in a circle. */
export function Avatar(props: { author: Pick<FriendsAuthor, "agent_name" | "handle" | "pet_id">; size?: number }) {
  const size = () => props.size ?? 44;
  const portrait = () => findPet(props.author.pet_id)?.portrait;
  return (<>{portrait() ? (
    <Image src={portrait()!} style={{ width: size(), height: size(), borderRadius: size() * 0.28 }} accessibilityElementsHidden />
  ) : (
    <View class="items-center justify-center rounded-full bg-secondary" style={{ width: size(), height: size() }} accessibilityElementsHidden>
      <Text class="font-semibold text-muted-foreground" style={{ fontSize: Math.round(size() * 0.36) }}>{initials(props.author)}</Text>
    </View>
  )}</>);
}

/** "Agent": the person posts as their agent. */
export function AgentPill() {
  return (
    <View class="rounded-full border border-border px-1.5 py-px">
      <Text class="text-[11px] font-medium text-muted-foreground">Agent</Text>
    </View>
  );
}

/** What a Friends action said: shown at the top of the page for a few seconds. */
export interface Flash {
  title: string;
  detail: string;
  tone: "success" | "error";
}

export const flash = signal<Flash | null>(null);
let flashTimer: ReturnType<typeof setTimeout> | null = null;

export function say(next: Flash): void {
  flash.set(next);
  if (flashTimer) clearTimeout(flashTimer);
  flashTimer = setTimeout(() => flash.set(null), 4000);
}

/** A failed Friends action, in the service's words. */
export function fail(reason: unknown, fallback = "That did not work"): void {
  say({ title: "Friends", detail: reason instanceof Error && reason.message ? reason.message : fallback, tone: "error" });
}

export function FlashLine() {
  return (
    <>{flash() ? (
    <View testID="friends-flash" role="alert" class="mx-4 mt-2 flex-row items-start gap-3 rounded-2xl border border-border bg-card px-4 py-3">
      <Icon name={flash()!.tone === "error" ? "alert-circle" : "check-circle"} size={18} tone={flash()!.tone === "error" ? "failed" : "ready"} />
      <View class="flex-1">
        <Text class="text-sm font-medium">{flash()!.title}</Text>
        <Text class="mt-0.5 text-sm text-muted-foreground">{flash()!.detail}</Text>
      </View>
    </View>
  ) : null}</>
  );
}

/** A quiet icon button of a post's action row. */
export function ActionButton(props: { icon: IconName; label: string; count?: number; on?: boolean; testID?: string; onPress: () => void }) {
  return (
    <Button testID={props.testID} accessibilityLabel={props.label} selected={props.on} onPress={props.onPress} class="min-h-[36px] flex-row items-center gap-1.5 pr-4 active:opacity-60">
      <Icon name={props.icon} size={20} tone={props.on ? "ink" : "soft"} />
      {props.count !== undefined ? <Text class={props.on ? "text-sm" : "text-sm text-muted-foreground"} style={{ fontVariant: ["tabular-nums"] }}>{String(props.count)}</Text> : null}
    </Button>
  );
}

/** A post's pictures in one frame (1 wide, 2 side by side, 3 one tall and two small, 4 in a square), or its video's poster. */
export function PostMedia(props: { media: readonly FriendsMedia[] }) {
  const playing = signal(false);
  const first = () => props.media[0];
  const tile = (item: FriendsMedia, index: number, count: number) => (
    <Image
      src={item.url}
      class="flex-1 bg-secondary"
      style={{ objectFit: "cover" }}
      accessibilityLabel={`Picture ${index + 1} of ${count}`}
    />
  );
  return (
    <View class="mt-2 overflow-hidden rounded-xl border border-border bg-secondary">
      {first().kind === "video" ? (
        playing() ? (
          <Video src={first().url} controls style={{ width: "100%", aspectRatio: singleMediaAspect(first().width, first().height) }} />
        ) : (
          <Button
            testID="friends-video"
            accessibilityLabel={`Play video, ${durationLabel(first().duration_ms ?? 0)}`}
            onPress={() => playing.set(true)}
            class="active:opacity-80"
            style={{ width: "100%", aspectRatio: singleMediaAspect(first().width, first().height) }}
          >
            {first().poster ? <Image src={first().poster!.url} class="absolute inset-0" style={{ objectFit: "cover" }} /> : null}
            <View class="absolute inset-0 items-center justify-center">
              <View class="size-12 items-center justify-center rounded-full bg-primary/70"><Icon name="play" tone="paper" /></View>
            </View>
            <View class="absolute bottom-2 right-2 rounded-md bg-primary/70 px-1.5 py-0.5">
              <Text class="text-xs text-primary-foreground">{durationLabel(first().duration_ms ?? 0)}</Text>
            </View>
          </Button>
        )
      ) : mediaLayout(props.media.length) === "single" ? (
        <View style={{ width: "100%", aspectRatio: singleMediaAspect(first().width, first().height) }}>{tile(first(), 0, 1)}</View>
      ) : mediaLayout(props.media.length) === "pair" ? (
        <View class="flex-row gap-0.5" style={{ width: "100%", aspectRatio: 2 }}>{props.media.slice(0, 2).map((m, i) => tile(m, i, 2))}</View>
      ) : mediaLayout(props.media.length) === "trio" ? (
        <View class="flex-row gap-0.5" style={{ width: "100%", aspectRatio: 1.25 }}>
          {tile(props.media[0], 0, 3)}
          <View class="flex-1 gap-0.5">{props.media.slice(1, 3).map((m, i) => tile(m, i + 1, 3))}</View>
        </View>
      ) : (
        <View class="gap-0.5" style={{ width: "100%", aspectRatio: 1.25 }}>
          <View class="flex-1 flex-row gap-0.5">{props.media.slice(0, 2).map((m, i) => tile(m, i, 4))}</View>
          <View class="flex-1 flex-row gap-0.5">{props.media.slice(2, 4).map((m, i) => tile(m, i + 2, 4))}</View>
        </View>
      )}
    </View>
  );
}

/** What a list does when a post changes, goes, or its author is blocked. */
export interface PostHandlers {
  onPost(post: FriendsPost): void;
  onRemoved(postId: string): void;
  onBlocked(personId: string): void;
}

/** Like or bookmark: shown at once, the service's answer replaces it, a failure puts it back. */
export async function togglePost(post: FriendsPost, field: "like" | "bookmark", handlers: PostHandlers): Promise<void> {
  const server = friendsServer();
  if (!server) return;
  const on = field === "like" ? post.liked : post.bookmarked;
  handlers.onPost(toggled(post, field));
  try {
    const answer = await friendsCall<FriendsPost | { ok: boolean }>(server, on ? "DELETE" : "PUT", `/posts/${post.id}/${field}`);
    if (answer && "id" in answer) handlers.onPost(answer);
  } catch (reason) {
    handlers.onPost(post);
    fail(reason);
  }
}

async function confirm(title: string, description: string, action: string): Promise<boolean> {
  const answer = await AlertDialog.show({
    title,
    description,
    actions: [
      { id: "cancel", label: "Cancel", role: "cancel" },
      { id: "go", label: action, role: "destructive" },
    ],
  });
  return answer.action === "go";
}

/** Blocks a person after asking; true when blocked. */
export async function blockPerson(person: Pick<FriendsAuthor, "id" | "handle">): Promise<boolean> {
  const server = friendsServer();
  if (!server) return false;
  if (!(await confirm(`Block @${person.handle}?`, "Neither of you will see the other's posts, comments or profile.", "Block"))) return false;
  try {
    await friendsCall(server, "PUT", `/people/${person.id}/block`);
    return true;
  } catch (reason) {
    fail(reason);
    return false;
  }
}

/** Reports a post, a comment or a person with the one fixed reason. */
export async function report(kind: "post" | "comment" | "person", targetId: string): Promise<void> {
  const server = friendsServer();
  if (!server) return;
  const what = kind === "person" ? "profile" : kind;
  try {
    await friendsCall(server, "POST", "/reports", { kind, target_id: targetId, reason: "reported" });
    say({ title: "Reported", detail: `Thanks. Three reports from different people hide a ${what}.`, tone: "success" });
  } catch (reason) {
    fail(reason);
  }
}

/** A post's More menu: delete your own; report or block someone else's. */
export async function postMenu(post: FriendsPost, handlers: PostHandlers): Promise<void> {
  const server = friendsServer();
  if (!server) return;
  const actions = post.mine
    ? [{ id: "delete", label: "Delete post", role: "destructive" as const }]
    : [
        { id: "report", label: "Report post" },
        { id: "block", label: `Block @${post.author.handle}`, role: "destructive" as const },
      ];
  const answer = await AlertDialog.show({
    presentation: "actionSheet",
    actions: [...actions, { id: "cancel", label: "Cancel", role: "cancel" }],
  });
  if (answer.action === "delete") {
    if (!(await confirm("Delete this post?", "It disappears from Friends for everyone.", "Delete"))) return;
    try {
      await friendsCall(server, "DELETE", `/posts/${post.id}`);
      handlers.onRemoved(post.id);
    } catch (reason) {
      fail(reason);
    }
  } else if (answer.action === "report") {
    await report("post", post.id);
  } else if (answer.action === "block") {
    if (await blockPerson(post.author)) handlers.onBlocked(post.author.id);
  }
}

/** One post in a timeline: avatar, who and when, the text, its media and the quiet action row. */
export function PostCard(props: { post: FriendsPost; now: number; nav: PageNav; handlers: PostHandlers }) {
  const name = () => displayName(props.post.author);
  return (
    <View testID={`post-${props.post.id}`} class="flex-row gap-3 border-b border-border px-4 pb-1 pt-3">
      <Button accessibilityLabel={`Open ${name()}'s profile`} onPress={() => props.nav.push({ name: "friends-person", id: props.post.author.id })} class="active:opacity-70">
        <Avatar author={props.post.author} />
      </Button>
      <View class="min-w-0 flex-1">
        <Button testID={`open-post-${props.post.id}`} accessibilityLabel={`Open post by ${name()}`} onPress={() => props.nav.push({ name: "friends-post", id: props.post.id })} class="active:opacity-70">
          <View class="flex-row items-center gap-1.5">
            <Text class="shrink text-base font-semibold line-clamp-1">{name()}</Text>
            {props.post.author.agent_name ? <AgentPill /> : null}
            <Text class="shrink text-sm text-muted-foreground line-clamp-1">{`@${props.post.author.handle} · ${relativeTime(props.post.created_at, props.now)}`}</Text>
          </View>
          <Text class="mt-1 text-base">{props.post.text}</Text>
        </Button>
        {props.post.media?.length ? <PostMedia media={props.post.media} /> : null}
        <View class="mt-1 flex-row items-center">
          <ActionButton icon="chat" label={`${props.post.comments} comments`} count={props.post.comments} onPress={() => props.nav.push({ name: "friends-post", id: props.post.id })} />
          <ActionButton
            testID={`like-${props.post.id}`}
            icon={props.post.liked ? "heart-fill" : "heart"}
            on={props.post.liked}
            label={props.post.liked ? `Unlike, ${props.post.likes} likes` : `Like, ${props.post.likes} likes`}
            count={props.post.likes}
            onPress={() => void togglePost(props.post, "like", props.handlers)}
          />
          <ActionButton
            testID={`bookmark-${props.post.id}`}
            icon={props.post.bookmarked ? "bookmark-fill" : "bookmark"}
            on={props.post.bookmarked}
            label={props.post.bookmarked ? "Remove bookmark" : "Bookmark"}
            onPress={() => void togglePost(props.post, "bookmark", props.handlers)}
          />
          <View class="flex-1" />
          <ActionButton testID={`more-${props.post.id}`} icon="more-horizontal" label="More" onPress={() => void postMenu(props.post, props.handlers)} />
        </View>
      </View>
    </View>
  );
}

/**
 * A paged list of posts from `path` (30 a page, `before=` the last cursor) with like,
 * bookmark, delete, report and block wired in. Pull to load it again.
 */
export function PostList(props: {
  path: string;
  nav: PageNav;
  header?: JSX.Element;
  emptyTitle: string;
  emptyDetail?: string;
  emptyIcon?: IconName;
  testID?: string;
}) {
  const posts = signal<FriendsPost[] | null>(null);
  const next = signal("");
  const error = signal<string | null>(null);
  const refreshing = signal(false);
  const loadingMore = signal(false);
  let generation = 0;

  const load = async (append: boolean) => {
    const server = friendsServer();
    if (!server) return;
    const mine = ++generation;
    try {
      const page = await friendsCall<PostPage>(server, "GET", withCursor(props.path, append ? next() : ""));
      if (mine !== generation) return;
      error.set(null);
      next.set(page.next ?? "");
      posts.set(append && posts() ? appendPage(posts()!, page.posts ?? []) : (page.posts ?? []));
    } catch (reason) {
      if (mine === generation) error.set(reason instanceof Error && reason.message ? reason.message : "Could not load");
    }
  };
  void load(false);

  const handlers: PostHandlers = {
    onPost: (post) => posts.set(posts() ? replacePost(posts()!, post) : posts()),
    onRemoved: (id) => posts.set(posts()?.filter((p) => p.id !== id) ?? null),
    onBlocked: (personId) => posts.set(posts()?.filter((p) => p.author.id !== personId) ?? null),
  };

  return (
    <View class="flex-1 bg-background">{posts() === null && !error() ? (
    <View class="flex-1 bg-background">
      {props.header ?? null}
      <EmptyState busy />
    </View>
  ) : (
    <List
      testID={props.testID}
      class="flex-1 bg-background"
      each={posts() ?? []}
      keyExtractor={(post) => post.id}
      header={props.header ? () => props.header! : undefined}
      empty={() => (
        <EmptyState
          testID="friends-empty"
          title={error() ? "Could not load" : props.emptyTitle}
          detail={error() ?? props.emptyDetail}
          icon={error() ? "cloud-offline" : (props.emptyIcon ?? "people")}
        />
      )}
      footer={() => (loadingMore() ? <View class="items-center p-4"><Spinner /></View> : null)}
      loadMoreThreshold={0.6}
      onLoadMore={() => {
        if (!next() || loadingMore()) return;
        loadingMore.set(true);
        void load(true).finally(() => loadingMore.set(false));
      }}
      refreshing={refreshing()}
      onRefresh={() => {
        refreshing.set(true);
        void load(false).finally(() => refreshing.set(false));
      }}
    >
      {(post) => <PostCard post={post()} now={Date.now()} nav={props.nav} handlers={handlers} />}
    </List>
  )}</View>
  );
}

/** A Friends page: the system header and, under it, what an action last said. */
export function FriendsPageFrame(props: { title: string; testID?: string; headerRight?: JSX.Element; children?: JSX.Element }) {
  return (
    <NavPage title={props.title} testID={props.testID} headerRight={props.headerRight}>
      <View class="flex-1 bg-background">
        <FlashLine />
        {props.children}
      </View>
    </NavPage>
  );
}
