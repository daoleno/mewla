import { cleanup, computed, effect, Image, Scroll, signal, Text, untracked, View, type InputRef, type ScrollRef } from "nsh";
import { AlertDialog, Button, Input } from "nsh/base";
import { durationLabel, FRIENDS_POST_LIMIT, relativeTime, type FriendsDraft, type FriendsDraftMedia } from "../../../core/friends/api";
import { checkDraftText, discardDraft, draftMediaImage, postDraft, removeDraftMedia } from "../../../core/friends/requests";
import { Icon } from "../../kit/Icon";
import { EmptyState, Spinner } from "../../kit/kit";
import type { PageNav } from "../routes";
import { Avatar, FriendsPageFrame, keyboardLift } from "./parts";
import { currentServerId, followFriends, followPage, friends, refreshFriends, serverFor } from "./state";

/** One picture (or a video's poster frame) of a draft, read from the computer, with a way to take it off. */
function DraftMediaTile(props: { draftId: string; item: FriendsDraftMedia; busy: boolean; onRemove: () => void }) {
  const uri = signal<string | null>(null);
  const server = serverFor(currentServerId());
  if (server) {
    void draftMediaImage(server, props.draftId, props.item.poster?.file ?? props.item.file)
      .then((image) => uri.set(image.uri))
      .catch(() => {});
  }
  const label = props.item.kind === "video" ? "Video" : "Picture";
  return (
    <View testID={`draft-media-${props.item.file}`} class="size-24 overflow-hidden rounded-xl border border-border bg-secondary" accessibilityLabel={label}>
      {uri() ? (
        <Image src={uri()!} class="absolute inset-0" style={{ objectFit: "cover" }} accessibilityLabel={label} />
      ) : (
        <View class="flex-1 items-center justify-center"><Spinner size={16} /></View>
      )}
      {props.item.kind === "video" ? (
        <View class="absolute bottom-1 left-1 flex-row items-center gap-0.5 rounded-md bg-primary/70 px-1 py-0.5">
          <Icon name="play" size={10} tone="canvas" />
          <Text class="text-[11px] text-primary-foreground">{durationLabel(props.item.duration_ms ?? 0)}</Text>
        </View>
      ) : null}
      <Button
        accessibilityLabel="Remove from post"
        disabled={props.busy}
        onPress={props.onRemove}
        class="absolute right-1 top-1 size-6 items-center justify-center rounded-full bg-primary/70 active:opacity-70"
      >
        <Icon name="close" size={12} tone="canvas" />
      </Button>
    </View>
  );
}

/** The owner's edits of the drafts on screen, by draft id: a status refresh never puts Brain's text back. */
type Edits = Map<string, string>;

/**
 * One draft Brain wrote: edit it, see what the scrub says, then Post or Discard. It
 * reads its draft from the status by id, so a newer status (media taken off, another
 * draft gone) changes what it shows and keeps what the owner typed.
 */
function DraftCard(props: { id: string; nav: PageNav; edits: Edits; onFocus: (input: InputRef) => void }) {
  const find = () => friends().status?.drafts.find((d) => d.id === props.id);
  let last: FriendsDraft = untracked(find) ?? { id: props.id, text: "", created_at: new Date().toISOString() };
  const draft = () => (last = find() ?? last);
  const original = computed(() => draft().text);
  const text = signal(props.edits.get(props.id) ?? untracked(original));
  const problem = signal<string | null>(null);
  const busy = signal(false);
  const me = () => friends().status?.me;
  const length = () => text().trim().length;
  const ok = () => !problem() && length() > 0 && length() <= FRIENDS_POST_LIMIT;
  let input: InputRef | null = null;

  // The scrub runs on the computer as the owner edits, so a refusal shows before Post is pressed.
  effect(() => {
    const now = text().trim();
    const server = serverFor(currentServerId());
    if (!server || now === original().trim()) {
      untracked(() => problem.set(null));
      return;
    }
    let cancelled = false;
    const timer = setTimeout(() => {
      void checkDraftText(server, now).then((verdict) => !cancelled && problem.set(verdict));
    }, 500);
    cleanup(() => {
      cancelled = true;
      clearTimeout(timer);
    });
  });

  const post = async () => {
    const server = serverFor(currentServerId());
    if (!server || busy()) return;
    busy.set(true);
    try {
      // The files shown are what the owner reviewed; the computer posts the draft only if they match.
      const answer = await postDraft(server, props.id, text().trim(), (draft().media ?? []).map((item) => item.file));
      props.edits.delete(props.id);
      await refreshFriends();
      props.nav.pop();
      props.nav.push({ name: "friends-post", id: answer.post.id });
    } catch (reason) {
      problem.set(reason instanceof Error && reason.message ? reason.message : "Could not post");
    } finally {
      busy.set(false);
    }
  };

  const removeMedia = (index: number) => {
    const server = serverFor(currentServerId());
    if (!server || busy()) return;
    busy.set(true);
    removeDraftMedia(server, props.id, index)
      .then(() => refreshFriends())
      .catch((reason: unknown) => problem.set(reason instanceof Error && reason.message ? reason.message : "Could not remove that"))
      .finally(() => busy.set(false));
  };

  const discard = async () => {
    if (!serverFor(currentServerId()) || busy()) return;
    const answer = await AlertDialog.show({
      title: "Discard this draft?",
      actions: [
        { id: "keep", label: "Keep", role: "cancel" },
        { id: "go", label: "Discard", role: "destructive" },
      ],
    });
    const server = serverFor(currentServerId());
    if (answer.action !== "go" || !server) return;
    busy.set(true);
    try {
      await discardDraft(server, props.id);
      props.edits.delete(props.id);
      await refreshFriends();
    } catch {
      busy.set(false);
    }
  };

  // Taking a picture off changes the draft's media; the tiles left keep their pictures.
  const mediaKey = computed(() => (draft().media ?? []).map((item) => item.file).join("\n"));

  return (
    <View testID={`draft-${props.id}`} class="mt-4 gap-3 rounded-2xl border border-foreground bg-card p-4">
      <View class="flex-row items-center gap-3">
        {me() ? <Avatar author={me()!} size={40} /> : null}
        <View class="min-w-0 flex-1">
          <Text class="text-base font-semibold">{me()?.agent_name || "Brain"}</Text>
          <Text class="text-sm text-muted-foreground line-clamp-1">
            {`Draft${draft().source ? ` · from ${draft().source}` : ""} · ${relativeTime(Date.parse(draft().created_at), Date.now())}`}
          </Text>
        </View>
        <View class="rounded-full bg-seal px-2 py-0.5">
          <Text class="text-xs font-semibold text-primary-foreground">Needs you</Text>
        </View>
      </View>
      <Input
        testID={`draft-text-${props.id}`}
        ref={(node) => {
          input = node;
        }}
        accessibilityLabel="Draft text"
        multiline
        class="min-h-[110px] max-h-[550px] rounded-xl border border-border bg-background px-3 py-2.5 text-base"
        value={text()}
        maxLength={FRIENDS_POST_LIMIT + 50}
        onFocus={() => input && props.onFocus(input)}
        onChange={({ value }) => {
          text.set(value);
          props.edits.set(props.id, value);
        }}
      />
      {((key) =>
        untracked(() =>
          key ? (
            <View class="flex-row flex-wrap gap-2">
              {(draft().media ?? []).map((item, index) => (
                <DraftMediaTile draftId={props.id} item={item} busy={busy()} onRemove={() => removeMedia(index)} />
              ))}
            </View>
          ) : null,
        ))(mediaKey())}
      <View class="flex-row items-center gap-2">
        <Icon name={problem() ? "alert-circle" : "shield-check"} size={20} tone={problem() ? "failed" : "ready"} />
        <Text testID={`draft-check-${props.id}`} class={problem() ? "flex-1 text-sm text-destructive" : "flex-1 text-sm text-muted-foreground"}>
          {problem() ?? "Checked: no private names or keys"}
        </Text>
        <Text class={length() > FRIENDS_POST_LIMIT ? "text-sm text-destructive" : "text-sm text-muted-foreground"} style={{ fontVariant: ["tabular-nums"] }}>
          {`${length()}/${FRIENDS_POST_LIMIT}`}
        </Text>
      </View>
      <View class="flex-row gap-3">
        <Button testID={`draft-discard-${props.id}`} disabled={busy()} onPress={() => void discard()} class="flex-1 items-center rounded-full border border-border py-3 active:opacity-70 disabled:opacity-50">
          <Text class="text-base font-medium">Discard</Text>
        </Button>
        <Button
          testID={`draft-post-${props.id}`}
          disabled={!ok() || busy()}
          onPress={() => void post()}
          class="flex-[2] flex-row items-center justify-center gap-2 rounded-full bg-seal py-3 active:opacity-80 disabled:opacity-40"
        >
          {busy() ? <Spinner size={16} tone="canvas" /> : null}
          <Text class="text-base font-semibold text-primary-foreground">Post</Text>
        </Button>
      </View>
    </View>
  );
}

/** What Brain wrote for Friends, waiting for the owner to check. The drafts live on the computer. */
export function Drafts(props: { nav: PageNav; top: () => boolean }) {
  followFriends();
  const edits: Edits = new Map();
  followPage({ top: props.top, clear: () => edits.clear(), load: () => void refreshFriends() });
  // The cards are keyed on the drafts' ids: a newer status with the same drafts rebuilds none of them.
  const ids = computed(() => (friends().status?.drafts ?? []).map((draft) => draft.id).join("\n"));

  // The editor stays above the keyboard: the page ends where the keyboard begins, and
  // the draft being edited scrolls up into what is left.
  const keyboard = keyboardLift();
  let scroll: ScrollRef | null = null;
  let offset = 0;
  let editing: InputRef | null = null;
  const reveal = () => {
    const target = editing;
    if (!target || !scroll) return;
    void Promise.all([target.measure(), scroll.measure()]).then(([field, view]) => {
      if (!field || !view || !scroll) return;
      const bottom = field.pagePosition.y + field.bounds.size.height + 12;
      const visible = view.pagePosition.y + view.bounds.size.height;
      if (bottom > visible) scroll.scrollTo({ y: offset + bottom - visible, animated: true });
    });
  };
  effect(() => {
    if (keyboard.lift() > 0) untracked(reveal);
  });

  return (
    <FriendsPageFrame title="Drafts" testID="friends-drafts">
      <View class="flex-1" ref={keyboard.ref} onLayout={keyboard.onLayout}>
        <Scroll
          ref={(node) => {
            scroll = node;
          }}
          class="flex-1"
          contentClass="px-4 pb-10"
          keyboardShouldPersistTaps="handled"
          keyboardDismissMode="interactive"
          style={{ marginBottom: keyboard.lift() }}
          onScroll={(e) => {
            offset = e.offset.y;
          }}
        >
          {((key) =>
            untracked(() =>
              key ? (
                <View>
                  {key.split("\n").map((id) => (
                    <DraftCard
                      id={id}
                      nav={props.nav}
                      edits={edits}
                      onFocus={(input) => {
                        editing = input;
                        if (keyboard.lift() > 0) setTimeout(reveal, 0);
                      }}
                    />
                  ))}
                  <Text class="mt-3 text-center text-xs text-muted-foreground">Visible to everyone on Friends once posted.</Text>
                </View>
              ) : (
                <EmptyState icon="edit" title="No drafts waiting" detail="When Brain writes something for Friends, it waits here for you to check." />
              ),
            ))(ids())}
        </Scroll>
      </View>
    </FriendsPageFrame>
  );
}
