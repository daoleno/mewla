import { Text, untracked, View, type ViewRef } from "nsh";
import type { JSX } from "nsh";
import { Button } from "nsh/base";
import { gitDiffStatusMark, gitDiffStatusWord, gitDiffTone } from "../../../core/git/gitDiffPresentation";
import type { GitDiffFileInfo } from "../../../core/git/gitDiff";
import { splitGitReviewPath } from "../../../core/git/gitReviewModel";
import { statusTileLook } from "../../../core/git/colour";
import { color, withAlpha } from "../../kit/colors";
import { Icon, type IconName } from "../../kit/Icon";
import { EmptyState, Spinner } from "../../kit/kit";
import { gitFlash, type GitReview } from "./state";

/**
 * A file's status as a 28 pt tile (SPEC §15 Code review, §10.4): its mark in mono, in its colour on its wash —
 * added on `add` at 13 %, deleted on `del` at 11 %, modified in the meter blue at 12 %, renamed in ink on the neutral tile.
 */
export function StatusTile(props: { file: Pick<GitDiffFileInfo, "status" | "binary"> }) {
  const tone = () => gitDiffTone(props.file);
  const wash = () => {
    const w = statusTileLook(tone()).wash;
    return typeof w === "string" ? color(w) : withAlpha(color(w.of), w.alpha);
  };
  return (
    <View testID="git-status-tile" class="items-center justify-center" style={{ width: 28, height: 28, borderRadius: 8, backgroundColor: wash() }}>
      <Text class="font-mono text-caption font-semibold" style={{ color: color(statusTileLook(tone()).glyph) }} accessibilityLabel={gitDiffStatusWord(tone())}>
        {gitDiffStatusMark(tone())}
      </Text>
    </View>
  );
}

/** `+12 −3`, a side with no lines left out. */
export function Counts(props: { additions: number; deletions: number; binary?: boolean }) {
  if (props.binary) return null;
  return (
    <View class="flex-row gap-1.5">
      <If when={props.additions > 0}><Text class="font-mono text-code" style={{ color: color("add"), fontVariant: ["tabular-nums"] }}>{`+${props.additions}`}</Text></If>
      <If when={props.deletions > 0}><Text class="font-mono text-code" style={{ color: color("del"), fontVariant: ["tabular-nums"] }}>{`−${props.deletions}`}</Text></If>
    </View>
  );
}

/** A file's name over its folder (a file row's two lines). */
export function FileName(props: { path: string }) {
  // A new folder ("build/out/") reads "out/" in "build", not an empty name.
  const parts = splitGitReviewPath(props.path);
  return (
    <View class="min-w-0">
      <Text class="font-mono text-code font-semibold line-clamp-1">{parts.name}</Text>
      <If when={parts.directory}><Text class="font-mono text-caption text-muted-foreground line-clamp-1">{parts.directory.slice(0, -1)}</Text></If>
    </View>
  );
}

/** A small icon button with its label for screen readers. */
export function IconButton(props: { icon: IconName; label: string; onPress: () => void; disabled?: boolean; selected?: boolean; busy?: boolean; testID?: string; ref?: (node: ViewRef) => void }) {
  return (
    <Button
      ref={props.ref}
      testID={props.testID}
      accessibilityLabel={props.label}
      selected={props.selected}
      disabled={props.disabled || props.busy}
      onPress={() => props.onPress()}
      class={`size-11 items-center justify-center rounded-full hover:bg-accent active:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:opacity-40 ${props.selected ? "bg-muted" : ""}`}
    >
      <If when={props.busy} else={<Icon name={props.icon} size={20} tone="ink" />}><Spinner size={16} /></If>
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
      <If when={props.busy}><Spinner size={14} tone={props.filled ? "canvas" : "soft"} /></If>
      <Text class={`text-sub font-semibold line-clamp-1 ${props.filled ? "text-primary-foreground" : ""}`}>{props.label}</Text>
    </Button>
  );
}

/** What just happened, for a few seconds, with Undo when it can be undone. */
export function FlashLine() {
  return (
    <View>
      <If when={gitFlash()}><View testID="git-flash" class="mt-3 min-h-[52px] flex-row items-center gap-3 rounded-(--radius-group) bg-card px-4 py-3">
          <Icon name={gitFlash()!.tone === "error" ? "alert-circle" : gitFlash()!.tone === "success" ? "check-circle" : "info"} size={18} tone={gitFlash()!.tone === "error" ? "warning" : gitFlash()!.tone === "success" ? "ok" : "soft"} />
          <View class="min-w-0 flex-1">
            <Text class="text-body font-semibold">{gitFlash()!.title}</Text>
            <If when={gitFlash()!.detail}><Text class="text-sub text-muted-foreground line-clamp-3">{gitFlash()!.detail!}</Text></If>
          </View>
          <If when={gitFlash()!.action}><Button testID="git-flash-action" onPress={() => gitFlash()!.action!.run()} class="active:opacity-60">
              <Text class="text-sub font-semibold">{gitFlash()!.action!.label}</Text>
            </Button></If>
        </View></If>
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
      <If when={props.review.current()} else={<EmptyState testID="git-elsewhere" icon="server" title="Not on the current computer" detail="Switch back to the computer this Session runs on." />}>{untracked(props.children)}</If>
    </View>
  );
}
