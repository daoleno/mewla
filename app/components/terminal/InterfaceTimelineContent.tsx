import React from "react";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { InterfaceSessionIdleView } from "./InterfaceSessionIdleView";
import { InterfaceTimelineEmptyState } from "./InterfaceTimelineEmptyState";
import { InterfaceTimelineLoadingState } from "./InterfaceTimelineLoadingState";
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
      return null;
    case "loading":
      // History may still arrive, so Brain shows its outline, not "empty".
      return emptyTitle ? (
        <InterfaceTimelineLoadingState chrome={chrome} />
      ) : (
        <InterfaceSessionIdleView chrome={chrome} cwd={workerCwd} busy />
      );
    case "error":
      return (
        <InterfaceTimelineEmptyState
          chrome={chrome}
          title="Could not load this chat"
          body={error ?? undefined}
          tone="error"
          icon="alert-circle"
        />
      );
    case "unavailable":
      return (
        <InterfaceTimelineEmptyState
          chrome={chrome}
          title="Chat view is not available here"
          body={unavailableReason}
          icon="layers"
          actionLabel={showUnavailableAction ? "Open Terminal" : undefined}
          onAction={showUnavailableAction ? onUnavailableAction : undefined}
        />
      );
    case "empty":
      return emptyTitle ? (
        <InterfaceTimelineEmptyState
          chrome={chrome}
          title={emptyTitle}
          body={emptyBody}
          conversationEmpty
        />
      ) : (
        <InterfaceSessionIdleView chrome={chrome} cwd={workerCwd} />
      );
  }
}
