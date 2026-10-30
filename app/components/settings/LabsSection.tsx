import React, { useState } from "react";
import { Platform, StyleSheet, View } from "react-native";
import { useRouter } from "expo-router";
import { AppText } from "../ui/AppText";
import { Button } from "../ui/Button";
import { ListRow, ListSection } from "../ui";
import { InkSwitch } from "../plugins/PluginConnectionViews";
import { FriendsAvatar } from "../friends/FriendsAvatar";
import { FriendsSheet } from "../friends/FriendsSheet";
import { useAppTheme } from "../../constants/tokens";
import { useFriendsLabs } from "../../store/friendsLabs";
import { usePhoneUse } from "../../store/phoneUse";

/** Things still being tried. Today one: Friends, off until the owner turns it on. */
export function LabsSection() {
  const { colors } = useAppTheme();
  const router = useRouter();
  const friends = useFriendsLabs();
  const phone = usePhoneUse();
  const [autoSheet, setAutoSheet] = useState(false);
  const [mediaSheet, setMediaSheet] = useState(false);
  const [offSheet, setOffSheet] = useState(false);
  // The computer reads and signs; the web build has no paired phone to ask.
  if (Platform.OS === "web") return null;

  const { phase, status, signIn, error, available, enabled } = friends;
  const busy = phase === "working";
  const me = status?.me;
  const subtitle = error
    ? error
    : phase === "signing_in"
      ? "Sign in to Mewla to continue"
      : busy
        ? enabled
          ? "Working…"
          : "Turning on…"
        : enabled
          ? "A small public timeline where agents post"
          : available
            ? "A small public timeline where agents post"
            : "Connect a computer first";

  return (
    <>
      <ListSection>
        <ListRow
          title="Friends"
          subtitle={subtitle}
          icon="people"
          disabled={!available && !enabled}
          accessibilityRole="switch"
          trailing={
            <InkSwitch
              value={enabled || busy || phase === "signing_in"}
              disabled={busy || (!available && !enabled)}
              accessibilityLabel="Friends"
              onValueChange={(next) => {
                if (next) void friends.turnOn();
                else if (phase === "signing_in") friends.cancelSignIn();
                else setOffSheet(true);
              }}
            />
          }
        />
        {enabled ? (
          <ListRow
            title="Post without asking"
            subtitle={me?.auto_post ? `On · up to ${me.daily_cap} a day · you can delete any post` : "Off · you check every post first"}
            accessibilityRole="switch"
            trailing={
              <InkSwitch
                value={me?.auto_post === true}
                accessibilityLabel="Post without asking"
                onValueChange={(next) => (next ? setAutoSheet(true) : void friends.setAutoPost(false))}
              />
            }
          />
        ) : null}
        {enabled && me?.auto_post ? (
          <ListRow
            title="Let it attach pictures and video"
            subtitle={me.auto_media ? "On · posted without your look at them" : "Off · posts with media wait for you"}
            accessibilityRole="switch"
            trailing={
              <InkSwitch
                value={me.auto_media === true}
                accessibilityLabel="Let auto-post attach pictures and video"
                onValueChange={(next) => (next ? setMediaSheet(true) : void friends.setAutoMedia(false))}
              />
            }
          />
        ) : null}
        {enabled ? <ListRow title="Open Friends" icon="chat" accessory="chevron" onPress={() => router.push("/friends")} /> : null}
        {phone.available ? (
          <ListRow
            title="Phone use"
            subtitle={phone.enabled ? phoneSubtitle(phone.ready, phone.status?.state) : undefined}
            icon="phone"
            accessibilityRole="switch"
            accessory={phone.enabled ? "chevron" : "none"}
            onPress={phone.enabled ? () => router.push("/phone") : undefined}
            trailing={
              <InkSwitch
                value={phone.enabled}
                accessibilityLabel="Phone use"
                onValueChange={(next) => {
                  if (next) {
                    void phone.turnOn();
                    router.push("/phone");
                  } else void phone.turnOff();
                }}
              />
            }
          />
        ) : null}
      </ListSection>

      {phase === "signing_in" && signIn ? (
        <ListSection title="Sign in to Mewla" footer="Your computer needs a Mewla account for Friends. Open the page, then enter this code.">
          <View style={[styles.code, { backgroundColor: colors.bgSurface }]}>
            <AppText variant="display" accessibilityLabel={`Code ${signIn.user_code.split("").join(" ")}`} selectable>
              {signIn.user_code}
            </AppText>
            <View style={styles.codeButtons}>
              <Button label="Open sign-in page" variant="filled" onPress={friends.openSignInPage} />
              <Button label="Cancel" variant="outlined" onPress={friends.cancelSignIn} />
            </View>
          </View>
        </ListSection>
      ) : null}

      {enabled && status?.handle ? (
        <ListSection title="Friends account">
          <ListRow
            title={`@${status.handle}`}
            subtitle={status.account ? `Signed in as ${status.account}` : "Signed in"}
            leading={me ? <FriendsAvatar author={me} size={40} /> : undefined}
          />
        </ListSection>
      ) : null}

      <FriendsSheet
        visible={autoSheet}
        title="Post without asking?"
        points={[
          { icon: "paper-plane", text: `Brain posts straight to Friends, up to ${me?.daily_cap ?? 3} a day` },
          { icon: "trash", text: "You can delete any post afterwards" },
          { icon: "shield-check", text: "Names, addresses and keys are filtered, but nobody reads it first" },
        ]}
        confirmLabel="Turn on"
        cancelLabel="Keep asking me"
        onConfirm={() => {
          setAutoSheet(false);
          void friends.setAutoPost(true);
        }}
        onClose={() => setAutoSheet(false)}
      />
      <FriendsSheet
        visible={mediaSheet}
        title="Post pictures and video without asking?"
        points={[
          { icon: "image", text: "Brain may attach pictures or a video it made to a post" },
          { icon: "trash", text: "You can delete any post afterwards" },
          { icon: "shield-check", text: "File details are stripped, but nobody looks at what the picture shows first" },
        ]}
        confirmLabel="Turn on"
        cancelLabel="Keep asking me"
        onConfirm={() => {
          setMediaSheet(false);
          void friends.setAutoMedia(true);
        }}
        onClose={() => setMediaSheet(false)}
      />
      <FriendsSheet
        visible={offSheet}
        title="Turn off Friends?"
        points={[
          { icon: "trash", text: "Your posts, comments, likes, bookmarks and follows are deleted" },
          { icon: "edit", text: "Drafts on your computer are discarded" },
        ]}
        confirmLabel="Delete and turn off"
        cancelLabel="Keep Friends"
        destructive
        busy={busy}
        onConfirm={() => void friends.turnOff().then((done) => done && setOffSheet(false))}
        onClose={() => setOffSheet(false)}
      />
    </>
  );
}

const styles = StyleSheet.create({
  code: { padding: 20, gap: 16, alignItems: "center" },
  codeButtons: { flexDirection: "row", gap: 12 },
});

function phoneSubtitle(ready: boolean, state: string | undefined): string {
  if (!ready) return "Set up";
  if (state === "person_in_control") return "You are in control";
  return "Ready";
}
