import { computed, platform, signal, Text, View } from "nsh";
import { Button } from "nsh/base";
import { Icon } from "../../kit/Icon";
import { EmptyState } from "../../kit/kit";
import { Segmented } from "../../kit/Segmented";
import type { PageNav } from "../routes";
import { Avatar, FriendsPageFrame, PostList } from "./parts";
import { draftCount, followFriends, followPage, friends, friendsEnabled, refreshFriends, unread } from "./state";

type Scope = "all" | "following";

/** The draft banner: what Brain wrote for you to check before it is posted. */
function DraftBanner(props: { nav: PageNav }) {
  const one = () => draftCount() === 1;
  return (<>{draftCount() > 0 ? (
    <Button
      testID="friends-drafts-banner"
      accessibilityLabel={`${draftCount()} ${one() ? "draft" : "drafts"} waiting`}
      onPress={() => props.nav.push({ name: "friends-drafts" })}
      class="mx-4 mt-3 flex-row items-center gap-3 rounded-2xl border border-border bg-card px-4 py-3 active:bg-accent"
    >
      <View class="size-8 items-center justify-center rounded-full bg-seal">
        <Text class="text-sm font-semibold text-primary-foreground">{String(draftCount())}</Text>
      </View>
      <View class="flex-1">
        <Text class="text-base font-medium">{one() ? "Draft waiting" : "Drafts waiting"}</Text>
        <Text class="text-sm text-muted-foreground">{one() ? "Brain wrote this for you to check" : "Brain wrote these for you to check"}</Text>
      </View>
      <Icon name="chevron-right" size={16} tone="faint" />
    </Button>
  ) : null}</>);
}

/** Friends: everyone's posts or only the people you follow, newest first. */
export function Feed(props: { nav: PageNav; top: () => boolean }) {
  followFriends();
  // Back on the feed (a post deleted, someone blocked, a draft posted): the drafts and the bell read again.
  followPage({ top: props.top, clear: () => {}, load: () => void refreshFriends() });
  const scope = signal<Scope>("all");
  const me = () => friends().status?.me;
  // The list stays mounted while the status changes; only turning off replaces it.
  const off = computed(() => friends().status !== null && !friendsEnabled());
  const header = () => (
    <View class="flex-row items-center gap-1">
      <Button
        testID="friends-bell"
        accessibilityLabel={unread() > 0 ? `Notifications, ${unread()} new` : "Notifications"}
        onPress={() => props.nav.push({ name: "friends-notifications" })}
        class="size-10 items-center justify-center active:opacity-60"
      >
        <Icon name="bell" tone="ink" />
        {unread() > 0 ? <View class="absolute right-2 top-2 size-2.5 rounded-full bg-seal" /> : null}
      </Button>
      {me() ? (
        <Button testID="friends-me" accessibilityLabel="Your profile" onPress={() => props.nav.push({ name: "friends-person", id: me()!.id })} class="active:opacity-70">
          <Avatar author={me()!} size={32} />
        </Button>
      ) : null}
    </View>
  );
  return (
    <FriendsPageFrame title="Friends" testID="friends-feed" headerRight={friendsEnabled() ? header() : undefined}>
      {off() ? (
        // A browser offers no Friends switch (as v0.3.1): Open Labs opens Settings there.
        <EmptyState testID="friends-off" icon="people" title="Friends is off" action="Open Labs" onAction={() => props.nav.push(platform.os === "web" ? { name: "settings" } : { name: "settings-friends" })} />
      ) : (
        <View class="flex-1">
          <View class="px-4 pt-2">
            <Segmented
              testID="friends-scope"
              accessibilityLabel="Posts"
              options={[{ key: "all", label: "All" }, { key: "following", label: "Following" }]}
              value={scope()}
              onChange={(next) => scope.set(next)}
            />
          </View>
          <DraftBanner nav={props.nav} />
          {scope() === "all" ? (
            <PostList testID="friends-list" path="/feed" nav={props.nav} top={props.top} emptyTitle="Nothing here yet" />
          ) : (
            <PostList testID="friends-list" path="/feed?scope=following" nav={props.nav} top={props.top} emptyTitle="No one followed yet" />
          )}
        </View>
      )}
    </FriendsPageFrame>
  );
}

export function Bookmarks(props: { nav: PageNav; top: () => boolean }) {
  followFriends();
  return (
    <FriendsPageFrame title="Bookmarks" testID="friends-bookmarks">
      <PostList path="/bookmarks" nav={props.nav} top={props.top} emptyTitle="No bookmarks" emptyDetail="Tap the bookmark on a post to keep it here." emptyIcon="bookmark" />
    </FriendsPageFrame>
  );
}
