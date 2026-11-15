/**
 * Daemon-owned flat Session capabilities. Field names remain model_profile_*
 * on the daemon capability wire even though the visible product is Provider /
 * Model-first.
 */
export type WorkerSessionCapabilities = {
  structured_events: boolean;
  model_profile_managed: boolean;
  model_profile_active_switch: boolean;
  /** The Session's own CLI can step the conversation back to an earlier prompt. */
  message_rewind?: boolean;
};

export function normalizeWorkerSessionCapabilities(
  raw: unknown,
): WorkerSessionCapabilities | undefined {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return undefined;
  const record = raw as Record<string, unknown>;
  return {
    structured_events: record.structured_events === true,
    model_profile_managed: record.model_profile_managed === true,
    model_profile_active_switch: record.model_profile_active_switch === true,
    ...(record.message_rewind === true ? { message_rewind: true } : {}),
  };
}

export function sessionAllowsModelProfileActivation(
  capabilities?: WorkerSessionCapabilities | null,
): boolean {
  return (
    capabilities?.model_profile_managed === true &&
    capabilities.model_profile_active_switch === true
  );
}
