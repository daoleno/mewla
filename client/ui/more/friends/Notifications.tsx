import { effect, lifecycle, Scroll, signal, Text, untracked, View } from "nsh";
import { Button } from "nsh/base";
import { notificationRows, relativeTime, type FriendsNotification, type NotificationRow } from "../../../core/friends/api";
import { friendsCall } from "../../../core/friends/requests";
import { Icon, type IconName } from "../kit/Icon";
import { EmptyState } from "../kit/kit";
import type { PageNav } from "../routes";
import { FriendsPageFrame } from "./parts";
import { draftCount, followFriends, friends, friendsServer, refreshFriends } from "./state";

const ICONS: Record<NotificationRow["kind"], IconName> = { comment: "chat", like: "heart", follow: "people" };

function Line(props: { icon: IconName; text: string; detail?: string; at?: string; unread?: boolean; testID?: string; onPress: () => void }) {
  return (
    <Button testID={props.testID} onPress={props.onPress} class={`flex-row items-start gap-3 border-b border-border px-4 py-3 active:bg-accent ${props.unread ? "bg-card" : ""}`}>
      <View class="pt-0.5"><Icon name={props.icon} /></View>
      <View class="min-w-0 flex-1">
        <Text class="text-base">{props.text}</Text>
        {props.detail ? <Text class="mt-0.5 text-sm text-muted-foreground line-clamp-2">{props.detail}</Text> : null}
        {props.at ? <Text class="mt-0.5 text-xs text-muted-foreground">{props.at}</Text> : null}
      </View>
      {props.unread ? <View class="mt-2 size-2 rounded-full bg-seal" accessibilityLabel="New" /> : null}
    </Button>
  );
}

/** Comments, likes and follows, newest first; opening the page marks them read. */
export function Notifications(props: { nav: PageNav }) {
  followFriends();
  const rows = signal<NotificationRow[] | null>(null);
  const load = async () => {
    const server = friendsServer();
    if (!server) return;
    try {
      const answer = await friendsCall<{ notifications: FriendsNotification[] }>(server, "GET", "/notifications");
      rows.set(notificationRows(answer.notifications ?? []));
    } catch {
      rows.set([]);
      return;
    }
    // Read once shown: the rows keep their unread mark from before, the bell's dot clears.
    await friendsCall(server, "POST", "/notifications/read").catch(() => {});
    await refreshFriends();
  };
  effect(() => {
    if (lifecycle() === "active") untracked(() => void load());
  });
  const agent = () => friends().status?.me?.agent_name || "Brain";
  return (
    <FriendsPageFrame title="Notifications" testID="friends-notifications">
      {rows() === null ? (
        <EmptyState busy />
      ) : (
        <Scroll class="flex-1">
          {draftCount() > 0 ? (
            <Line
              testID="friends-notification-drafts"
              icon="edit"
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
