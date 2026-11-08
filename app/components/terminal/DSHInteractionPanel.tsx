import React, { useEffect, useRef, useState } from "react";
import { ScrollView, View } from "react-native";
import type { SessionFileRequest } from "../../services/sessionFilePreview";
import { nextDSHPollDelayMs, type DSHAnswer, type DSHInteraction, type DSHInteractionSnapshot } from "../../services/dshInteractions";
import { wsClient } from "../../services/websocket";
import { useCurrentServer } from "../../store/currentServer";
import { useWorkerServerConnections } from "../../store/workers";
import { AppText, BottomSheetFrame, Button, InlineNotice, ListRow, ListSection } from "../ui";
import { MultilineField } from "../ui/MultilineField";

export function DSHInteractionPanel({ serverId, request }: { serverId: string; request: SessionFileRequest }) {
  const { currentServerId, isCurrentServer } = useCurrentServer();
  const scope = JSON.stringify([serverId, request.workerId, request.processId, request.startedAt]);
  const owner = useRef(scope); owner.current = scope;
  const [snapshot, setSnapshot] = useState<{ scope: string; value: DSHInteractionSnapshot }>();
  const [open, setOpen] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const inFlight = useRef(false);
  const current = currentServerId === serverId && snapshot?.scope === scope ? snapshot.value : undefined;
  const first = current?.items[0];
  const connected = useWorkerServerConnections()[serverId] === "connected";
  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    owner.current = scope; setOpen(false); setError(""); setSnapshot(undefined); inFlight.current = false; setBusy(false);
    if (!connected || currentServerId !== serverId) return () => { if (owner.current === scope) owner.current = ""; };
    const sleep = (ms: number) => new Promise<void>((resolve) => { timer = setTimeout(resolve, ms); });
    // The daemon holds a request that names the revision it last saw until the
    // pending requests change, so this loop idles instead of polling.
    async function watch() {
      let rev: number | undefined;
      let failures = 0;
      while (!cancelled) {
        let delay: number;
        try {
          const next = await wsClient.dshInteraction(serverId, request, undefined, rev);
          if (cancelled || !isCurrentServer(serverId)) return;
          rev = "items" in next ? next.rev : undefined;
          if ("items" in next) { setSnapshot({ scope, value: next }); setError(""); }
          failures = 0;
          delay = nextDSHPollDelayMs({ ok: true, rev });
        } catch (error) {
          if (cancelled || !isCurrentServer(serverId)) return;
          setSnapshot(undefined); setError(error instanceof Error ? error.message : "Requests unavailable");
          failures += 1;
          delay = nextDSHPollDelayMs({ ok: false, failures });
        }
        if (delay > 0) await sleep(delay);
      }
    }
    void watch();
    return () => { cancelled = true; clearTimeout(timer); if (owner.current === scope) owner.current = ""; };
  }, [scope, isCurrentServer, serverId, connected, currentServerId]);
  const respond = async (answer: Omit<DSHAnswer, "epoch" | "rpcId">) => {
    if (!first || !current?.connected || !isCurrentServer(serverId) || inFlight.current) return;
    inFlight.current = true; setBusy(true); setError("");
    const rpcId = first.rpcId;
    try {
      await wsClient.dshInteraction(serverId, request, { ...answer, rpcId, epoch: current.epoch });
      if (owner.current === scope && isCurrentServer(serverId)) { setOpen(false); setSnapshot((previous) => previous?.scope === scope && previous.value.epoch === current.epoch ? { scope, value: { ...previous.value, items: previous.value.items.filter((item) => item.rpcId !== rpcId) } } : previous); }
    } catch (failure) { if (owner.current === scope && isCurrentServer(serverId)) setError(failure instanceof Error ? failure.message : "Answer failed. Refresh before retrying."); }
    finally { inFlight.current = false; if (owner.current === scope && isCurrentServer(serverId)) setBusy(false); }
  };
  if (currentServerId !== serverId) return null;
  if (!first && !error) return null;
  const errorNotice = error ? <InlineNotice tone="danger" title="Request problem" detail={error} detailNumberOfLines={0} /> : null;
  return <View style={{ padding: 10, gap: 8 }}>
    {first ? <InlineNotice tone="accent" icon="lock" title={first.payload.type === "approval/requested" ? "Permission requested" : "Answer requested"}
      action={{ label: "Review", accessibilityLabel: "Review request", onPress: () => setOpen(true) }} /> : null}
    {errorNotice}
    <BottomSheetFrame visible={open && Boolean(first)} onClose={() => setOpen(false)} maxHeight="85%">
      {first ? <InteractionForm key={`${scope}:${current?.epoch}:${first.rpcId}`} interaction={first} busy={busy} respond={respond} /> : null}
      {errorNotice}
    </BottomSheetFrame>
  </View>;
}

function InteractionForm({ interaction, busy, respond }: { interaction: DSHInteraction; busy: boolean; respond(answer: Omit<DSHAnswer, "epoch" | "rpcId">): Promise<void> }) {
  const [values, setValues] = useState<Record<string, { selected: string[]; custom?: string }>>({});
  const payload = interaction.payload;
  const questions = payload.questions ?? [];
  const action = (label: string, answer: Omit<DSHAnswer, "epoch" | "rpcId">, disabled = false, variant: "filled" | "tinted" = "tinted") =>
    <Button label={label} variant={variant} disabled={busy || disabled} onPress={() => void respond(answer)} style={{ flex: 1 }} />;
  return <ScrollView keyboardShouldPersistTaps="handled" style={{ maxHeight: 520 }} contentContainerStyle={{ padding: 16, gap: 12 }}>
    {payload.type === "approval/requested" ? <>
      <AppText variant="heading" selectable>{payload.toolName || "Permission requested"}</AppText>
      {payload.reason ? <AppText variant="body" selectable>{payload.reason}</AppText> : null}
      {payload.callId ? <AppText variant="caption" tone="secondary" selectable>{payload.callId}</AppText> : null}
      <View style={{ flexDirection: "row", gap: 10 }}>{action("Reject", { outcome: "rejected" })}{action("Allow once", { outcome: "allowed-once" }, false, "filled")}</View>
    </> : <>
      {questions.map((question) => <View key={question.id} style={{ gap: 8, marginBottom: 6 }}>
        <AppText variant="heading" selectable>{question.question}</AppText>
        {question.detail ? <AppText variant="caption" tone="secondary" selectable>{question.detail}</AppText> : null}
        {(question.options ?? []).length ? <ListSection>
          {(question.options ?? []).map((option) => {
            const selected = values[question.id]?.selected.includes(option.label) ?? false;
            return <ListRow key={option.label} title={option.label} subtitle={option.description} accessory="check" selected={selected} disabled={busy}
              accessibilityRole="none" accessibilityLabel={option.label} onPress={() => setValues((previous) => {
                const value = previous[question.id] ?? { selected: [] };
                return { ...previous, [question.id]: { selected: question.multiSelect ? selected ? value.selected.filter((label) => label !== option.label) : [...value.selected, option.label] : selected ? [] : [option.label], custom: question.multiSelect ? value.custom : undefined } };
              })} />;
          })}
        </ListSection> : null}
        <MultilineField accessibilityLabel={`Custom answer: ${question.question}`} placeholder="Your answer" editable={!busy} minHeight={48} value={values[question.id]?.custom ?? ""} onChangeText={(custom) => setValues((previous) => ({ ...previous, [question.id]: { selected: question.multiSelect || !custom ? previous[question.id]?.selected ?? [] : [], custom } }))} />
      </View>)}
      <View style={{ flexDirection: "row", gap: 10 }}>{action("Cancel request", { cancel: true })}{action("Send answer", { answer: { answers: questions.map((question) => ({ id: question.id, selected: values[question.id]?.selected ?? [], ...(values[question.id]?.custom?.trim() ? { custom: values[question.id]?.custom?.trim() } : {}) })) } }, !questions.length || questions.some((question) => !values[question.id]?.selected.length && !values[question.id]?.custom?.trim()), "filled")}</View>
    </>}
  </ScrollView>;
}
