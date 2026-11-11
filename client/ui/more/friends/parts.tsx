import type { JSX } from "nsh";
import { computed, effect, Image, keyboardFrame, List, signal, Text, untracked, Video, View, windowSize, type VideoRef, type ViewRef, lifecycle } from "nsh";
import { AlertDialog, Button } from "nsh/base";
import { GuardedDialog as Dialog } from "../../shell/AppSheet";
import { NavPage } from "../../shell/NavPage";
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
import { afterScroll, isPlaying, stopped, VIDEO_VISIBLE_SHARE, type PlayingVideo } from "../../../core/friends/video";
import { Icon, type IconName } from "../kit/Icon";
import { EmptyState, Spinner } from "../kit/kit";
import { findPet } from "../pet/pets";
import type { PageNav } from "../routes";
import { currentServerId, followPage, serverFor } from "./state";

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

/**
 * How far the keyboard reaches over the bottom of a page's view: what a composer or
 * an editor at the bottom of the page rises by. The view's `ref` and `onLayout` are
 * these; the keyboard covers what lies below the page (the tab bar) first.
 */
export function keyboardLift() {
  const below = signal(0);
  let node: ViewRef | null = null;
  const measure = () => {
    void node?.measure().then((m) => {
      if (m) below.set(Math.max(0, windowSize().height - m.pagePosition.y - m.bounds.size.height));
    });
  };
  return {
    ref: (next: ViewRef) => {
      node = next;
    },
    onLayout: measure,
    lift: () => Math.max(0, keyboardFrame().height - below()),
  };
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

/** The one video playing anywhere in Friends; another one starting stops it. */
const playingVideo = signal<PlayingVideo | null>(null);
let owners = 0;

/** A name for a list or page whose videos play: its rows' videos stop together. */
export function videoOwner(): string {
  owners += 1;
  return `videos-${owners}`;
}

/** Stops `owner`'s video (of `postId`, or any). */
export function stopVideo(owner: string, postId?: string): void {
  playingVideo.set(stopped(playingVideo(), owner, postId));
}

function PlayDisc() {
  return (
    <View class="size-14 items-center justify-center rounded-full bg-primary/70"><Icon name="play" size={26} tone="paper" /></View>
  );
}

function SoundButton(props: { muted: boolean; onPress: () => void; testID?: string; class?: string }) {
  return (
    <Button
      testID={props.testID}
      accessibilityLabel={props.muted ? "Turn sound on" : "Turn sound off"}
      selected={!props.muted}
      hitSlop={8}
      onPress={props.onPress}
      class={`${props.class ?? "absolute bottom-2 right-2 size-8"} items-center justify-center rounded-full bg-primary/70 active:opacity-70`}
    >
      <Icon name={props.muted ? "volume-off" : "volume"} size={18} tone="paper" />
    </Button>
  );
}

/**
 * A post's video: its poster and length until tapped; nothing loads before. A tap
 * plays it muted, in place; a tap on the playing picture goes full screen, the sound
 * button turns sound on or off. It plays only while it is the one playing video, its
 * page is on top and the app is in front; at the end it goes back to the poster.
 */
function PostVideo(props: { media: FriendsMedia; postId: string; owner: string; active: () => boolean }) {
  const mine = computed(() => isPlaying(playingVideo(), props.owner, props.postId));
  const muted = signal(true);
  const paused = signal(false);
  const full = signal(false);
  let at = 0;
  let fullAt = 0;
  let inline: VideoRef | null = null;
  let wide: VideoRef | null = null;
  const aspect = () => singleMediaAspect(props.media.width, props.media.height);
  const label = () => durationLabel(props.media.duration_ms ?? 0);

  const start = () => {
    muted.set(true);
    paused.set(false);
    full.set(false);
    at = 0;
    playingVideo.set({ owner: props.owner, postId: props.postId });
  };
  const end = () => {
    full.set(false);
    stopVideo(props.owner, props.postId);
  };
  const closeFull = () => {
    if (!full()) return;
    full.set(false);
    at = fullAt;
    inline?.seek(at);
  };

  // Its page went under another one: it stops, back to the poster.
  effect(() => {
    if (mine() && !props.active()) untracked(end);
  });
  // The app left the front: it pauses, and shows Play.
  effect(() => {
    if (lifecycle() !== "active") untracked(() => mine() && paused.set(true));
  });

  return (
    <View style={{ width: "100%", aspectRatio: aspect() }}>{mine() ? (
      <View class="flex-1" style={{ backgroundColor: "#000000" }}>
        <Video
          testID="friends-video-player"
          ref={(node) => {
            inline = node;
          }}
          src={props.media.url}
          muted={muted()}
          paused={paused() || full()}
          class="absolute inset-0"
          accessibilityLabel="Video"
          onProgress={({ currentTime }) => {
            at = currentTime;
          }}
          onEnd={end}
        />
        <Button testID="friends-video-full-screen" accessibilityLabel="Full screen" onPress={() => {
          fullAt = at;
          full.set(true);
        }} class="absolute inset-0" />
        {paused() ? (
          <Button testID="friends-video-play" accessibilityLabel="Play" onPress={() => paused.set(false)} class="absolute inset-0 items-center justify-center">
            <PlayDisc />
          </Button>
        ) : null}
        <SoundButton testID="friends-video-sound" muted={muted()} onPress={() => muted.set(!muted())} />
        <Dialog
          testID="friends-video-dialog"
          open={full()}
          presentation="fullScreen"
          animation="fade"
          onOpenChange={({ open }) => !open && closeFull()}
          style={{ flex: 1, backgroundColor: "#000000" }}
        >
          <View class="flex-1">
            <Video
              testID="friends-video-wide"
              ref={(node) => {
                wide = node;
              }}
              src={props.media.url}
              controls
              muted={muted()}
              paused={!full()}
              style={{ flex: 1 }}
              accessibilityLabel="Video"
              onLoad={() => wide?.seek(fullAt)}
              onProgress={({ currentTime }) => {
                fullAt = currentTime;
              }}
              onEnd={end}
            />
            <View class="absolute left-0 right-0 top-0 flex-row items-center justify-between px-3" style={{ paddingTop: "calc(env(safe-area-inset-top) + 8px)" }}>
              <Button testID="friends-video-close" accessibilityLabel="Close" onPress={closeFull} class="size-10 items-center justify-center rounded-full bg-primary/70 active:opacity-70">
                <Icon name="close" size={18} tone="paper" />
              </Button>
              <SoundButton testID="friends-video-wide-sound" class="size-10" muted={muted()} onPress={() => muted.set(!muted())} />
            </View>
          </View>
        </Dialog>
      </View>
    ) : (
      <Button
        testID="friends-video"
        accessibilityLabel={`Play video, ${label()}`}
        onPress={start}
        class="flex-1 active:opacity-80"
      >
        {props.media.poster ? <Image src={props.media.poster.url} class="absolute inset-0" style={{ objectFit: "cover" }} /> : null}
        <View class="absolute inset-0 items-center justify-center">
          <PlayDisc />
        </View>
        {props.media.duration_ms ? (
          <View class="absolute bottom-2 right-2 rounded-md bg-primary/70 px-1.5 py-0.5">
            <Text class="text-xs text-primary-foreground">{label()}</Text>
          </View>
        ) : null}
      </Button>
    )}</View>
  );
}

/** A post's pictures in one frame (1 wide, 2 side by side, 3 one tall and two small, 4 in a square), or its video. */
export function PostMedia(props: { media: readonly FriendsMedia[]; postId: string; owner: string; active: () => boolean }) {
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
        <PostVideo media={first()} postId={props.postId} owner={props.owner} active={props.active} />
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

/**
 * What a list does when a post changes, goes, or its author is blocked. `from` is the
 * computer the posts came from: an action goes there, or nowhere once another
 * computer is the current one.
 */
export interface PostHandlers {
  from: string;
  onPost(post: FriendsPost): void;
  onRemoved(postId: string): void;
  onBlocked(personId: string): void;
}

/** Like or bookmark: shown at once, the service's answer replaces it, a failure puts it back. */
export async function togglePost(post: FriendsPost, field: "like" | "bookmark", handlers: PostHandlers): Promise<void> {
  const server = serverFor(handlers.from);
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

/** Blocks a person shown from computer `from` after asking; true when blocked. */
export async function blockPerson(person: Pick<FriendsAuthor, "id" | "handle">, from: string): Promise<boolean> {
  if (!serverFor(from)) return false;
  if (!(await confirm(`Block @${person.handle}?`, "Neither of you will see the other's posts, comments or profile.", "Block"))) return false;
  const server = serverFor(from);
  if (!server) return false;
  try {
    await friendsCall(server, "PUT", `/people/${person.id}/block`);
    return true;
  } catch (reason) {
    fail(reason);
    return false;
  }
}

/** Reports a post, a comment or a person shown from computer `from`, with the one fixed reason. */
export async function report(kind: "post" | "comment" | "person", targetId: string, from: string): Promise<void> {
  const server = serverFor(from);
  if (!server) return;
  const what = kind === "person" ? "profile" : kind;
  try {
    await friendsCall(server, "POST", "/reports", { kind, target_id: targetId, reason: "reported" });
    say({ title: "Reported", detail: `Thanks. Three reports from different people hide a ${what}.`, tone: "success" });
  } catch (reason) {
    fail(reason);
  }
}

/** What the Block item of a menu says under it: the native sheet has one line for the whole sheet. */
export const BLOCK_DETAIL = "You stop seeing each other";

/** A post's More menu: delete your own; report or block someone else's. */
export async function postMenu(post: FriendsPost, handlers: PostHandlers): Promise<void> {
  if (!serverFor(handlers.from)) return;
  const actions = post.mine
    ? [{ id: "delete", label: "Delete post", role: "destructive" as const }]
    : [
        { id: "report", label: "Report post" },
        { id: "block", label: `Block @${post.author.handle}`, role: "destructive" as const },
      ];
  const answer = await AlertDialog.show({
    presentation: "actionSheet",
    description: post.mine ? undefined : `Block @${post.author.handle}: ${BLOCK_DETAIL.toLowerCase()}`,
    actions: [...actions, { id: "cancel", label: "Cancel", role: "cancel" }],
  });
  if (answer.action === "delete") {
    if (!(await confirm("Delete this post?", "It disappears from Friends for everyone.", "Delete"))) return;
    const server = serverFor(handlers.from);
    if (!server) return;
    try {
      await friendsCall(server, "DELETE", `/posts/${post.id}`);
      handlers.onRemoved(post.id);
    } catch (reason) {
      fail(reason);
    }
  } else if (answer.action === "report") {
    await report("post", post.id, handlers.from);
  } else if (answer.action === "block") {
    if (await blockPerson(post.author, handlers.from)) handlers.onBlocked(post.author.id);
  }
}

/** One post in a timeline: avatar, who and when, the text, its media and the quiet action row. */
export function PostCard(props: { post: FriendsPost; now: number; nav: PageNav; handlers: PostHandlers; owner: string; active: () => boolean }) {
  const name = () => displayName(props.post.author);
  // A like replaces the post; its media (a video playing) stays as it is.
  const media = computed(() => props.post.media ?? [], { equals: (a, b) => a.length === b.length && a.every((m, i) => m.url === b[i].url) });
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
        {media().length ? ((list) => untracked(() => <PostMedia media={list} postId={props.post.id} owner={props.owner} active={props.active} />))(media()) : null}
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
 * bookmark, delete, report and block wired in. Pull to load it again. It reads the
 * list again when its page comes back to the top, and drops it for the next
 * computer's on a switch. One of its videos plays at a time, only while its row is on
 * screen.
 */
export function PostList(props: {
  path: string;
  nav: PageNav;
  top: () => boolean;
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
  const from = signal(currentServerId());
  const owner = videoOwner();
  let generation = 0;

  const load = async (append: boolean) => {
    const id = currentServerId();
    const server = serverFor(id);
    if (!server) return;
    const mine = ++generation;
    try {
      const page = await friendsCall<PostPage>(server, "GET", withCursor(props.path, append ? next() : ""));
      if (mine !== generation || currentServerId() !== id) return;
      from.set(id);
      error.set(null);
      next.set(page.next ?? "");
      posts.set(append && posts() ? appendPage(posts()!, page.posts ?? []) : (page.posts ?? []));
    } catch (reason) {
      if (mine === generation && currentServerId() === id) error.set(reason instanceof Error && reason.message ? reason.message : "Could not load");
    }
  };

  followPage({
    top: props.top,
    clear: () => {
      generation++;
      stopVideo(owner);
      posts.set(null);
      next.set("");
      error.set(null);
      from.set(currentServerId());
    },
    load: () => void load(false),
  });

  const handlers = (): PostHandlers => {
    const shown = from();
    const live = () => from() === shown && currentServerId() === shown;
    return {
      from: shown,
      onPost: (post) => live() && posts.set(posts() ? replacePost(posts()!, post) : posts()),
      onRemoved: (id) => live() && posts.set(posts()?.filter((p) => p.id !== id) ?? null),
      onBlocked: (personId) => live() && posts.set(posts()?.filter((p) => p.author.id !== personId) ?? null),
    };
  };

  return (
    <View class="flex-1 bg-background">{posts() === null && !error() ? (
    <View class="flex-1 bg-background">
      {props.header ?? null}
      <EmptyState busy />
    </View>
  ) : (
    <List keyboardShouldPersistTaps="handled"
      testID={props.testID}
      class="flex-1 bg-background"
      each={posts() ?? []}
      keyExtractor={(post) => post.id}
      header={props.header ? () => props.header! : undefined}
      viewabilityConfig={{ itemVisibleThreshold: VIDEO_VISIBLE_SHARE }}
      onViewableItemsChange={({ viewableItems }) => playingVideo.set(afterScroll(playingVideo(), owner, viewableItems.map((token) => token.item.id)))}
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
      {(post) => <PostCard post={post()} now={Date.now()} nav={props.nav} handlers={handlers()} owner={owner} active={props.top} />}
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
