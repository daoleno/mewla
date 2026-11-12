import { Text, untracked, View } from "nsh";
import type { JSX } from "nsh";
import { Button } from "nsh/base";
import { gitDiffStatusMark, gitDiffStatusWord, gitDiffTone, type GitDiffStatusTone } from "../../../core/git/gitDiffPresentation";
import type { GitDiffFileInfo } from "../../../core/git/gitDiff";
import { color, type ColorName } from "../../kit/colors";
import { Icon, type IconName } from "../../kit/Icon";
import { EmptyState, Spinner, Tile } from "../../kit/kit";
import { gitFlash, type GitReview } from "./state";
import { haptic } from "../../shell/haptics";

/** Added and deleted keep the change colours (`add`, `del`); modified and renamed are ink. */
export function toneColor(tone: GitDiffStatusTone): ColorName {
  switch (tone) {
    case "added":
      return "add";
    case "deleted":
    case "conflict":
      return "del";
    case "renamed":
    case "modified":
      return "ink";
    default:
      return "faint";
  }
}

/** A file's status as a 28 pt tile (SPEC §15 Code review): M, A, D or R in mono, coloured by what happened to it. */
export function StatusTile(props: { file: Pick<GitDiffFileInfo, "status" | "binary"> }) {
  const tone = gitDiffTone(props.file);
  return (
    <Tile size={28}>
      <Text class="font-mono text-caption font-semibold" style={{ color: color(toneColor(tone)) }} accessibilityLabel={gitDiffStatusWord(tone)}>
        {gitDiffStatusMark(tone)}
      </Text>
    </Tile>
  );
}

/** `+12 −3`, a side with no lines left out. */
export function Counts(props: { additions: number; deletions: number; binary?: boolean }) {
  if (props.binary) return null;
  return (
    <View class="flex-row gap-1.5">
      {props.additions > 0 ? <Text class="font-mono text-code" style={{ color: color("add"), fontVariant: ["tabular-nums"] }}>{`+${props.additions}`}</Text> : null}
      {props.deletions > 0 ? <Text class="font-mono text-code" style={{ color: color("del"), fontVariant: ["tabular-nums"] }}>{`−${props.deletions}`}</Text> : null}
    </View>
  );
}

/** A file's name over its folder (a file row's two lines). */
export function FileName(props: { path: string }) {
  const index = props.path.lastIndexOf("/");
  return (
    <View class="min-w-0">
      <Text class="font-mono text-code font-semibold line-clamp-1">{index === -1 ? props.path : props.path.slice(index + 1)}</Text>
      {index > 0 ? <Text class="font-mono text-caption text-muted-foreground line-clamp-1">{props.path.slice(0, index)}</Text> : null}
    </View>
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
      onPress={() => {
        haptic.selection();
        props.onPress();
      }}
      class={`size-11 items-center justify-center rounded-full active:bg-accent disabled:opacity-40 ${props.selected ? "bg-muted" : ""}`}
    >
      {props.busy ? <Spinner size={16} /> : <Icon name={props.icon} size={20} tone="ink" />}
    </Button>
  );
}

/** A small capsule with text (SPEC §15 Code review's ghost buttons): ink when it is the next step. */
export function TextButton(props: { label: string; onPress: () => void; filled?: boolean; busy?: boolean; disabled?: boolean; testID?: string; accessibilityLabel?: string }) {
  return (
    <Button
      testID={props.testID}
      accessibilityLabel={props.accessibilityLabel ?? props.label}
      disabled={props.disabled || props.busy}
      onPress={props.onPress}
      hitSlop={4}
      class={`h-9 flex-row items-center justify-center gap-1.5 rounded-full px-3.5 active:opacity-70 disabled:opacity-40 ${props.filled ? "bg-primary" : "bg-muted"}`}
    >
      {props.busy ? <Spinner size={14} tone={props.filled ? "canvas" : "soft"} /> : null}
      <Text class={`text-sub font-semibold line-clamp-1 ${props.filled ? "text-primary-foreground" : ""}`}>{props.label}</Text>
    </Button>
  );
}

/** What just happened, for a few seconds, with Undo when it can be undone. */
export function FlashLine() {
  return (
    <View>
      {gitFlash() ? (
        <View testID="git-flash" class="mt-3 min-h-[52px] flex-row items-center gap-3 rounded-(--radius-group) bg-card px-4 py-3">
          <Icon name={gitFlash()!.tone === "error" ? "alert-circle" : gitFlash()!.tone === "success" ? "check-circle" : "info"} size={18} tone={gitFlash()!.tone === "error" ? "failed" : gitFlash()!.tone === "success" ? "ready" : "soft"} />
          <View class="min-w-0 flex-1">
            <Text class="text-body font-semibold">{gitFlash()!.title}</Text>
            {gitFlash()!.detail ? <Text class="text-sub text-muted-foreground line-clamp-3">{gitFlash()!.detail!}</Text> : null}
          </View>
          {gitFlash()!.action ? (
            <Button testID="git-flash-action" onPress={() => gitFlash()!.action!.run()} class="active:opacity-60">
              <Text class="text-sub font-semibold">{gitFlash()!.action!.label}</Text>
            </Button>
          ) : null}
        </View>
      ) : null}
    </View>
  );
}

/** A labelled block inside a page: what a state card is in the old sheet. */
export function Block(props: { children?: JSX.Element; class?: string }) {
  return <View class={`mt-4 overflow-hidden rounded-(--radius-group) bg-card ${props.class ?? ""}`}>{props.children}</View>;
}

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
