import { effect, lifecycle, signal, Text, untracked, View } from "nsh";
import { AlertDialog, Button } from "nsh/base";
import { displayName, type FriendsAuthor, type FriendsPerson } from "../../../core/friends/api";
import { friendsCall } from "../../../core/friends/requests";
import { Icon } from "../kit/Icon";
import { EmptyState, PillButton, Row, Section } from "../kit/kit";
import type { PageNav } from "../routes";
import { AgentPill, Avatar, blockPerson, fail, FriendsPageFrame, PostList, report } from "./parts";
import { toggleFollow } from "./Post";
import { followFriends, friendsServer } from "./state";

async function unblock(id: string): Promise<FriendsPerson | null> {
  const server = friendsServer();
  if (!server) return null;
  try {
    return await friendsCall<FriendsPerson>(server, "DELETE", `/people/${id}/block`);
  } catch (reason) {
    fail(reason);
    return null;
  }
}

/** A person: who they are, follow or block, and their posts. Yours links to your bookmarks and blocks. */
export function Person(props: { id: string; nav: PageNav }) {
  followFriends();
  const person = signal<FriendsPerson | null>(null);

  const load = async () => {
    const server = friendsServer();
    if (!server) return;
    try {
      person.set(await friendsCall<FriendsPerson>(server, "GET", `/people/${props.id}`));
    } catch (reason) {
      fail(reason);
      props.nav.pop();
    }
  };
  effect(() => {
    if (lifecycle() === "active") untracked(() => void load());
  });

  const menu = async () => {
    const p = person();
    if (!p) return;
    const answer = await AlertDialog.show({
      presentation: "actionSheet",
      actions: [
        { id: "report", label: "Report" },
        p.blocked ? { id: "unblock", label: "Unblock" } : { id: "block", label: `Block @${p.handle}`, role: "destructive" },
        { id: "cancel", label: "Cancel", role: "cancel" },
      ],
    });
    if (answer.action === "report") await report("person", p.id);
    else if (answer.action === "unblock") {
      const next = await unblock(p.id);
      if (next) person.set(next);
    } else if (answer.action === "block" && (await blockPerson(p))) props.nav.pop();
  };

  const header = () => {
    const p = person()!;
    return (
      <View>
        <View class="items-center gap-1 px-4 pt-5">
          <Avatar author={p} size={76} />
          <View class="mt-2 flex-row items-center gap-1.5">
            <Text testID="friends-person-name" class="text-2xl font-semibold" role="header">{displayName(p)}</Text>
            {p.agent_name ? <AgentPill /> : null}
          </View>
          <Text class="text-sm text-muted-foreground">{`by @${p.handle}`}</Text>
          {p.bio ? <Text class="mt-1 text-center text-base">{p.bio}</Text> : null}
          <Text class="mt-1 text-sm text-muted-foreground" style={{ fontVariant: ["tabular-nums"] }}>
            {`${p.posts} posts  ${p.following} following  ${p.followers} followers`}
          </Text>
          {!p.mine ? (
            <View class="flex-row items-center gap-2">
              {p.blocked ? (
                <PillButton testID="friends-unblock" title="Unblock" onPress={() => void unblock(p.id).then((next) => next && person.set(next))} />
              ) : (
                <PillButton
                  testID="friends-follow"
                  title={p.followed ? "Following" : "Follow"}
                  filled={!p.followed}
                  onPress={() => void toggleFollow(p).then((next) => next && person.set(next))}
                />
              )}
              <Button testID="friends-person-more" accessibilityLabel="More" onPress={() => void menu()} class="mt-2 size-9 items-center justify-center rounded-full bg-secondary active:opacity-70">
                <Icon name="more-horizontal" size={18} />
              </Button>
            </View>
          ) : null}
        </View>
        {p.mine ? (
          <View class="px-4">
            <Section>
              <Row testID="friends-open-bookmarks" icon="bookmark" title="Bookmarks" chevron onPress={() => props.nav.push({ name: "friends-bookmarks" })} />
              <Row testID="friends-open-blocked" icon="person-remove" title="Blocked people" chevron onPress={() => props.nav.push({ name: "friends-blocked" })} />
            </Section>
          </View>
        ) : null}
        <View class="h-4 border-b border-border" />
      </View>
    );
  };

  return (
    <FriendsPageFrame title="Profile" testID="friends-person">
      {person() ? (
        <PostList
          path={`/people/${props.id}/posts`}
          nav={props.nav}
          header={header()}
          emptyTitle="No posts yet"
          emptyDetail={person()!.mine ? "What Brain posts for you shows up here." : undefined}
        />
      ) : null}
    </FriendsPageFrame>
  );
}

/** The people you blocked and who blocked you, each with Unblock. */
export function Blocked() {
  followFriends();
  const people = signal<FriendsAuthor[] | null>(null);
  const load = async () => {
    const server = friendsServer();
    if (!server) return;
    try {
      people.set((await friendsCall<{ people: FriendsAuthor[] }>(server, "GET", "/blocks")).people ?? []);
    } catch {
      people.set([]);
    }
  };
  effect(() => {
    if (lifecycle() === "active") untracked(() => void load());
  });
  return (
    <FriendsPageFrame title="Blocked people" testID="friends-blocked">
      {people() === null ? (
        <EmptyState busy />
      ) : people()!.length === 0 ? (
        <EmptyState icon="person-remove" title="No one blocked" detail="People you block, and who block you, are hidden from each other." />
      ) : (
        <View class="px-4">
          <Section>
            {people()!.map((p) => (
              <Row
                leading={<Avatar author={p} size={36} />}
                title={displayName(p)}
                subtitle={`by @${p.handle}`}
                trailing={<PillButton testID={`unblock-${p.id}`} title="Unblock" onPress={() => void unblock(p.id).then(() => load())} />}
              />
            ))}
          </Section>
        </View>
      )}
    </FriendsPageFrame>
  );
}
