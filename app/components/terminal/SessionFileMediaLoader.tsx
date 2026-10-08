import React, { useCallback, useEffect, useState } from "react";
import { ActivityIndicator, StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import {
  bindSessionFileRequestToGeneration,
  buildSessionFileBinarySource,
  sessionFileTooLargeMessage,
  type SessionFileBinarySource,
  type SessionFileIdentity,
  type SessionFileMetadata,
} from "../../services/sessionFilePreview";
import { wsClient } from "../../services/websocket";
import { Icon } from "../icons/Icon";
import { SessionFileMediaPreview } from "./SessionFileMediaPreview";

type LoadState =
  | { status: "loading" }
  | { status: "ready"; metadata: SessionFileMetadata; source: SessionFileBinarySource }
  | { status: "error"; message: string };

/**
 * Plays a file through a live Session's file stream, outside the file preview
 * sheet: the same metadata, read capability and player, nothing parallel.
 */
export function SessionFileMediaLoader({
  serverId,
  daemonId,
  identity,
  path,
  chrome,
}: {
  serverId: string;
  daemonId: string;
  identity: SessionFileIdentity | null;
  path: string;
  chrome: TerminalThemeChrome;
}) {
  const [state, setState] = useState<LoadState>({ status: "loading" });
  const [epoch, setEpoch] = useState(0);
  const workerId = identity?.workerId ?? "";
  const processId = identity?.processId ?? 0;
  const startedAt = identity?.startedAt ?? 0;

  useEffect(() => {
    let cancelled = false;
    setState({ status: "loading" });
    const load = async () => {
      if (!workerId || !processId || !startedAt) {
        throw new Error("Brain isn't running, so this file can't be played yet.");
      }
      const request = { workerId, processId, startedAt, path };
      const metadata = await wsClient.getSessionFileMetadata(serverId, request);
      if (metadata.kind !== "video" && metadata.kind !== "audio") {
        throw new Error("This file isn't a video or audio file Mewla can play.");
      }
      if (metadata.tooLarge) throw new Error(sessionFileTooLargeMessage(metadata));
      const source = await buildSessionFileBinarySource(
        serverId,
        daemonId,
        bindSessionFileRequestToGeneration(request, metadata),
      );
      if (!cancelled) setState({ status: "ready", metadata, source });
    };
    void load().catch((error: unknown) => {
      if (cancelled) return;
      setState({
        status: "error",
        message: error instanceof Error ? error.message : "Could not open the file.",
      });
    });
    return () => {
      cancelled = true;
    };
  }, [daemonId, epoch, path, processId, serverId, startedAt, workerId]);

  const metadata = state.status === "ready" ? state.metadata : null;
  const resolveSource = useCallback(async () => {
    if (!metadata) throw new Error("The file is not open.");
    return buildSessionFileBinarySource(
      serverId,
      daemonId,
      bindSessionFileRequestToGeneration(
        { workerId, processId, startedAt, path: metadata.path },
        metadata,
      ),
    );
  }, [daemonId, metadata, processId, serverId, startedAt, workerId]);
  const fail = useCallback((message: string, stale: boolean) => {
    setState({ status: "error", message });
    if (stale) setEpoch((value) => value + 1);
  }, []);

  if (state.status === "loading") {
    return (
      <View style={styles.state}>
        <ActivityIndicator color={chrome.textMuted} />
      </View>
    );
  }
  if (state.status === "error") {
    return (
      <View style={styles.state}>
        <Icon name="warning-fill" size={20} color={chrome.danger} />
        <Text selectable style={[styles.message, { color: chrome.textMuted }]}>
          {state.message}
        </Text>
      </View>
    );
  }
  return (
    <SessionFileMediaPreview
      key={state.source.uri}
      kind={state.metadata.kind === "audio" ? "audio" : "video"}
      name={state.metadata.name}
      source={state.source}
      resolveSource={resolveSource}
      chrome={chrome}
      onError={fail}
    />
  );
}

const styles = StyleSheet.create({
  state: {
    flex: 1,
    minHeight: 160,
    padding: 24,
    gap: 10,
    alignItems: "center",
    justifyContent: "center",
  },
  message: {
    maxWidth: 360,
    fontFamily: Typography.uiFont,
    fontSize: 13,
    lineHeight: 19,
    textAlign: "center",
  },
});
