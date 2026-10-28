import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Platform, Pressable, ScrollView, StyleSheet, Text, TextInput, View } from "react-native";
import { webReplyKeyPress } from "../terminal/composerInputBehavior";
import { InterfaceComposerAttachmentRail } from "../terminal/InterfaceComposerAttachmentRail";
import { InterfaceDropOverlay } from "../terminal/InterfaceDropOverlay";
import type { ComposerAttachment } from "../terminal/InterfaceChatSession";
import { useComposerFileTransfer } from "../terminal/useComposerFileTransfer";
import { useInterfaceComposerAttachments } from "../terminal/useInterfaceComposerAttachments";
import type { SendAttachment } from "../../services/structuredWebSocketTransport";
import type { ConnectionState } from "../../store/workers";
import { workReplyFiles, workReplyReady } from "./workReply";
import { useDesktopWeb } from "../navigation/useDesktopWeb";
import { ResizeHandle, useResizableWidth } from "../navigation/ResizeHandle";
import { PRIMARY_APP_BAR_HEIGHT } from "../navigation/primaryAppBarGeometry";
import { usePrimaryAppBarLayout } from "../navigation/primaryAppBarInset";
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
import { composerNeutralFill } from "../terminal/composerMaterial";
import { CalendarOriginLink } from "../calendar/CalendarOriginLink";

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
  return [slip.calendarLabel ?? slip.who, formatChatBubbleTime(slip.updatedAt)].filter(Boolean).join(" · ") || undefined;
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
          No open Work
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
                metaIcon={slip.calendarLabel ? "calendar" : undefined}
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
  const width = desktopWeb ? panel.width : BRAIN_WORK_COLUMN_WIDTH;
  // The app bar ends where this column starts; the column's own header takes
  // the bar's row, so "Work" sits level with the page title.
  usePrimaryAppBarLayout("brain", { trailingInset: width });
  return (
    <View
      role="complementary"
      accessibilityLabel="Work"
      style={[
        styles.column,
        { paddingTop: Math.max(0, topInset - PRIMARY_APP_BAR_HEIGHT), width },
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
      </View>
      {objective ? (
        <View style={styles.columnGoal}>
          <BrainGoalLine objective={objective} chrome={chrome} />
        </View>
      ) : null}
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
  serverId,
  connectionState = "offline",
  onReply,
  onOpenCalendar,
  onClose,
}: {
  slip: BrainWorkSlip | null;
  waitFor?: string;
  chrome: TerminalThemeChrome;
  actions?: readonly WorkSlipAction[];
  /** Open with the reply box focused ("Reply", "Something else…"). */
  replyOpen?: boolean;
  replyBusy?: boolean;
  /** The computer files are uploaded to, and whether it is reachable. */
  serverId?: string;
  connectionState?: ConnectionState;
  onReply?(text: string, attachments: SendAttachment[]): void;
  /** A Calendar run's Work: open its schedule at this run. */
  onOpenCalendar?(): void;
  onClose(): void;
}) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  // A Calendar run's meta is its schedule, shown as the link below.
  const meta = slip ? (slip.calendarLabel ? formatChatBubbleTime(slip.updatedAt) : slipMeta(slip)) : undefined;
  const [draft, setDraft] = useState("");
  const [attachments, setAttachments] = useState<ComposerAttachment[]>([]);
  const workId = slip?.workId;
  const inputRef = useRef<TextInput>(null);
  const rootRef = useRef<View>(null);
  useEffect(() => {
    setDraft("");
    setAttachments([]);
  }, [workId]);
  const canReply = Boolean(onReply && slip && !slip.closed);
  const focusInput = useCallback(() => inputRef.current?.focus(), []);
  const files = useInterfaceComposerAttachments({
    serverId: serverId ?? "",
    ownerKey: workId ?? "",
    attachments,
    connectionState: serverId ? connectionState : "offline",
    setAttachments,
    focusComposer: focusInput,
  });
  // Web: paste or drop files on the sheet, as in the chats.
  const { dragging } = useComposerFileTransfer({
    rootRef,
    enabled: canReply && !replyBusy && files.canAttach,
    onFiles: files.addFiles,
  });
  const replyReady = workReplyReady(draft, attachments, Boolean(replyBusy));
  // The reply stays in the box until it lands: the sheet closes on success,
  // and a failed send keeps the words and the uploaded files to try again.
  const send = () => {
    if (!onReply || !replyReady) return;
    onReply(draft.trim(), workReplyFiles(attachments));
  };
  const age = slip?.waitedMs && slip.group === "needs" ? `Waiting ${brainWorkAge(slip.waitedMs)}` : undefined;
  return (
    <BottomSheetFrame visible={Boolean(slip)} onClose={onClose} keyboardAvoiding cardStyle={{ backgroundColor: chrome.surface }}>
      {slip ? (
        <View ref={rootRef} style={styles.sheetRoot}>
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
            {slip.calendarLink && onOpenCalendar ? (
              <CalendarOriginLink label={slip.calendarLink} onPress={onOpenCalendar} />
            ) : null}
            {slip.question ? (
              <Text selectable style={styles.question}>{slip.question}</Text>
            ) : slipLine(slip) ? (
              <Text selectable style={styles.detailBody}>{slipLine(slip)}</Text>
            ) : null}
            {waitFor && !slip.calendar && waitFor !== slip.summary && !slip.question && waitFor.includes(" ") ? (
              <Text selectable style={styles.columnSummary}>Waiting for: {waitFor}</Text>
            ) : null}
            {actions?.length ? <WorkSlipActions actions={actions} chrome={chrome} /> : null}
            {canReply ? (
              <InterfaceComposerAttachmentRail
                attachments={attachments}
                activeUpload={files.activeUpload}
                chrome={chrome}
                onRemoveAttachment={files.removeAttachment}
                onCancelUpload={files.cancelUpload}
              />
            ) : null}
            {canReply ? (
              <View style={styles.replyRow}>
                <Pressable
                  accessibilityRole="button"
                  accessibilityLabel="Attach file"
                  accessibilityState={{ disabled: !files.canAttach || replyBusy, busy: files.uploading }}
                  disabled={!files.canAttach || replyBusy}
                  onPress={() => void files.handleUploadAttachment()}
                  style={[styles.replySend, styles.replySendIdle]}
                >
                  <Icon name="attach" size={18} color={files.canAttach ? chrome.textMuted : chrome.textSubtle} />
                </Pressable>
                <TextInput
                  ref={inputRef}
                  accessibilityLabel={`Reply to Brain about ${slip.title}`}
                  value={draft}
                  onChangeText={setDraft}
                  placeholder={slip.question ? "Answer Brain…" : "Tell Brain about this…"}
                  placeholderTextColor={chrome.textSubtle}
                  autoFocus={replyOpen}
                  multiline
                  editable={!replyBusy}
                  onSubmitEditing={send}
                  onKeyPress={
                    Platform.OS === "web"
                      ? (event) =>
                          webReplyKeyPress(event, {
                            canSend: replyReady,
                            send,
                            leave: () => event.currentTarget?.blur?.(),
                          })
                      : undefined
                  }
                  style={styles.replyInput}
                />
                <Pressable
                  accessibilityRole="button"
                  accessibilityLabel="Send reply"
                  disabled={!replyReady}
                  onPress={send}
                  style={[styles.replySend, replyReady ? null : styles.replySendIdle]}
                >
                  <Icon name="arrow-up" size={18} color={replyReady ? chrome.onSeal : chrome.textSubtle} />
                </Pressable>
              </View>
            ) : null}
          </ScrollView>
          {dragging ? <InterfaceDropOverlay chrome={chrome} /> : null}
        </View>
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
      height: PRIMARY_APP_BAR_HEIGHT,
      justifyContent: "center",
      paddingHorizontal: 20,
    },
    columnTitle: {
      ...TypeScale.heading,
      color: chrome.text,
    },
    columnGoal: {
      paddingHorizontal: 20,
      paddingBottom: 10,
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
    sheetRoot: {
      position: "relative",
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
    // Like the Composer's Send: a quiet neutral disc until there is a reply.
    replySendIdle: {
      backgroundColor: composerNeutralFill(chrome),
    },
  });
}
