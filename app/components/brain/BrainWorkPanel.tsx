import React, { useEffect, useMemo, useRef, useState } from "react";
import { Pressable, ScrollView, StyleSheet, Text, TextInput, View } from "react-native";
import { useDesktopWeb } from "../navigation/useDesktopWeb";
import { ResizeHandle, useResizableWidth } from "../navigation/ResizeHandle";
import { formatChatBubbleTime } from "../../constants/telegramPresentation";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography, TypeScale } from "../../constants/tokens";
import type { BrainObjective } from "../../store/brain";
import { BottomSheetFrame } from "../ui/BottomSheetFrame";
import {
  BRAIN_WORK_GROUP_LABELS,
  BRAIN_WORK_GROUP_ORDER,
  brainWorkAge,
  brainWorkSummaryLine,
  type BrainWorkGroup,
  type BrainWorkSlip,
  type BrainWorkSurface,
} from "./brainWorkSurface";
import { PetSprite } from "../pets/PetSprite";
import { WorkSlip, WorkSlipActions, WorkStatusWord, type WorkSlipAction } from "./WorkSlip";
import { Icon } from "../icons/Icon";

/** The slip's actions, built by the screen that can run them. */
export type BrainWorkActionsFor = (slip: BrainWorkSlip, placement: "slip" | "sheet") => WorkSlipAction[];

/** Width of the Work column beside the conversation on wide screens. */
export const BRAIN_WORK_COLUMN_WIDTH = 360;
const BRAIN_WORK_COLUMN_MIN_WIDTH = 280;
const BRAIN_WORK_COLUMN_MAX_WIDTH = 560;

/** The summary, with a running Worker's phase in front ("Verifying · …"). */
function slipLine(slip: BrainWorkSlip): string | undefined {
  if (!slip.phase) return slip.summary;
  const phase = slip.phase[0].toUpperCase() + slip.phase.slice(1);
  return slip.summary ? `${phase} · ${slip.summary}` : phase;
}

function slipMeta(slip: BrainWorkSlip): string | undefined {
  return [slip.who, formatChatBubbleTime(slip.updatedAt)].filter(Boolean).join(" · ") || undefined;
}

/**
 * Current Work as slips, one caption per state. The captions carry no count:
 * the sheet's summary line ("6 running · 2 back") is the one count there,
 * and beside the wide column the cat's row is. The first needs-you slip
 * carries the cat when `perch` is set (the wide column is where it sits).
 */
export function BrainWorkList({
  surface,
  chrome,
  perch,
  animate,
  onOpenSlip,
  actionsFor,
  onCatPress,
  emptyCat,
  highlight,
  onGroupLayout,
}: {
  surface: BrainWorkSurface;
  chrome: TerminalThemeChrome;
  perch?: boolean;
  animate?: boolean;
  onOpenSlip(slip: BrainWorkSlip): void;
  actionsFor?: BrainWorkActionsFor;
  onCatPress?: () => void;
  /** The sheet covers the chat, so its empty list can hold the cat. */
  emptyCat?: boolean;
  /** The group the cat's row just pointed at; its caption lights up. */
  highlight?: BrainWorkGroup | null;
  onGroupLayout?(group: BrainWorkGroup, y: number): void;
}) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  if (surface.slips.length === 0) {
    return (
      <View style={emptyCat ? styles.emptyWithCat : undefined}>
        {emptyCat ? <PetSprite state="idle" size={72} animate={animate} /> : null}
        <Text style={[styles.empty, emptyCat ? styles.emptyCentered : null]}>
          Nothing out right now. Work Brain hands off shows up here.
        </Text>
      </View>
    );
  }
  const firstNeeds = surface.slips.find((slip) => slip.group === "needs")?.workId;
  return (
    <View>
      {BRAIN_WORK_GROUP_ORDER.map((group) => {
        const slips = surface.slips.filter((slip) => slip.group === group);
        if (slips.length === 0) return null;
        return (
          <View
            key={group}
            style={styles.group}
            onLayout={onGroupLayout ? (event) => onGroupLayout(group, event.nativeEvent.layout.y) : undefined}
          >
            <Text accessibilityRole="header" style={[styles.groupLabel, highlight === group ? styles.groupLabelLit : null]}>
              {BRAIN_WORK_GROUP_LABELS[group]}
            </Text>
            {slips.map((slip) => (
              <WorkSlip
                key={slip.workId}
                chrome={chrome}
                status={slip.status}
                statusLabel={slip.statusLabel}
                // The group caption says it once; a card speaks only when it differs.
                statusShown={slip.statusLabel !== BRAIN_WORK_GROUP_LABELS[group]}
                title={slip.title}
                meta={slipMeta(slip)}
                summary={slipLine(slip)}
                unread={slip.unread}
                perched={Boolean(perch && slip.workId === firstNeeds)}
                animate={animate}
                onPress={() => onOpenSlip(slip)}
                actions={actionsFor?.(slip, "slip")}
                onCatPress={onCatPress}
                accessibilityLabel={[slip.statusLabel, slip.title, slip.summary].filter(Boolean).join(", ")}
              />
            ))}
          </View>
        );
      })}
    </View>
  );
}

/** A request to show one group of the Work column; `seq` repeats a tap. */
export type BrainWorkReveal = { group: BrainWorkGroup; seq: number };

/** How long a revealed group's caption stays lit. */
const REVEAL_MS = 1600;

/** Wide screens: the Work column to the right of the conversation. */
export function BrainWorkColumn({
  surface,
  objective,
  chrome,
  topInset,
  animate,
  onOpenSlip,
  actionsFor,
  onCatPress,
  perch = true,
  reveal,
}: {
  surface: BrainWorkSurface;
  /** What the Work is for, while it is current (see the daemon's objective rule). */
  objective?: BrainObjective;
  chrome: TerminalThemeChrome;
  topInset: number;
  animate: boolean;
  /** False while Brain's turn runs: the cat is in the conversation, working. */
  perch?: boolean;
  onOpenSlip(slip: BrainWorkSlip): void;
  actionsFor?: BrainWorkActionsFor;
  onCatPress?: () => void;
  /** The cat's row was tapped: scroll to this group and light its caption. */
  reveal?: BrainWorkReveal | null;
}) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  const scrollRef = useRef<ScrollView>(null);
  const groupY = useRef(new Map<BrainWorkGroup, number>());
  const [lit, setLit] = useState<BrainWorkGroup | null>(null);
  useEffect(() => {
    if (!reveal) return;
    const y = groupY.current.get(reveal.group);
    scrollRef.current?.scrollTo({ y: y ?? 0, animated: true });
    if (y === undefined) return;
    setLit(reveal.group);
    const timer = setTimeout(() => setLit(null), REVEAL_MS);
    return () => clearTimeout(timer);
  }, [reveal]);
  // Desktop web: a side panel you can resize; the width persists.
  const desktopWeb = useDesktopWeb();
  const panel = useResizableWidth(
    "mewla.desktop.workColumnWidth",
    BRAIN_WORK_COLUMN_WIDTH,
    BRAIN_WORK_COLUMN_MIN_WIDTH,
    BRAIN_WORK_COLUMN_MAX_WIDTH,
  );
  return (
    <View
      role="complementary"
      accessibilityLabel="Work"
      style={[
        styles.column,
        { paddingTop: topInset },
        desktopWeb ? { width: panel.width } : null,
      ]}
    >
      {desktopWeb ? (
        <ResizeHandle
          edge="left"
          width={panel.width}
          label="Resize Work"
          onResize={panel.update}
          onCommit={panel.commit}
          onReset={panel.reset}
        />
      ) : null}
      <View style={styles.columnHeader}>
        <Text accessibilityRole="header" style={styles.columnTitle}>Work</Text>
        {objective ? <BrainGoalLine objective={objective} chrome={chrome} /> : null}
      </View>
      <ScrollView ref={scrollRef} contentContainerStyle={styles.columnContent}>
        <BrainWorkList
          surface={surface}
          chrome={chrome}
          perch={perch}
          animate={animate}
          onOpenSlip={onOpenSlip}
          actionsFor={actionsFor}
          onCatPress={onCatPress}
          highlight={lit}
          onGroupLayout={(group, y) => groupY.current.set(group, y)}
        />
      </ScrollView>
    </View>
  );
}

/** "Ship atlas-notes v1.4 this week · 2 of 5 back" */
export function BrainGoalLine({
  objective,
  chrome,
}: {
  objective: BrainObjective;
  chrome: TerminalThemeChrome;
}) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  return (
    <Text
      numberOfLines={2}
      accessibilityLabel={`Objective: ${objective.title}, ${objective.back} of ${objective.total} back`}
      style={styles.goal}
    >
      {objective.title}
      {objective.total > 0 ? (
        <Text style={styles.goalProgress}>
          {" · "}
          {objective.back} of {objective.total} back
        </Text>
      ) : null}
    </Text>
  );
}

/** Phone: the Work list as a sheet. */
export function BrainWorkSheet({
  visible,
  surface,
  objective,
  chrome,
  onClose,
  onOpenSlip,
  actionsFor,
}: {
  visible: boolean;
  surface: BrainWorkSurface;
  objective?: BrainObjective;
  chrome: TerminalThemeChrome;
  onClose(): void;
  onOpenSlip(slip: BrainWorkSlip): void;
  actionsFor?: BrainWorkActionsFor;
}) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  // With a goal line above ("2 of 5 back"), a second "back" count would read
  // as the same number; the Back group below still lists them.
  const summary = brainWorkSummaryLine(objective ? { ...surface.counts, back: 0 } : surface.counts);
  return (
    <BottomSheetFrame visible={visible} onClose={onClose} maxHeight="85%" cardStyle={{ backgroundColor: chrome.appBackground }}>
      <View style={styles.sheetHeader}>
        <View style={styles.sheetHeading}>
          <Text accessibilityRole="header" style={styles.columnTitle}>Work</Text>
          {objective ? <BrainGoalLine objective={objective} chrome={chrome} /> : null}
          {summary ? (
            <Text numberOfLines={1} style={styles.columnSummary}>{summary}</Text>
          ) : null}
        </View>
        <Pressable accessibilityRole="button" accessibilityLabel="Close Work" onPress={onClose} style={styles.close}>
          <Icon name="close" size={22} color={chrome.textMuted} />
        </Pressable>
      </View>
      <ScrollView contentContainerStyle={styles.sheetContent}>
        <BrainWorkList surface={surface} chrome={chrome} onOpenSlip={onOpenSlip} actionsFor={actionsFor} emptyCat />
      </ScrollView>
    </BottomSheetFrame>
  );
}

/**
 * One Work, and everything you can do with it: Brain's question with its
 * answers, a reply box, and the state's actions (BRAIN_WORK_ACTIONS).
 */
export function BrainWorkDetailSheet({
  slip,
  waitFor,
  chrome,
  actions,
  replyOpen,
  replyBusy,
  onReply,
  onClose,
}: {
  slip: BrainWorkSlip | null;
  waitFor?: string;
  chrome: TerminalThemeChrome;
  actions?: readonly WorkSlipAction[];
  /** Open with the reply box focused ("Reply", "Something else…"). */
  replyOpen?: boolean;
  replyBusy?: boolean;
  onReply?(text: string): void;
  onClose(): void;
}) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  const meta = slip ? slipMeta(slip) : undefined;
  const [draft, setDraft] = useState("");
  const workId = slip?.workId;
  useEffect(() => setDraft(""), [workId]);
  const canReply = Boolean(onReply && slip && !slip.closed);
  const send = () => {
    const text = draft.trim();
    if (!text || !onReply) return;
    onReply(text);
    setDraft("");
  };
  const age = slip?.waitedMs && slip.group === "needs" ? `Waiting ${brainWorkAge(slip.waitedMs)}` : undefined;
  return (
    <BottomSheetFrame visible={Boolean(slip)} onClose={onClose} keyboardAvoiding cardStyle={{ backgroundColor: chrome.surface }}>
      {slip ? (
        <>
          <View style={styles.sheetHeader}>
            <Text selectable style={styles.detailTitle}>{slip.title}</Text>
            <Pressable accessibilityRole="button" accessibilityLabel="Close Work details" onPress={onClose} style={styles.close}>
              <Icon name="close" size={22} color={chrome.textMuted} />
            </Pressable>
          </View>
          <ScrollView contentContainerStyle={styles.detailContent} keyboardShouldPersistTaps="handled">
            <View style={styles.detailMeta}>
              <WorkStatusWord status={slip.status} label={slip.statusLabel} chrome={chrome} />
              {[meta, age].filter(Boolean).length ? (
                <Text style={styles.columnSummary}>{[meta, age].filter(Boolean).join(" · ")}</Text>
              ) : null}
            </View>
            {slip.question ? (
              <Text selectable style={styles.question}>{slip.question}</Text>
            ) : slipLine(slip) ? (
              <Text selectable style={styles.detailBody}>{slipLine(slip)}</Text>
            ) : null}
            {waitFor && waitFor !== slip.summary && !slip.question && waitFor.includes(" ") ? (
              <Text selectable style={styles.columnSummary}>Waiting for: {waitFor}</Text>
            ) : null}
            {actions?.length ? <WorkSlipActions actions={actions} chrome={chrome} /> : null}
            {canReply ? (
              <View style={styles.replyRow}>
                <TextInput
                  accessibilityLabel={`Reply to Brain about ${slip.title}`}
                  value={draft}
                  onChangeText={setDraft}
                  placeholder={slip.question ? "Answer Brain…" : "Tell Brain about this…"}
                  placeholderTextColor={chrome.textSubtle}
                  autoFocus={replyOpen}
                  multiline
                  editable={!replyBusy}
                  onSubmitEditing={send}
                  style={styles.replyInput}
                />
                <Pressable
                  accessibilityRole="button"
                  accessibilityLabel="Send reply"
                  disabled={!draft.trim() || replyBusy}
                  onPress={send}
                  style={[styles.replySend, !draft.trim() || replyBusy ? styles.replySendIdle : null]}
                >
                  <Icon name="arrow-up" size={18} color={chrome.onSeal} />
                </Pressable>
              </View>
            ) : null}
          </ScrollView>
        </>
      ) : null}
    </BottomSheetFrame>
  );
}

function createStyles(chrome: TerminalThemeChrome) {
  return StyleSheet.create({
    group: {
      marginBottom: 6,
    },
    groupLabel: {
      ...TypeScale.caption,
      fontFamily: Typography.uiFontMedium,
      color: chrome.textSubtle,
      marginBottom: 8,
      marginTop: 4,
    },
    groupLabelLit: {
      color: chrome.text,
      backgroundColor: chrome.surfaceActive,
      borderRadius: 6,
      alignSelf: "flex-start",
      marginHorizontal: -6,
      paddingHorizontal: 6,
    },
    empty: {
      ...TypeScale.compact,
      color: chrome.textMuted,
      paddingVertical: 12,
    },
    emptyWithCat: {
      alignItems: "center",
      paddingTop: 16,
    },
    emptyCentered: {
      textAlign: "center",
    },
    column: {
      width: BRAIN_WORK_COLUMN_WIDTH,
      position: "relative",
      borderLeftWidth: StyleSheet.hairlineWidth,
      borderLeftColor: chrome.border,
      backgroundColor: chrome.appBackground,
    },
    columnHeader: {
      paddingHorizontal: 20,
      paddingTop: 8,
      paddingBottom: 10,
      gap: 2,
    },
    columnTitle: {
      ...TypeScale.label,
      fontFamily: Typography.uiFontSemibold,
      color: chrome.text,
    },
    columnSummary: {
      ...TypeScale.caption,
      color: chrome.textMuted,
    },
    columnContent: {
      paddingHorizontal: 20,
      paddingBottom: 24,
    },
    goal: {
      ...TypeScale.compact,
      fontFamily: Typography.uiFontSemibold,
      color: chrome.text,
    },
    goalProgress: {
      fontFamily: Typography.uiFontMedium,
      color: chrome.textMuted,
    },
    sheetHeader: {
      flexDirection: "row",
      alignItems: "center",
      gap: 8,
    },
    sheetHeading: {
      flexShrink: 1,
      gap: 2,
    },
    close: {
      width: 44,
      height: 44,
      marginLeft: "auto",
      alignItems: "center",
      justifyContent: "center",
    },
    sheetContent: {
      paddingTop: 4,
      paddingBottom: 12,
    },
    detailTitle: {
      ...TypeScale.body,
      fontFamily: Typography.uiFontSemibold,
      color: chrome.text,
      flex: 1,
    },
    detailContent: {
      gap: 12,
      paddingVertical: 8,
    },
    detailMeta: {
      flexDirection: "row",
      alignItems: "center",
      gap: 10,
      flexWrap: "wrap",
    },
    detailBody: {
      ...TypeScale.compact,
      color: chrome.text,
      lineHeight: 20,
    },
    question: {
      ...TypeScale.body,
      color: chrome.text,
    },
    replyRow: {
      flexDirection: "row",
      alignItems: "flex-end",
      gap: 8,
      marginTop: 4,
    },
    replyInput: {
      ...TypeScale.compact,
      flex: 1,
      minHeight: 40,
      maxHeight: 120,
      paddingHorizontal: 14,
      paddingVertical: 10,
      borderRadius: 20,
      borderWidth: 1,
      borderColor: chrome.border,
      color: chrome.text,
      backgroundColor: chrome.appBackground,
    },
    replySend: {
      width: 40,
      height: 40,
      borderRadius: 20,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: chrome.seal,
    },
    replySendIdle: {
      opacity: 0.35,
    },
  });
}
