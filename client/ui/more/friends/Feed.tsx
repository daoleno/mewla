import { computed, platform, signal, Text, View } from "nsh";
import { Button } from "nsh/base";
import { Icon } from "../../kit/Icon";
import { EmptyState } from "../../kit/kit";
import { Segmented } from "../../kit/Segmented";
import { createTabPager, TabPages } from "../../kit/TabPager";
import type { PageNav } from "../routes";
import { Avatar, FriendsPageFrame, PostList } from "./parts";
import { draftCount, followFriends, followPage, friends, friendsEnabled, refreshFriends, unread } from "./state";

type Scope = "all" | "following";

/** The draft banner: what Brain wrote for you to check before it is posted. */
function DraftBanner(props: { nav: PageNav }) {
  const one = () => draftCount() === 1;
  return (<><If when={draftCount() > 0}><Button
      testID="friends-drafts-banner"
      accessibilityLabel={`${draftCount()} ${one() ? "draft" : "drafts"} waiting`}
      onPress={() => props.nav.push({ name: "friends-drafts" })}
      class="mx-4 mt-3 min-h-[52px] flex-row items-center gap-3 rounded-full bg-card pl-2 pr-4 active:bg-accent"
    >
      {/* Waiting for you: the one place Friends uses the seal. */}
      <View class="size-9 items-center justify-center rounded-full bg-seal">
        <Text class="text-sub font-semibold text-seal-foreground">{String(draftCount())}</Text>
      </View>
      <Text class="flex-1 text-body font-semibold">{one() ? "Draft waiting" : "Drafts waiting"}</Text>
      <Icon name="chevron-right" size={16} tone="faint" />
    </Button></If></>);
}

/** Friends: everyone's posts or only the people you follow, newest first. */
export function Feed(props: { nav: PageNav; top: () => boolean }) {
  followFriends();
  // Back on the feed (a post deleted, someone blocked, a draft posted): the drafts and the bell read again.
  followPage({ top: props.top, clear: () => {}, load: () => void refreshFriends() });
  const scope = signal<Scope>("all");
  // All and Following are lists side by side that follow the finger (kit/TabPager.tsx); a list counts as shown
  // (it reads again, its videos play) only while it is the one in view.
  const pager = createTabPager({ keys: ["all", "following"] as const, value: scope, onChange: (next) => scope.set(next) });
  const shown = (key: Scope) => () => props.top() && scope() === key;
  const me = () => friends().status?.me;
  // The list stays mounted while the status changes; only turning off replaces it.
  const off = computed(() => friends().status !== null && !friendsEnabled());
  const header = () => (
    <View class="flex-row items-center gap-1">
      <Button
        testID="friends-bell"
        accessibilityLabel={unread() > 0 ? `Notifications, ${unread()} new` : "Notifications"}
        onPress={() => props.nav.push({ name: "friends-notifications" })}
        class="size-11 items-center justify-center rounded-full active:bg-accent"
      >
        <Icon name="bell" tone="ink" />
        <If when={unread() > 0}><View class="absolute right-2 top-2 size-2.5 rounded-full bg-seal" /></If>
      </Button>
      <If when={me()}><Button testID="friends-me" accessibilityLabel="Your profile" onPress={() => props.nav.push({ name: "friends-person", id: me()!.id })} class="active:opacity-70">
          <Avatar author={me()!} size={32} />
        </Button></If>
    </View>
  );
  return (
    <FriendsPageFrame title="Friends" testID="friends-feed" headerRight={friendsEnabled() ? header() : undefined}>
      <If when={off()} else={<View class="flex-1">
          <View class="px-4 pt-2">
            <Segmented
              testID="friends-scope"
              accessibilityLabel="Posts"
              options={[{ key: "all", label: "All" }, { key: "following", label: "Following" }]}
              value={scope()}
              onChange={(next) => scope.set(next)}
              pager={pager}
            />
          </View>
          <DraftBanner nav={props.nav} />
          <TabPages fill testID="friends-pages" pager={pager} pages={{
            all: () => <PostList testID="friends-list" path="/feed" nav={props.nav} top={shown("all")} emptyTitle="Nothing here yet" />,
            following: () => <PostList testID="friends-list-following" path="/feed?scope=following" nav={props.nav} top={shown("following")} emptyTitle="No one followed yet" />,
          }} />
        </View>}><EmptyState testID="friends-off" icon="people" title="Friends is off" action="Open Labs" onAction={() => props.nav.push(platform.os === "web" ? { name: "settings" } : { name: "settings-friends" })} /></If>
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
