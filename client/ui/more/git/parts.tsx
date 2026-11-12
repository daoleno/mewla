import { Text, untracked, View } from "nsh";
import type { JSX } from "nsh";
import { Button } from "nsh/base";
import { gitDiffStatusMark, gitDiffStatusWord, gitDiffTone, type GitDiffStatusTone } from "../../../core/git/gitDiffPresentation";
import type { GitDiffFileInfo } from "../../../core/git/gitDiff";
import { color, type ColorName } from "../../kit/colors";
import { Icon, type IconName } from "../../kit/Icon";
import { EmptyState, Spinner } from "../../kit/kit";
import { gitFlash, type GitReview } from "./state";

/** Added and deleted keep the change colours; modified and renamed are ink (amber reads as Warning). */
export function toneColor(tone: GitDiffStatusTone): ColorName {
  switch (tone) {
    case "added":
      return "ready";
    case "deleted":
    case "conflict":
      return "failed";
    case "renamed":
    case "modified":
      return "ink";
    default:
      return "faint";
  }
}

/** The one-character status of a file, coloured by what happened to it. */
export function StatusChar(props: { file: Pick<GitDiffFileInfo, "status" | "binary"> }) {
  const tone = gitDiffTone(props.file);
  return (
    <Text class="w-4 text-center font-mono text-sm font-semibold" style={{ color: color(toneColor(tone)) }} accessibilityLabel={gitDiffStatusWord(tone)}>
      {gitDiffStatusMark(tone)}
    </Text>
  );
}

/** `+12 −3`, a side with no lines left out. */
export function Counts(props: { additions: number; deletions: number; binary?: boolean }) {
  if (props.binary) return null;
  return (
    <View class="flex-row gap-1.5">
      {props.additions > 0 ? <Text class="font-mono text-xs" style={{ color: color("ready") }}>{`+${props.additions}`}</Text> : null}
      {props.deletions > 0 ? <Text class="font-mono text-xs" style={{ color: color("failed") }}>{`−${props.deletions}`}</Text> : null}
    </View>
  );
}

/** A path as a dim folder and its name. */
export function PathText(props: { path: string; class?: string }) {
  const index = props.path.lastIndexOf("/");
  const dir = index === -1 ? "" : props.path.slice(0, index + 1);
  const name = index === -1 ? props.path : props.path.slice(index + 1);
  return (
    <Text class={`font-mono text-sm line-clamp-1 ${props.class ?? ""}`}>
      {dir ? <Text class="text-muted-foreground">{dir}</Text> : null}
      {name}
    </Text>
  );
}

/** A small icon button with its label for screen readers. */
export function IconButton(props: { icon: IconName; label: string; onPress: () => void; disabled?: boolean; selected?: boolean; busy?: boolean; testID?: string }) {
  return (
    <Button
      testID={props.testID}
      accessibilityLabel={props.label}
      selected={props.selected}
      disabled={props.disabled || props.busy}
      onPress={props.onPress}
      class={`size-10 items-center justify-center rounded-full active:bg-accent disabled:opacity-40 ${props.selected ? "bg-secondary" : ""}`}
    >
      {props.busy ? <Spinner size={16} /> : <Icon name={props.icon} size={20} tone="ink" />}
    </Button>
  );
}

/** A capsule with text, tinted when it is the next step. */
export function TextButton(props: { label: string; onPress: () => void; filled?: boolean; busy?: boolean; disabled?: boolean; testID?: string; accessibilityLabel?: string }) {
  return (
    <Button
      testID={props.testID}
      accessibilityLabel={props.accessibilityLabel ?? props.label}
      disabled={props.disabled || props.busy}
      onPress={props.onPress}
      class={`flex-row items-center justify-center gap-1.5 rounded-full px-3 py-1.5 active:opacity-70 disabled:opacity-50 ${props.filled ? "bg-primary" : "bg-secondary"}`}
    >
      {props.busy ? <Spinner size={14} tone={props.filled ? "canvas" : "soft"} /> : null}
      <Text class={`text-sm font-medium ${props.filled ? "text-primary-foreground" : ""}`}>{props.label}</Text>
    </Button>
  );
}

/** What just happened, for a few seconds, with Undo when it can be undone. */
export function FlashLine() {
  return (
    <View>
      {gitFlash() ? (
        <View testID="git-flash" class="mt-3 flex-row items-center gap-3 rounded-2xl border border-border bg-card px-4 py-3">
          <Icon name={gitFlash()!.tone === "error" ? "alert-circle" : gitFlash()!.tone === "success" ? "check-circle" : "info"} size={18} tone={gitFlash()!.tone === "error" ? "failed" : gitFlash()!.tone === "success" ? "ready" : "soft"} />
          <View class="min-w-0 flex-1">
            <Text class="text-sm font-medium">{gitFlash()!.title}</Text>
            {gitFlash()!.detail ? <Text class="text-sm text-muted-foreground line-clamp-3">{gitFlash()!.detail!}</Text> : null}
          </View>
          {gitFlash()!.action ? (
            <Button testID="git-flash-action" onPress={() => gitFlash()!.action!.run()} class="active:opacity-60">
              <Text class="text-sm font-semibold">{gitFlash()!.action!.label}</Text>
            </Button>
          ) : null}
        </View>
      ) : null}
    </View>
  );
}

/** A labelled block inside a page: what a state card is in the old sheet. */
export function Block(props: { children?: JSX.Element; class?: string }) {
  return <View class={`mt-4 overflow-hidden rounded-2xl border border-border bg-card ${props.class ?? ""}`}>{props.children}</View>;
}

export const SHEET = "max-h-[90%] rounded-t-3xl bg-card px-4 pb-8 pt-3";
export const BACKDROP = "bg-black/30";

/**
 * A page's content while its review's computer is current. After a switch the
 * page shows none of the last computer's repository.
 */
export function Current(props: { review: GitReview; class?: string; children: () => JSX.Element }) {
  return (
    <View class={props.class}>
      {props.review.current() ? (
        untracked(props.children)
      ) : (
        <EmptyState testID="git-elsewhere" icon="server" title="Not on the current computer" detail="Switch back to the computer this Session runs on." />
      )}
    </View>
  );
}
