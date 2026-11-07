import { useCallback, useRef } from "react";
import * as Clipboard from "expo-clipboard";
import type { GitDiffPatchPayload, GitDiffScope } from "../../services/gitDiff";
import { useToast } from "../ui/Toast";
import { gitDiffPatchText } from "./gitDiffReview";

/** Clipboard actions with Toast feedback (Toast also owns the haptic). */
export function useGitDiffClipboard(
  loadPatch: (path: string) => Promise<GitDiffPatchPayload>,
) {
  const toast = useToast();
  const copyingPatch = useRef(false);
  const copyText = useCallback(
    async (text: string, title: string, detail?: string) => {
      try {
        await Clipboard.setStringAsync(text);
        toast.show({ title, detail, tone: "success" });
      } catch (error) {
        toast.show({
          title: "Couldn't copy",
          detail: error instanceof Error ? error.message : undefined,
          tone: "error",
        });
      }
    },
    [toast],
  );
  const copyPath = useCallback(
    (path: string) => copyText(path, "Path copied", path),
    [copyText],
  );
  const copyPatch = useCallback(
    async (path: string, scope: GitDiffScope) => {
      if (copyingPatch.current) return;
      copyingPatch.current = true;
      try {
        const text = gitDiffPatchText(await loadPatch(path), scope);
        if (!text) {
          toast.show({ tone: "error", title: "Nothing to copy" });
          return;
        }
        const lines = text.split("\n").length - 1;
        await copyText(text, "Diff copied", `${lines} ${lines === 1 ? "line" : "lines"}`);
      } catch (error) {
        toast.show({
          title: "Couldn't copy diff",
          detail: error instanceof Error ? error.message : undefined,
          tone: "error",
        });
      } finally {
        copyingPatch.current = false;
      }
    },
    [copyText, loadPatch, toast],
  );
  return { copyText, copyPath, copyPatch };
}
