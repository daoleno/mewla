import React, { useEffect, useMemo, useState } from "react";
import { FlatList, Pressable, StyleSheet, View } from "react-native";
import { TypeScale, UiTextMetrics, useAppColors } from "../../constants/tokens";
import type { GitBranchInfo, GitBranchesPayload } from "../../services/gitDiff";
import { Icon } from "../icons/Icon";
import { AppText } from "../ui/AppText";
import { BottomSheetFrame } from "../ui/BottomSheetFrame";
import { Button } from "../ui/Button";
import { EmptyState } from "../ui/EmptyState";
import { InlineNotice } from "../ui/InlineNotice";
import { MobileSingleLineInput } from "../ui/MobileSingleLineInput";
import { MultilineField } from "../ui/MultilineField";
import { formatGitAge } from "./gitReviewTime";

/** Write the message, then Commit, or Commit and push. */
export function GitCommitSheet({
  visible,
  fileCount,
  canPush,
  onClose,
  onCommit,
}: {
  visible: boolean;
  fileCount: number;
  /** Commit & push is offered unless HEAD is detached. */
  canPush: boolean;
  onClose(): void;
  onCommit(message: string, push: boolean): Promise<void>;
}) {
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState<"commit" | "push" | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (!visible) setError(null);
  }, [visible]);
  const ready = message.trim().length > 0;
  const submit = async (push: boolean) => {
    if (!ready || busy) return;
    setBusy(push ? "push" : "commit");
    setError(null);
    try {
      await onCommit(message, push);
      setMessage("");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Could not commit.");
    } finally {
      setBusy(null);
    }
  };
  return (
    <BottomSheetFrame visible={visible} keyboardAvoiding maxHeight="80%" contentStyle={styles.sheet} onClose={onClose}>
      <AppText variant="title" accessibilityRole="header">
        {`Commit ${fileCount} ${fileCount === 1 ? "file" : "files"}`}
      </AppText>
      <MultilineField
        accessibilityLabel="Commit message"
        placeholder="Message"
        value={message}
        onChangeText={setMessage}
        minHeight={96}
        autoFocus
        editable={!busy}
      />
      {error ? <InlineNotice tone="danger" title="Couldn't commit" detail={error} /> : null}
      <View style={styles.buttons}>
        <Button label="Commit" variant={canPush ? "tinted" : "filled"} block disabled={!ready} loading={busy === "commit"} onPress={() => void submit(false)} style={styles.flex} />
        {canPush ? <Button label="Commit & push" variant="filled" icon="arrow-up" block disabled={!ready} loading={busy === "push"} onPress={() => void submit(true)} style={styles.flex} /> : null}
      </View>
    </BottomSheetFrame>
  );
}

/** A comment on a hunk or line, sent to the Session's agent with file:line context. */
export function GitAskAgentSheet({
  visible,
  where,
  quote,
  onClose,
  onSend,
}: {
  visible: boolean;
  /** `app/x.ts:42`. */
  where: string;
  /** The first lines of what the comment is about. */
  quote: string[];
  onClose(): void;
  onSend(comment: string): void;
}) {
  const colors = useAppColors();
  const [comment, setComment] = useState("");
  useEffect(() => {
    if (!visible) setComment("");
  }, [visible]);
  return (
    <BottomSheetFrame visible={visible} keyboardAvoiding maxHeight="80%" contentStyle={styles.sheet} onClose={onClose}>
      <AppText variant="title" accessibilityRole="header">
        Ask the agent
      </AppText>
      <AppText variant="caption" tone="tertiary" numberOfLines={1} ellipsizeMode="head">
        {where}
      </AppText>
      {quote.length ? (
        <View style={[styles.quote, { backgroundColor: colors.surfaceSubtle, borderColor: colors.borderSubtle }]}>
          {quote.map((line, index) => (
            <AppText key={index} variant="mono" numberOfLines={1} style={styles.quoteLine}>
              {line}
            </AppText>
          ))}
        </View>
      ) : null}
      <MultilineField
        accessibilityLabel="Comment for the agent"
        placeholder="What should change?"
        value={comment}
        onChangeText={setComment}
        minHeight={84}
        autoFocus
      />
      <Button label="Send to agent" variant="filled" icon="paper-plane" block disabled={!comment.trim()} onPress={() => onSend(comment)} />
    </BottomSheetFrame>
  );
}

/** Local and remote branches; tap to switch, or create one from a name. */
export function GitBranchSheet({
  visible,
  load,
  onClose,
  onSwitch,
}: {
  visible: boolean;
  load(): Promise<GitBranchesPayload>;
  onClose(): void;
  onSwitch(branch: string, create: boolean): Promise<void>;
}) {
  const colors = useAppColors();
  const [payload, setPayload] = useState<GitBranchesPayload | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [busy, setBusy] = useState<string | null>(null);
  useEffect(() => {
    if (!visible) {
      setQuery("");
      setError(null);
      return;
    }
    let cancelled = false;
    setPayload(null);
    load().then(
      (next) => !cancelled && setPayload(next),
      (reason) => !cancelled && setError(reason instanceof Error ? reason.message : "Could not load branches."),
    );
    return () => {
      cancelled = true;
    };
  }, [visible, load]);

  const rows = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return (payload?.branches ?? []).filter((branch) => !needle || branch.name.toLowerCase().includes(needle));
  }, [payload, query]);
  const exact = (payload?.branches ?? []).some((branch) => branch.name === query.trim());
  const canCreate = query.trim().length > 0 && !exact && !/\s/.test(query.trim());

  const run = async (name: string, create: boolean) => {
    setBusy(name);
    setError(null);
    try {
      await onSwitch(name, create);
      onClose();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Could not switch branch.");
    } finally {
      setBusy(null);
    }
  };

  return (
    <BottomSheetFrame visible={visible} keyboardAvoiding maxHeight="80%" contentStyle={styles.sheet} onClose={onClose}>
      <AppText variant="title" accessibilityRole="header">
        Branches
      </AppText>
      <MobileSingleLineInput
        framed
        accessibilityLabel="Find or create a branch"
        placeholder="Find or create a branch"
        value={query}
        onChangeText={setQuery}
        autoCapitalize="none"
        autoCorrect={false}
      />
      {error ? <InlineNotice tone="danger" title="Couldn't switch" detail={error} /> : null}
      {canCreate ? (
        <Button label={`Create ${query.trim()}`} variant="tinted" icon="add" block loading={busy === query.trim()} onPress={() => void run(query.trim(), true)} />
      ) : null}
      {!payload && !error ? (
        <EmptyState size="inline" busy title="Loading branches" />
      ) : (
        <FlatList
          data={rows}
          keyExtractor={(branch) => `${branch.remote ? "remote" : "local"}:${branch.name}`}
          style={styles.list}
          keyboardShouldPersistTaps="handled"
          ListEmptyComponent={payload ? <EmptyState size="inline" title="No matching branches" /> : undefined}
          renderItem={({ item }) => <BranchRow branch={item} busy={busy === item.name} disabled={Boolean(busy)} onPress={() => void run(item.name, false)} colors={colors} />}
        />
      )}
    </BottomSheetFrame>
  );
}

function BranchRow({
  branch,
  busy,
  disabled,
  onPress,
  colors,
}: {
  branch: GitBranchInfo;
  busy: boolean;
  disabled: boolean;
  onPress(): void;
  colors: ReturnType<typeof useAppColors>;
}) {
  const sync = [branch.ahead ? `↑${branch.ahead}` : "", branch.behind ? `↓${branch.behind}` : ""].filter(Boolean).join(" ");
  const detail = [branch.remote ? "on origin" : "", sync, branch.subject, branch.date ? formatGitAge(branch.date) : ""].filter(Boolean).join(" · ");
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${branch.name}${branch.current ? ", current" : ""}`}
      accessibilityState={{ selected: branch.current, busy }}
      disabled={disabled || branch.current}
      onPress={onPress}
      style={({ pressed }) => [styles.branchRow, { opacity: pressed ? 0.6 : 1 }]}
    >
      <Icon name={branch.current ? "check" : "git-branch"} size={16} color={branch.current ? colors.accentStrong : colors.textTertiary} />
      <View style={styles.branchCopy}>
        <AppText variant="compact" numberOfLines={1} style={branch.current ? styles.current : undefined}>
          {branch.name}
        </AppText>
        {detail ? (
          <AppText variant="caption" tone="tertiary" numberOfLines={1}>
            {detail}
          </AppText>
        ) : null}
      </View>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  sheet: { gap: 12, paddingHorizontal: 16, paddingBottom: 8 },
  buttons: { flexDirection: "row", gap: 8 },
  flex: { flex: 1 },
  quote: { borderWidth: StyleSheet.hairlineWidth, borderRadius: 8, paddingHorizontal: 10, paddingVertical: 6 },
  quoteLine: { ...TypeScale.mono, fontSize: 12, lineHeight: 18, ...UiTextMetrics },
  list: { maxHeight: 360 },
  branchRow: { minHeight: 48, flexDirection: "row", alignItems: "center", gap: 10 },
  branchCopy: { flex: 1, minWidth: 0 },
  current: { fontWeight: "600" },
});
