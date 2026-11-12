import { Scroll, signal, Text, View } from "nsh";
import { GuardedSheet as Sheet } from "../../shell/AppSheet";
import type { GitBranchesPayload, GitBranchInfo } from "../../../core/git/gitDiff";
import { formatGitAge } from "../../../core/git/gitReviewTime";
import { EmptyState, Field, Notice, Row } from "../../kit/kit";
import { Icon } from "../../kit/Icon";
import { BACKDROP, SHEET, TextButton } from "./parts";
import type { GitReview } from "./state";

/** `Commit N files`: the message, then Commit, or Commit & push when the branch can sync. */
export function CommitSheet(props: { review: GitReview; open: boolean; count: number; canPush: boolean; onClose: () => void }) {
  return (
    <Sheet open={props.open} onOpenChange={({ open }) => !open && props.onClose()} closeOnSwipe popupClass={SHEET} backdropClass={BACKDROP}>
      <CommitForm review={props.review} count={props.count} canPush={props.canPush} onDone={props.onClose} />
    </Sheet>
  );
}

function CommitForm(props: { review: GitReview; count: number; canPush: boolean; onDone: () => void }) {
  const message = signal("");
  const busy = signal<"" | "commit" | "push">("");
  const error = signal("");
  const submit = async (push: boolean) => {
    if (busy() || !message().trim()) return;
    busy.set(push ? "push" : "commit");
    error.set("");
    try {
      await props.review.commit(message().trim(), push);
      props.onDone();
    } catch (reason) {
      error.set(reason instanceof Error && reason.message ? reason.message : "Could not commit.");
    } finally {
      busy.set("");
    }
  };
  return (
    <View testID="commit-sheet" class="gap-2">
      <Text class="px-1 pt-1 text-lg font-semibold" role="header">{`Commit ${props.count} ${props.count === 1 ? "file" : "files"}`}</Text>
      <View class="-mx-4">
        <Field testID="commit-message" accessibilityLabel="Commit message" placeholder="Message" multiline autoFocus disabled={busy() !== ""} value={message()} onChange={(value) => { message.set(value); error.set(""); }} />
      </View>
      {error() ? <Notice tone="failed" title="Couldn't commit" detail={error()} /> : null}
      <View class="mt-1 flex-row gap-2">
        <View class="flex-1"><TextButton testID="commit-submit" label="Commit" filled={!props.canPush} busy={busy() === "commit"} disabled={!message().trim() || busy() !== ""} onPress={() => void submit(false)} /></View>
        {props.canPush ? <View class="flex-1"><TextButton testID="commit-push" label="Commit & push" filled busy={busy() === "push"} disabled={!message().trim() || busy() !== ""} onPress={() => void submit(true)} /></View> : null}
      </View>
    </View>
  );
}

/** A branch row's detail: `on origin`, `↑a ↓b`, its last subject and age. */
export function gitBranchDetail(branch: GitBranchInfo): string {
  const sync = [branch.ahead ? `↑${branch.ahead}` : "", branch.behind ? `↓${branch.behind}` : ""].filter(Boolean).join(" ");
  return [branch.remote ? "on origin" : "", sync, branch.subject ?? "", branch.date ? formatGitAge(branch.date) : ""].filter(Boolean).join(" · ");
}

/** Find a branch to switch to, or create one; a remote branch switches to a local one tracking it. */
export function BranchSheet(props: { review: GitReview; open: boolean; onClose: () => void }) {
  return (
    <Sheet open={props.open} onOpenChange={({ open }) => !open && props.onClose()} closeOnSwipe popupClass={SHEET} backdropClass={BACKDROP}>
      <BranchPicker review={props.review} onDone={props.onClose} />
    </Sheet>
  );
}

function BranchPicker(props: { review: GitReview; onDone: () => void }) {
  const query = signal("");
  const payload = signal<GitBranchesPayload | null>(null);
  const error = signal("");
  const busy = signal("");
  props.review.branches().then(
    (value) => payload.set(value),
    (reason) => error.set(reason instanceof Error && reason.message ? reason.message : "Could not load branches."),
  );
  const run = async (branch: string, create: boolean) => {
    if (busy()) return;
    busy.set(branch);
    error.set("");
    try {
      await props.review.switchBranch(branch, create);
      props.onDone();
    } catch (reason) {
      error.set(reason instanceof Error && reason.message ? reason.message : "Could not switch branch.");
    } finally {
      busy.set("");
    }
  };
  const shown = () => {
    const needle = query().trim().toLowerCase();
    return (payload()?.branches ?? []).filter((branch) => !needle || branch.name.toLowerCase().includes(needle));
  };
  // Git refuses a branch name with spaces in it: offer Create only for one it takes.
  const creatable = () => {
    const name = query().trim();
    return !!name && !/\s/.test(name) && !(payload()?.branches ?? []).some((branch) => branch.name === name);
  };
  return (
    <View testID="branch-sheet" class="gap-2">
      <Text class="px-1 pt-1 text-lg font-semibold" role="header">Branches</Text>
      <View class="-mx-4">
        <Field testID="branch-query" accessibilityLabel="Find or create a branch" placeholder="Find or create a branch" value={query()} onChange={(value) => query.set(value)} />
      </View>
      {error() ? <Notice tone="failed" title="Couldn't switch" detail={error()} /> : null}
      {creatable() ? <TextButton testID="branch-create" label={`Create ${query().trim()}`} busy={busy() === query().trim()} onPress={() => void run(query().trim(), true)} /> : null}
      {!payload() && !error() ? <EmptyState busy title="Loading branches" /> : null}
      {payload() && !shown().length ? <EmptyState title="No matching branches" /> : null}
      <Scroll keyboardShouldPersistTaps="handled" class="max-h-[420px] overflow-hidden rounded-2xl border border-border">
        {shown().map((branch) => (
          <Row
            testID={`branch-${branch.name}`}
            leading={<Icon name="git-branch" size={18} tone={branch.current ? "ink" : "soft"} />}
            title={branch.name}
            subtitle={gitBranchDetail(branch) || undefined}
            accessibilityLabel={`${branch.name}${branch.current ? ", current" : ""}`}
            check={branch.current}
            disabled={branch.current || busy() !== ""}
            onPress={branch.current ? undefined : () => void run(branch.name, false)}
          />
        ))}
      </Scroll>
    </View>
  );
}
