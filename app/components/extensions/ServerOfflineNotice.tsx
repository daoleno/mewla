import React from "react";
import { InlineNotice } from "../ui/InlineNotice";

export type ServerConnection = "offline" | "connecting" | "connected";

/**
 * Connection status has one home: the menu's footer. A page names the server
 * only when it is offline and the page can't work, as a compact notice.
 */
export function ServerOfflineNotice({
  name,
  connection,
  detail = "What you see may be out of date. Switch servers in Settings.",
}: {
  name: string;
  connection: ServerConnection;
  detail?: string;
}) {
  if (connection !== "offline") return null;
  return <InlineNotice tone="warning" title={`${name} is offline`} detail={detail} />;
}
