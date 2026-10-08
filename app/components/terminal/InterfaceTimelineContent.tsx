import React from "react";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { InterfaceSessionIdleView } from "./InterfaceSessionIdleView";
import { InterfaceTimelineEmptyState } from "./InterfaceTimelineEmptyState";
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
  if (suppressed && items.length === 0) {
    return null;
  }

  if (loading && items.length === 0) {
    if (emptyTitle) {
      return (
        <InterfaceTimelineEmptyState
          chrome={chrome}
          title={emptyTitle}
          body={emptyBody}
          busy
          conversationEmpty
        />
      );
    }
    return <InterfaceSessionIdleView chrome={chrome} cwd={workerCwd} busy />;
  }

  if (error && items.length === 0) {
    return (
      <InterfaceTimelineEmptyState
        chrome={chrome}
        title="Could not load this chat"
        body={error}
        tone="error"
        icon="alert-circle"
      />
    );
  }

  if (syncing && items.length === 0) {
    if (emptyTitle) {
      return (
        <InterfaceTimelineEmptyState
          chrome={chrome}
          title={emptyTitle}
          body={emptyBody}
          busy
          conversationEmpty
        />
      );
    }
    return <InterfaceSessionIdleView chrome={chrome} cwd={workerCwd} busy />;
  }

  if (unavailable) {
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
  }

  if (items.length === 0) {
    if (!emptyTitle) {
      return <InterfaceSessionIdleView chrome={chrome} cwd={workerCwd} />;
    }
    return (
      <InterfaceTimelineEmptyState
        chrome={chrome}
        title={emptyTitle}
        body={emptyBody}
        conversationEmpty
      />
    );
  }

  return null;
}
