import { computed, signal, Text, View } from "nsh";
import { Button } from "nsh/base";
import { displayName, type FriendsAuthor, type FriendsPerson } from "../../../core/friends/api";
import { friendsCall } from "../../../core/friends/requests";
import { Icon } from "../../kit/Icon";
import { EmptyState, PillButton, Row, Section } from "../../kit/kit";
import type { PageNav } from "../routes";
import { AgentPill, Avatar, BLOCK_DETAIL, blockPerson, fail, FriendsPageFrame, PostList, report } from "./parts";
import { leaveWhenOnTop, toggleFollow } from "./Post";
import { currentServerId, followFriends, followPage, serverFor } from "./state";
import { showAlert } from "../../shell/alert";

async function unblock(id: string, from: string): Promise<FriendsPerson | null> {
  const server = serverFor(from);
  if (!server) return null;
  try {
    return await friendsCall<FriendsPerson>(server, "DELETE", `/people/${id}/block`);
  } catch (reason) {
    fail(reason);
    return null;
  }
}

/** A person: who they are, follow or block, and their posts. Yours links to your bookmarks and blocks. */
export function Person(props: { id: string; nav: PageNav; top: () => boolean }) {
  followFriends();
  const person = signal<FriendsPerson | null>(null);
  const from = signal(currentServerId());
  const leave = leaveWhenOnTop(props.nav, props.top);
  let generation = 0;

  const load = async () => {
    const id = currentServerId();
    const server = serverFor(id);
    if (!server) return;
    const mine = ++generation;
    try {
      const next = await friendsCall<FriendsPerson>(server, "GET", `/people/${props.id}`);
      if (mine !== generation || currentServerId() !== id) return;
      from.set(id);
      person.set(next);
    } catch (reason) {
      if (mine !== generation || currentServerId() !== id) return;
      fail(reason);
      leave();
    }
  };
  followPage({
    top: props.top,
    clear: () => {
      generation++;
      person.set(null);
      from.set(currentServerId());
    },
    load: () => void load(),
  });

  /** Sets the person an action answered with, unless the page now shows another computer's. */
  const answered = (shownFrom: string) => (next: FriendsPerson | null) => {
    if (next && from() === shownFrom && currentServerId() === shownFrom) person.set(next);
  };

  const menu = async () => {
    const p = person();
    if (!p) return;
    const shownFrom = from();
    const answer = await showAlert({
      presentation: "actionSheet",
      description: p.blocked ? undefined : `Block @${p.handle}: ${BLOCK_DETAIL.toLowerCase()}`,
      actions: [
        { id: "report", label: "Report" },
        p.blocked ? { id: "unblock", label: "Unblock" } : { id: "block", label: `Block @${p.handle}`, role: "destructive" },
        { id: "cancel", label: "Cancel", role: "cancel" },
      ],
    });
    if (answer.action === "report") await report("person", p.id, shownFrom);
    else if (answer.action === "unblock") answered(shownFrom)(await unblock(p.id, shownFrom));
    else if (answer.action === "block" && (await blockPerson(p, shownFrom))) leave();
  };

  // A follow or an unblock changes the person; the list of their posts below stays.
  const loaded = computed(() => person() !== null);
  const header = () => (
    <View>
      <View class="items-center gap-1 px-4 pt-5">
        <Avatar author={person()!} size={76} />
        <View class="mt-2 flex-row items-center gap-1.5">
          <Text testID="friends-person-name" class="text-2xl font-semibold" role="header">{displayName(person()!)}</Text>
          {person()!.agent_name ? <AgentPill /> : null}
        </View>
        <Text class="text-sub text-muted-foreground">{`by @${person()!.handle}`}</Text>
        {person()!.bio ? <Text class="mt-1 text-center text-body">{person()!.bio}</Text> : null}
        <Text class="mt-1 text-sub text-muted-foreground" style={{ fontVariant: ["tabular-nums"] }}>
          {`${person()!.posts} posts  ${person()!.following} following  ${person()!.followers} followers`}
        </Text>
        {!person()!.mine ? (
          <View class="flex-row items-center gap-2">
            {person()!.blocked ? (
              <PillButton testID="friends-unblock" title="Unblock" onPress={() => void unblock(person()!.id, from()).then(answered(from()))} />
            ) : (
              <PillButton
                testID="friends-follow"
                title={person()!.followed ? "Following" : "Follow"}
                filled={!person()!.followed}
                onPress={() => void toggleFollow(person()!, from()).then(answered(from()))}
              />
            )}
            <Button testID="friends-person-more" accessibilityLabel="More" onPress={() => void menu()} class="mt-2 size-9 items-center justify-center rounded-full bg-secondary active:opacity-70">
              <Icon name="more-horizontal" size={18} />
            </Button>
          </View>
        ) : null}
      </View>
      {person()!.mine ? (
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

  return (
    <FriendsPageFrame title="Profile" testID="friends-person">
      {loaded() ? (
        <PostList
          path={`/people/${props.id}/posts`}
          nav={props.nav}
          top={props.top}
          header={header()}
          emptyTitle="No posts yet"
          emptyDetail={person()?.mine ? "What Brain posts for you shows up here." : undefined}
        />
      ) : null}
    </FriendsPageFrame>
  );
}

/** The people you blocked and who blocked you, each with Unblock. */
export function Blocked(props: { top: () => boolean }) {
  followFriends();
  const people = signal<FriendsAuthor[] | null>(null);
  const from = signal(currentServerId());
  let generation = 0;
  const load = async () => {
    const id = currentServerId();
    const server = serverFor(id);
    if (!server) return;
    const mine = ++generation;
    const live = () => mine === generation && currentServerId() === id;
    try {
      const answer = await friendsCall<{ people: FriendsAuthor[] }>(server, "GET", "/blocks");
      if (!live()) return;
      from.set(id);
      people.set(answer.people ?? []);
    } catch {
      if (live()) people.set([]);
    }
  };
  followPage({
    top: props.top,
    clear: () => {
      generation++;
      people.set(null);
      from.set(currentServerId());
    },
    load: () => void load(),
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
                trailing={<PillButton testID={`unblock-${p.id}`} title="Unblock" onPress={() => void unblock(p.id, from()).then(() => load())} />}
              />
            ))}
          </Section>
        </View>
      )}
    </FriendsPageFrame>
  );
}
