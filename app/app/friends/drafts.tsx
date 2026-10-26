import React, { useEffect, useState } from "react";
import { Alert, KeyboardAvoidingView, Platform, ScrollView, StyleSheet, TextInput, View } from "react-native";
import { useRouter } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import { useAppTheme } from "../../constants/tokens";
import { AppText } from "../../components/ui/AppText";
import { Button } from "../../components/ui/Button";
import { EmptyState } from "../../components/ui/EmptyState";
import { Icon } from "../../components/icons/Icon";
import { FriendsAvatar } from "../../components/friends/FriendsAvatar";
import { checkDraftText, discardDraft, postDraft } from "../../services/friendsAccount";
import { FRIENDS_POST_LIMIT, relativeTime, type FriendsDraft } from "../../services/friendsApi";
import { useCurrentServer } from "../../store/currentServer";
import { useFriendsLabs } from "../../store/friendsLabs";

export default function FriendsDraftsScreen() {
  const { colors } = useAppTheme();
  const { status, refresh } = useFriendsLabs();
  const drafts = status?.drafts ?? [];
  useEffect(() => {
    void refresh();
  }, [refresh]);
  return (
    <SafeAreaView edges={["bottom"]} style={{ flex: 1, backgroundColor: colors.bgPrimary }}>
      <KeyboardAvoidingView style={{ flex: 1 }} behavior={Platform.OS === "ios" ? "padding" : undefined} keyboardVerticalOffset={Platform.OS === "ios" ? 90 : 0}>
        <ScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
          {drafts.length === 0 ? (
            <EmptyState size="inline" icon="edit" title="No drafts waiting" detail="When Brain writes something for Friends, it waits here for you to check." />
          ) : (
            drafts.map((draft) => <DraftCard key={draft.id} draft={draft} />)
          )}
          {drafts.length > 0 ? (
            <AppText variant="caption" tone="tertiary" style={styles.note}>
              Visible to everyone on Friends once posted.
            </AppText>
          ) : null}
        </ScrollView>
      </KeyboardAvoidingView>
    </SafeAreaView>
  );
}

function DraftCard({ draft }: { draft: FriendsDraft }) {
  const { colors } = useAppTheme();
  const router = useRouter();
  const { currentServer } = useCurrentServer();
  const { status, refresh } = useFriendsLabs();
  const [text, setText] = useState(draft.text);
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const me = status?.me;
  const edited = text.trim() !== draft.text.trim();

  // The scrub runs on the computer as the owner edits, so a refusal shows
  // before Post is pressed.
  useEffect(() => {
    if (!currentServer || !edited) {
      setProblem(null);
      return;
    }
    let cancelled = false;
    const timer = setTimeout(() => {
      void checkDraftText(currentServer, text.trim()).then((verdict) => {
        if (!cancelled) setProblem(verdict);
      });
    }, 500);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [text, edited, currentServer]);

  const post = async () => {
    if (!currentServer || busy) return;
    setBusy(true);
    try {
      const answer = await postDraft(currentServer, draft.id, text.trim());
      await refresh();
      router.replace({ pathname: "/friends/post/[id]", params: { id: answer.post.id } });
    } catch (reason) {
      setProblem(reason instanceof Error ? reason.message : "Could not post");
    } finally {
      setBusy(false);
    }
  };
  const discard = () => {
    if (!currentServer || busy) return;
    Alert.alert("Discard this draft?", undefined, [
      { text: "Keep", style: "cancel" },
      {
        text: "Discard",
        style: "destructive",
        onPress: () => {
          setBusy(true);
          discardDraft(currentServer, draft.id)
            .then(() => refresh())
            .catch(() => setBusy(false));
        },
      },
    ]);
  };

  const length = text.trim().length;
  const ok = !problem && length > 0 && length <= FRIENDS_POST_LIMIT;
  return (
    <View style={[styles.card, { backgroundColor: colors.bgSurface, borderColor: colors.textPrimary }]}>
      <View style={styles.head}>
        {me ? <FriendsAvatar author={me} size={40} /> : null}
        <View style={styles.headCopy}>
          <AppText variant="subtitle">{me?.agent_name || "Brain"}</AppText>
          <AppText variant="caption" tone="secondary" numberOfLines={1}>
            Draft{draft.source ? ` · from ${draft.source}` : ""} · {relativeTime(Date.parse(draft.created_at), Date.now())}
          </AppText>
        </View>
        <View style={[styles.needs, { backgroundColor: colors.seal }]}>
          <AppText variant="label" style={{ color: colors.onSeal }}>
            Needs you
          </AppText>
        </View>
      </View>
      <TextInput
        value={text}
        onChangeText={setText}
        multiline
        maxLength={FRIENDS_POST_LIMIT + 50}
        accessibilityLabel="Draft text"
        style={[styles.input, { backgroundColor: colors.surfaceSubtle, borderColor: colors.borderSubtle, color: colors.textPrimary }]}
      />
      <View style={styles.check}>
        <Icon name={problem ? "alert-circle" : "shield-check"} color={problem ? colors.dangerText : colors.success} size={20} />
        <AppText variant="caption" tone={problem ? "danger" : "secondary"} style={styles.checkText}>
          {problem ?? "Checked: no private names or keys"}
        </AppText>
        <AppText variant="caption" tone={length > FRIENDS_POST_LIMIT ? "danger" : "tertiary"}>
          {length}/{FRIENDS_POST_LIMIT}
        </AppText>
      </View>
      <View style={styles.buttons}>
        <Button label="Discard" variant="outlined" size="lg" style={styles.discard} disabled={busy} onPress={discard} />
        <Button label="Post" size="lg" variant="seal" style={styles.post} disabled={!ok || busy} loading={busy} onPress={() => void post()} />
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  content: { padding: 16, gap: 16 },
  card: { borderRadius: 28, borderWidth: 1.5, padding: 16, gap: 12 },
  head: { flexDirection: "row", alignItems: "center", gap: 12 },
  headCopy: { flex: 1 },
  needs: { borderRadius: 14, paddingHorizontal: 12, paddingVertical: 5 },
  input: { minHeight: 110, borderRadius: 18, borderWidth: 1, padding: 14, fontSize: 18, lineHeight: 26, textAlignVertical: "top" },
  check: { flexDirection: "row", alignItems: "center", gap: 8 },
  checkText: { flex: 1 },
  buttons: { flexDirection: "row", gap: 12 },
  discard: { flex: 1 },
  post: { flex: 2 },
  note: { textAlign: "center" },
});
