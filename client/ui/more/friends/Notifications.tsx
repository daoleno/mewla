import { Scroll, signal, Text, View } from "nsh";
import { Button } from "nsh/base";
import { notificationRows, relativeTime, type FriendsNotification, type NotificationRow } from "../../../core/friends/api";
import { friendsCall } from "../../../core/friends/requests";
import { Icon, type IconName } from "../../kit/Icon";
import { EmptyState } from "../../kit/kit";
import type { PageNav } from "../routes";
import { FriendsPageFrame } from "./parts";
import { currentServerId, draftCount, followFriends, followPage, friends, refreshFriends, serverFor } from "./state";

const ICONS: Record<NotificationRow["kind"], IconName> = { comment: "chat", like: "heart", follow: "people" };

function Line(props: { icon: IconName; text: string; detail?: string; at?: string; unread?: boolean; chevron?: boolean; testID?: string; onPress: () => void }) {
  return (
    <Button testID={props.testID} onPress={props.onPress} class={`flex-row items-start gap-3 border-b border-border px-4 py-3 active:bg-accent ${props.unread ? "bg-card" : ""}`}>
      <View class="pt-0.5"><Icon name={props.icon} /></View>
      <View class="min-w-0 flex-1">
        <Text class="text-body">{props.text}</Text>
        {props.detail ? <Text class="mt-0.5 text-sub text-muted-foreground line-clamp-2">{props.detail}</Text> : null}
        {props.at ? <Text class="mt-0.5 text-caption text-muted-foreground">{props.at}</Text> : null}
      </View>
      {props.unread ? <View class="mt-2 size-2 rounded-full bg-seal" accessibilityLabel="New" /> : null}
      {props.chevron ? <View class="self-center"><Icon name="chevron-right" size={16} tone="faint" /></View> : null}
    </Button>
  );
}

/** Comments, likes and follows, newest first; opening the page marks them read. */
export function Notifications(props: { nav: PageNav; top: () => boolean }) {
  followFriends();
  const rows = signal<NotificationRow[] | null>(null);
  let generation = 0;
  const load = async () => {
    const id = currentServerId();
    const server = serverFor(id);
    if (!server) return;
    const mine = ++generation;
    const live = () => mine === generation && currentServerId() === id;
    try {
      const answer = await friendsCall<{ notifications: FriendsNotification[] }>(server, "GET", "/notifications");
      if (!live()) return;
      rows.set(notificationRows(answer.notifications ?? []));
    } catch {
      if (live()) rows.set([]);
      return;
    }
    // Read once shown: the rows keep their unread mark from before, the bell's dot clears.
    await friendsCall(server, "POST", "/notifications/read").catch(() => {});
    if (live()) await refreshFriends();
  };
  // Back on the page (from a post or a profile), newer ones show and are marked read too.
  followPage({
    top: props.top,
    clear: () => {
      generation++;
      rows.set(null);
    },
    load: () => void load(),
  });
  const agent = () => friends().status?.me?.agent_name || "Brain";
  return (
    <FriendsPageFrame title="Notifications" testID="friends-notifications">
      {rows() === null ? (
        <EmptyState busy />
      ) : (
        <Scroll keyboardShouldPersistTaps="handled" class="flex-1">
          {draftCount() > 0 ? (
            <Line
              testID="friends-notification-drafts"
              icon="edit"
              chevron
              text={draftCount() === 1 ? `${agent()} has a draft waiting for you` : `${agent()} has ${draftCount()} drafts waiting for you`}
              onPress={() => props.nav.push({ name: "friends-drafts" })}
            />
          ) : null}
          {rows()!.length === 0 && draftCount() === 0 ? <EmptyState icon="bell" title="Nothing yet" detail="Comments, likes and follows show up here." /> : null}
          {rows()!.map((row) => (
            <Line
              testID={`notification-${row.key}`}
              icon={ICONS[row.kind]}
              text={row.text}
              detail={row.detail}
              at={relativeTime(row.at, Date.now())}
              unread={row.unread}
              onPress={() => props.nav.push(row.postId ? { name: "friends-post", id: row.postId } : { name: "friends-person", id: row.actorId })}
            />
          ))}
        </Scroll>
      )}
    </FriendsPageFrame>
  );
}
