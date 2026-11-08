import React from "react";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { InterfaceSessionIdleView } from "./InterfaceSessionIdleView";
import { EmptyState } from "../ui/EmptyState";
import { useBrainCompanion } from "../mewla/BrainCompanion";
import { PetSprite } from "../pets/PetSprite";
import { interfaceTimelinePhase } from "./interfaceTimelinePhase";
import type { TimelineItem } from "./InterfaceTimelineItemView";

interface InterfaceTimelineEmptyContentProps {
  items: TimelineItem[];
  loading: boolean;
  error?: string | null;
  suppressed: boolean;
  unavailable: boolean | null;
  unavailableReason?: string;
  syncing: boolean;
  chrome: TerminalThemeChrome;
  workerCwd?: string;
  onUnavailableAction?: () => void;
  showUnavailableAction?: boolean;
  emptyTitle?: string;
  emptyBody?: string;
}

/** A loaded, empty conversation in Brain: the pet sleeps where the halo would be. */
function useSealArt() {
  const companion = useBrainCompanion();
  return companion ? <PetSprite state="idle" size={112} animate={companion.animate} /> : undefined;
}

export function InterfaceTimelineEmptyContent({
  items,
  loading,
  error,
  suppressed,
  unavailable,
  unavailableReason,
  syncing,
  chrome,
  workerCwd,
  onUnavailableAction,
  showUnavailableAction = true,
  emptyTitle,
  emptyBody,
}: InterfaceTimelineEmptyContentProps) {
  const sealArt = useSealArt();
  switch (
    interfaceTimelinePhase({
      itemCount: items.length,
      loading,
      error,
      suppressed,
      unavailable,
      syncing,
    })
  ) {
    case "content":
    case "hidden":
    // History may still arrive, so never "empty": the timeline draws the
    // loading screen over the list once a load runs past its delay.
    case "loading":
      return null;
    case "error":
      return (
        <EmptyState
          title="Could not load this chat"
          detail={error}
          tone="danger"
          icon="alert-circle"
        />
      );
    case "unavailable":
      return (
        <EmptyState
          title="Chat view is not available here"
          detail={unavailableReason}
          icon="layers"
          action={
            showUnavailableAction && onUnavailableAction
              ? { label: "Open Terminal", icon: "terminal", onPress: onUnavailableAction }
              : undefined
          }
        />
      );
    case "empty":
      return emptyTitle ? (
        <EmptyState title={emptyTitle} detail={emptyBody} icon="chat-dots" art={sealArt} />
      ) : (
        <InterfaceSessionIdleView chrome={chrome} cwd={workerCwd} />
      );
  }
}
