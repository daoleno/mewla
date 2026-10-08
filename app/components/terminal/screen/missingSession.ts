export interface MissingSessionInput {
  /** The route resolved a Session (store row, Brain host or route hint). */
  resolved: boolean;
  /** The route resolved a Session at some point since this screen mounted. */
  everResolved: boolean;
  /** A full worker_session_list arrived for the current connection. */
  sessionListFresh: boolean;
  /** brain_snapshot arrived, so the hidden Brain host is known. */
  brainHostKnown: boolean;
}

/**
 * A Terminal link to a Session the server does not have (closed or unknown)
 * leaves for the Session list once the server has confirmed its list and
 * Brain host. A Session that ends while it is open keeps its screen.
 */
export function shouldLeaveMissingSession(input: MissingSessionInput): boolean {
  return !input.resolved && !input.everResolved && input.sessionListFresh && input.brainHostKnown;
}
