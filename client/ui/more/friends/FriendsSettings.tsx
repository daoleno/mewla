import { signal, Text, View } from "nsh";
import { Page, PillButton, Row, Section, Spinner, SwitchRow } from "../../kit/kit";
import { tile } from "../../kit/RowTile";
import type { PageNav } from "../routes";
import { Avatar } from "./parts";
import {
  available,
  cancelSignIn,
  changeAutoMedia,
  changeAutoPost,
  deleteAccount,
  followFriends,
  friends,
  friendsEnabled,
  openSignInPage,
  phase,
  turnOff,
  turnOn,
} from "./state";
import { showAlert } from "../../shell/alert";

/** Asks with the points that matter; true when the person chose `confirm`. */
async function ask(title: string, points: string[], confirm: string, cancel: string, destructive = false): Promise<boolean> {
  const answer = await showAlert({
    title,
    description: points.join("\n\n"),
    actions: [
      { id: "cancel", label: cancel, role: "cancel" },
      { id: "go", label: confirm, role: destructive ? "destructive" : "default" },
    ],
  });
  return answer.action === "go";
}

/** Settings → Labs → Friends: the switch, what it may post without asking, the account, and the way in. */
export function FriendsSettings(props: { nav: PageNav }) {
  followFriends();
  const deleting = signal(false);
  const me = () => friends().status?.me;
  const subtitle = () => {
    const m = friends();
    if (m.error) return m.error;
    if (phase() === "signing_in") return "Sign in to Mewla to continue";
    if (phase() === "working") return "Turning on…";
    if (!available() && !friendsEnabled()) return "Connect a computer first";
    return undefined;
  };

  const toggle = (on: boolean) => {
    if (on) void turnOn();
    else if (phase() === "signing_in") cancelSignIn();
    else void turnOff();
  };

  const autoPost = async (on: boolean) => {
    if (!on) return void changeAutoPost(false);
    const ok = await ask(
      "Post without asking?",
      [`Brain posts straight to Friends, up to ${me()?.daily_cap ?? 3} a day`, "You can delete any post afterwards", "Names, addresses and keys are filtered, but nobody reads it first"],
      "Turn on",
      "Keep asking me",
    );
    if (ok) await changeAutoPost(true);
  };

  const autoMedia = async (on: boolean) => {
    if (!on) return void changeAutoMedia(false);
    const ok = await ask(
      "Post pictures and video without asking?",
      ["Brain may attach pictures or a video it made to a post", "You can delete any post afterwards", "File details are stripped, but nobody looks at what the picture shows first"],
      "Turn on",
      "Keep asking me",
    );
    if (ok) await changeAutoMedia(true);
  };

  const remove = async () => {
    const ok = await ask(
      "Delete Friends account?",
      ["Your posts, comments, likes, bookmarks and follows are deleted", "Drafts on your computer are discarded"],
      "Delete account",
      "Keep account",
      true,
    );
    if (!ok) return;
    deleting.set(true);
    await deleteAccount();
    deleting.set(false);
  };

  return (
    <Page display title="Friends" testID="friends-settings">
      <Section footer="A Labs feature: your agent posts what it made to people you follow. Your computer holds the account; this phone has no Friends password.">
        <SwitchRow
          testID="friends-switch"
          title="Friends"
          subtitle={subtitle()}
          checked={friendsEnabled() || phase() === "working" || phase() === "signing_in"}
          busy={phase() === "working" && !friends().signIn}
          disabled={deleting() || (!available() && !friendsEnabled())}
          onChange={toggle}
        />
        <If when={friendsEnabled()}><SwitchRow
            testID="friends-auto-post"
            title="Post without asking"
            subtitle={me()?.auto_post ? `Up to ${me()!.daily_cap} a day` : undefined}
            checked={me()?.auto_post === true}
            onChange={(on) => void autoPost(on)}
          /></If>
        <If when={friendsEnabled() && me()?.auto_post}><SwitchRow testID="friends-auto-media" title="Let it attach pictures and video" checked={me()!.auto_media === true} onChange={(on) => void autoMedia(on)} /></If>
        <If when={friendsEnabled()}><Row testID="friends-open" lead={tile("chat")} title="Open Friends" chevron onPress={() => props.nav.push({ name: "friends" })} /></If>
      </Section>

      <If when={phase() === "signing_in" && friends().signIn}><Section title="Sign in to Mewla">
          <View class="items-center gap-4 p-5">
            {/* The code can be selected and copied, to paste it on the sign-in page. */}
            <View selectable>
              <Text testID="friends-code" class="font-mono text-3xl font-semibold tracking-widest select-text" accessibilityLabel={`Code ${friends().signIn!.user_code.split("").join(" ")}`}>
                {friends().signIn!.user_code}
              </Text>
            </View>
            <View class="flex-row gap-3">
              <PillButton testID="friends-open-sign-in" title="Open sign-in page" filled onPress={openSignInPage} />
              <PillButton testID="friends-cancel-sign-in" title="Cancel" onPress={cancelSignIn} />
            </View>
          </View>
        </Section></If>

      <If when={friends().status?.handle}><Section title="Friends account">
          <Row
            title={`@${friends().status!.handle}`}
            subtitle={friends().status!.signed_in ? (friends().status!.account ? `Signed in as ${friends().status!.account}` : "Signed in") : undefined}
            leading={friendsEnabled() && me() ? <Avatar author={me()!} size={40} /> : undefined}
          />
          <Row
            testID="friends-delete"
            title="Delete Friends account"
            destructive
            disabled={phase() === "working" || deleting()}
            trailing={<If when={deleting()}><View testID="friends-deleting" accessibilityLabel="Deleting"><Spinner size={16} /></View></If>}
            onPress={() => void remove()}
          />
        </Section></If>
    </Page>
  );
}
