/**
 * Pure presentation policy for the one-surface Providers editor. The overlay
 * is always bound to exactly one target — a new custom endpoint or an
 * existing Provider connection — so this stays free of React Native imports
 * to keep it unit-testable.
 *
 * There is exactly one Edit action per Provider. The unified form edits Name,
 * Base URL and API key together and saves them atomically; an empty API-key
 * field preserves the stored secret (never a separate Replace/Clear key flow).
 */
import type {
  ProviderClient,
  ProviderConnection,
} from "./types";

export type ProvidersEditorState =
  | { kind: "create"; client: ProviderClient }
  | { kind: "edit"; connection: ProviderConnection; retry?: boolean }
  | null;

export type ProviderEditorFieldState = {
  mutating: boolean;
  name: string;
  baseUrl: string;
  apiKey: string;
  /** Inline display-name validation result; null means the name is valid. */
  nameIssue: string | null;
  /** New custom endpoint: name, Base URL and API key are all required. */
  createMode: boolean;
  /** Existing connection: empty API key preserves the stored secret. */
  editMode: boolean;
  /** Advanced/Custom connections always require a Base URL. */
  requiresBaseUrl: boolean;
};

/**
 * Save eligibility for the unified Add/Edit Provider form. In create mode the
 * API key is required; in edit mode an empty key means "keep the stored
 * secret" and is always a valid save (the daemon replaces only on non-empty).
 * A display-name issue (empty, over-long, duplicate) blocks saving so the
 * inline validation and the daemon agree before any write.
 */
export function providerEditorCanSave(
  input: ProviderEditorFieldState,
): boolean {
  if (input.mutating) return false;
  if (input.name.trim().length === 0) return false;
  if (input.nameIssue !== null) return false;
  if (input.requiresBaseUrl && input.baseUrl.trim().length === 0) return false;
  if (input.createMode && input.apiKey.trim().length === 0) return false;
  return input.createMode || input.editMode;
}

/**
 * Initial display name for a fresh form: the connection's current name when
 * editing, otherwise empty.
 */
export function providerEditorInitialName(
  editor: ProvidersEditorState,
): string {
  return editor?.kind === "edit" ? editor.connection.name : "";
}

/**
 * Initial Base URL for a fresh form: the connection's current gateway when
 * editing an advanced connection, otherwise empty. Curated connections have
 * no editable gateway.
 */
export function providerEditorInitialBaseUrl(
  editor: ProvidersEditorState,
): string {
  return editor?.kind === "edit" ? (editor.connection.base_url ?? "") : "";
}

/**
 * Whether the unified form shows an editable Base URL field for this target:
 * new custom endpoints and advanced connections only. Curated connections own
 * the official endpoint and never expose it.
 */
export function providerEditorRequiresBaseUrl(
  editor: ProvidersEditorState,
): boolean {
  if (!editor) return false;
  if (editor.kind === "create") return true;
  return Boolean(editor.connection.base_url);
}
