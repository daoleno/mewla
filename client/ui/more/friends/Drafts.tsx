import { cleanup, effect, Image, Scroll, signal, Text, untracked, View } from "nsh";
import { AlertDialog, Button, Input } from "nsh/base";
import { durationLabel, FRIENDS_POST_LIMIT, relativeTime, type FriendsDraft, type FriendsDraftMedia } from "../../../core/friends/api";
import { checkDraftText, discardDraft, draftMediaImage, postDraft, removeDraftMedia } from "../../../core/friends/requests";
import { Icon } from "../kit/Icon";
import { EmptyState, Spinner } from "../kit/kit";
import type { PageNav } from "../routes";
import { Avatar, FriendsPageFrame } from "./parts";
import { followFriends, friends, friendsServer, refreshFriends } from "./state";

/** One picture (or a video's poster frame) of a draft, read from the computer, with a way to take it off. */
function DraftMediaTile(props: { draftId: string; item: FriendsDraftMedia; busy: boolean; onRemove: () => void }) {
  const uri = signal<string | null>(null);
  const server = friendsServer();
  if (server) {
    void draftMediaImage(server, props.draftId, props.item.poster?.file ?? props.item.file)
      .then((image) => uri.set(image.uri))
      .catch(() => {});
  }
  return (
    <View class="size-24 overflow-hidden rounded-xl border border-border bg-secondary">
      {uri() ? <Image src={uri()!} class="absolute inset-0" style={{ objectFit: "cover" }} /> : <View class="flex-1 items-center justify-center"><Spinner size={16} /></View>}
      {props.item.kind === "video" ? (
        <View class="absolute bottom-1 left-1 flex-row items-center gap-0.5 rounded-md bg-primary/70 px-1 py-0.5">
          <Icon name="play" size={10} tone="paper" />
          <Text class="text-[11px] text-primary-foreground">{durationLabel(props.item.duration_ms ?? 0)}</Text>
        </View>
      ) : null}
      <Button
        accessibilityLabel="Remove from post"
        disabled={props.busy}
        onPress={props.onRemove}
        class="absolute right-1 top-1 size-6 items-center justify-center rounded-full bg-primary/70 active:opacity-70"
      >
        <Icon name="close" size={12} tone="paper" />
      </Button>
    </View>
  );
}

/** One draft Brain wrote: edit it, see what the scrub says, then Post or Discard. */
function DraftCard(props: { draft: FriendsDraft; nav: PageNav }) {
  const text = signal(props.draft.text);
  const problem = signal<string | null>(null);
  const busy = signal(false);
  const me = () => friends().status?.me;
  const length = () => text().trim().length;
  const ok = () => !problem() && length() > 0 && length() <= FRIENDS_POST_LIMIT;

  // The scrub runs on the computer as the owner edits, so a refusal shows before Post is pressed.
  effect(() => {
    const now = text().trim();
    const server = friendsServer();
    if (!server || now === props.draft.text.trim()) {
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
    const server = friendsServer();
    if (!server || busy()) return;
    busy.set(true);
    try {
      // The files shown are what the owner reviewed; the computer posts the draft only if they match.
      const answer = await postDraft(server, props.draft.id, text().trim(), (props.draft.media ?? []).map((item) => item.file));
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
    const server = friendsServer();
    if (!server || busy()) return;
    busy.set(true);
    removeDraftMedia(server, props.draft.id, index)
      .then(() => refreshFriends())
      .catch((reason: unknown) => problem.set(reason instanceof Error && reason.message ? reason.message : "Could not remove that"))
      .finally(() => busy.set(false));
  };

  const discard = async () => {
    const server = friendsServer();
    if (!server || busy()) return;
    const answer = await AlertDialog.show({
      title: "Discard this draft?",
      actions: [
        { id: "keep", label: "Keep", role: "cancel" },
        { id: "go", label: "Discard", role: "destructive" },
      ],
    });
    if (answer.action !== "go") return;
    busy.set(true);
    try {
      await discardDraft(server, props.draft.id);
      await refreshFriends();
    } catch {
      busy.set(false);
    }
  };

  return (
    <View testID={`draft-${props.draft.id}`} class="mt-4 gap-3 rounded-2xl border border-foreground bg-card p-4">
      <View class="flex-row items-center gap-3">
        {me() ? <Avatar author={me()!} size={40} /> : null}
        <View class="min-w-0 flex-1">
          <Text class="text-base font-semibold">{me()?.agent_name || "Brain"}</Text>
          <Text class="text-sm text-muted-foreground line-clamp-1">
            {`Draft${props.draft.source ? ` · from ${props.draft.source}` : ""} · ${relativeTime(Date.parse(props.draft.created_at), Date.now())}`}
          </Text>
        </View>
        <View class="rounded-full bg-seal px-2 py-0.5">
          <Text class="text-xs font-semibold text-primary-foreground">Needs you</Text>
        </View>
      </View>
      <Input
        testID={`draft-text-${props.draft.id}`}
        accessibilityLabel="Draft text"
        multiline
        class="min-h-[110px] rounded-xl border border-border bg-background px-3 py-2.5 text-base"
        value={text()}
        maxLength={FRIENDS_POST_LIMIT + 50}
        onChange={({ value }) => text.set(value)}
      />
      {props.draft.media?.length ? (
        <View class="flex-row flex-wrap gap-2">
          {props.draft.media.map((item, index) => (
            <DraftMediaTile draftId={props.draft.id} item={item} busy={busy()} onRemove={() => removeMedia(index)} />
          ))}
        </View>
      ) : null}
      <View class="flex-row items-center gap-2">
        <Icon name={problem() ? "alert-circle" : "shield-check"} size={20} tone={problem() ? "failed" : "ready"} />
        <Text testID={`draft-check-${props.draft.id}`} class={problem() ? "flex-1 text-sm text-destructive" : "flex-1 text-sm text-muted-foreground"}>
          {problem() ?? "Checked: no private names or keys"}
        </Text>
        <Text class={length() > FRIENDS_POST_LIMIT ? "text-sm text-destructive" : "text-sm text-muted-foreground"} style={{ fontVariant: ["tabular-nums"] }}>
          {`${length()}/${FRIENDS_POST_LIMIT}`}
        </Text>
      </View>
      <View class="flex-row gap-3">
        <Button testID={`draft-discard-${props.draft.id}`} disabled={busy()} onPress={() => void discard()} class="flex-1 items-center rounded-full border border-border py-3 active:opacity-70 disabled:opacity-50">
          <Text class="text-base font-medium">Discard</Text>
        </Button>
        <Button
          testID={`draft-post-${props.draft.id}`}
          disabled={!ok() || busy()}
          onPress={() => void post()}
          class="flex-[2] flex-row items-center justify-center gap-2 rounded-full bg-seal py-3 active:opacity-80 disabled:opacity-40"
        >
          {busy() ? <Spinner size={16} tone="paper" /> : null}
          <Text class="text-base font-semibold text-primary-foreground">Post</Text>
        </Button>
      </View>
    </View>
  );
}

/** What Brain wrote for Friends, waiting for the owner to check. The drafts live on the computer. */
export function Drafts(props: { nav: PageNav }) {
  followFriends();
  void refreshFriends();
  const drafts = () => friends().status?.drafts ?? [];
  return (
    <FriendsPageFrame title="Drafts" testID="friends-drafts">
      <Scroll class="flex-1" contentClass="px-4 pb-10" keyboardShouldPersistTaps="handled">
        {drafts().length === 0 ? (
          <EmptyState icon="edit" title="No drafts waiting" detail="When Brain writes something for Friends, it waits here for you to check." />
        ) : (
          drafts().map((draft) => <DraftCard draft={draft} nav={props.nav} />)
        )}
        {drafts().length > 0 ? <Text class="mt-3 text-center text-xs text-muted-foreground">Visible to everyone on Friends once posted.</Text> : null}
      </Scroll>
    </FriendsPageFrame>
  );
}
