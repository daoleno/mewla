/**
 * A tool call as a Ledger step (owner 11-27, variant A): the agent's own words for it, else a title made from what the
 * daemon read it as (kind + target, never a bare verb), the target in mono, and on the right what came back and how
 * long it took. Every agent's calls arrive already read (`event.tool`, daemon/work/conversation_tool.go), so nothing
 * here knows one agent from another.
 */
import type { CodexConversationEvent, ConversationTool } from "../conversation/conversation";
import type { ActivityTimelineItem, LedgerFacts } from "./activityTypes";
import type { SemanticActionKind } from "./toolCallSemantics";
import { distinctivePath } from "./toolCallSemantics";

/** The lines of a failed call's output its open step shows: the end, where the error is. */
const FAILED_OUTPUT_TAIL_LINES = 6;
const OUTPUT_PREVIEW_LINES = 12;
const TITLE_TARGET_LIMIT = 72;

export function hasToolStep(event: CodexConversationEvent): boolean {
  return Boolean(event.tool) && !event.choice;
}

/** A finished wait (Sleep, polling a background command) says nothing the next step does not: no row. */
export function isNoiseToolStep(tool: ConversationTool): boolean {
  return tool.kind === "wait" && tool.status !== "running" && tool.status !== "failed";
}

/** The Ledger step for a tool call, or null for one that is noise. */
export function toolStepActivity(event: CodexConversationEvent): ActivityTimelineItem | null {
  const tool = event.tool!;
  if (isNoiseToolStep(tool)) return null;
  const running = tool.status === "running";
  const failed = tool.status === "failed" || tool.status === "refused";
  const title = toolStepTitle(tool);
  const shownTarget = stepTarget(tool, title);
  const ledger: LedgerFacts = {
    target: shownTarget,
    result: tool.status === "refused" ? "refused" : tool.status === "interrupted" ? "stopped" : tool.result,
    added: tool.added,
    removed: tool.removed,
    durationMs: tool.duration_ms,
    more: tool.more,
  };
  // What came back is worth showing for a command, a search or a fetch, or any call that failed; a read's is the file.
  const showsOutput = failed || tool.kind === "command" || tool.kind === "search" || tool.kind === "fetch" || tool.kind === "web_search" || tool.kind === "other" || tool.kind === "browser";
  const output = !showsOutput ? "" : cleanOutput(event.output || (event.kind === "command" && event.body !== event.command && event.source !== "claude_code_transcript" ? event.body : "") || "").trim();
  const command = tool.kind === "command" ? (event.command || tool.target || "").trim() : "";
  const files = filesOf(tool, event);
  return {
    type: "activity",
    id: event.id || `tool:${event.seq}`,
    timestamp: event.timestamp,
    statusKey: running ? "running" : failed ? "failed" : "done",
    title,
    tone: running ? "running" : failed ? "failed" : "success",
    icon: running ? "time" : failed ? "alert-circle" : TOOL_ICONS[tool.kind],
    commandText: command || undefined,
    body: output ? (failed ? tail(output, FAILED_OUTPUT_TAIL_LINES) : head(output, OUTPUT_PREVIEW_LINES)) : undefined,
    bodyKind: output ? "terminal" : undefined,
    files: files.length ? files : undefined,
    // A failed step starts closed like any other; its row and its run header say it failed (owner 11-28).
    defaultExpanded: false,
    semanticKind: SEMANTIC_KIND[tool.kind],
    accessibilityLabel: [title, shownTarget, ledger.result, failed ? "failed" : ""].filter(Boolean).join(", "),
    providerToolId: event.tool_name || undefined,
    ledger,
  };
}

const SEMANTIC_KIND: Record<ConversationTool["kind"], SemanticActionKind> = {
  command: "run_command",
  read: "read_files",
  list: "read_files",
  search: "search_code",
  edit: "update_files",
  fetch: "use_tool",
  web_search: "use_tool",
  agent: "delegate_agent",
  wait: "wait",
  plan: "update_plan",
  image: "view_image",
  browser: "test_app",
  other: "use_tool",
};

const TOOL_ICONS: Record<ConversationTool["kind"], string> = {
  command: "terminal",
  read: "document-text",
  list: "folder-open",
  search: "search",
  edit: "edit",
  fetch: "link",
  web_search: "navigate",
  agent: "git-branch",
  wait: "time",
  plan: "list",
  image: "image",
  browser: "desktop",
  other: "construct",
};

function filesOf(tool: ConversationTool, event: CodexConversationEvent): string[] {
  if (tool.kind !== "read" && tool.kind !== "edit" && tool.kind !== "image") return [];
  if (event.file_changes?.length) return event.file_changes.map((change) => change.path);
  if (event.files?.length) return event.files;
  return tool.target && !tool.target.includes(", ") ? [tool.target] : [];
}

/** A step's title: the agent's own words, else what it did to what (never a bare verb). */
export function toolStepTitle(tool: ConversationTool): string {
  if (tool.intent) return tool.intent;
  const target = tool.target?.trim() ?? "";
  switch (tool.kind) {
    case "command":
      return commandTitle(target);
    case "read":
      return target ? `Read ${shortPath(target)}` : "Read a file";
    case "list":
      return target && target !== "." ? `List ${shortPath(target)}` : "List the folder";
    case "search":
      return target ? `Search ${quoted(target)}${tool.scope ? ` in ${shortPath(tool.scope)}` : ""}` : "Search the code";
    case "edit":
      return target ? `Edit ${shortPath(target)}` : "Edit a file";
    case "fetch":
      return target ? `Fetch ${urlLabel(target)}` : "Fetch a page";
    case "web_search":
      return target ? `Search the web for ${quoted(target)}` : "Search the web";
    case "agent":
      return target ? `Subagent: ${clip(target)}` : "Run a subagent";
    case "wait":
      return "Wait";
    case "plan":
      return "Update the plan";
    case "image":
      return target ? `View ${shortPath(target)}` : "View an image";
    case "browser":
      return [tool.name || "Use the browser", target ? clip(target, 48) : ""].filter(Boolean).join(" ");
    default:
      return [tool.name || "Use a tool", target ? clip(target, 48) : ""].filter(Boolean).join(" ");
  }
}

/** The mono target beside a title: the command or path when the title does not already say it whole. */
function stepTarget(tool: ConversationTool, title: string): string | undefined {
  const target = tool.target?.trim();
  if (!target) return undefined;
  if (tool.kind === "command") return target;
  // The agent's own words leave out what it worked on; a path's title names it from its project down already.
  if (tool.intent) return tool.kind === "search" ? quoted(target) : shortPath(target);
  return undefined;
}

function quoted(value: string) {
  return `“${clip(value, 48)}”`;
}

function clip(value: string, limit = TITLE_TARGET_LIMIT) {
  const chars = Array.from(value.replace(/\s+/g, " ").trim());
  return chars.length > limit ? `${chars.slice(0, limit - 1).join("")}…` : chars.join("");
}

/** A path as a title names it: from its project down, else its last two parts ("worklog/RESULT.md"). */
export function shortPath(path: string): string {
  if (path.includes(", ")) {
    const parts = path.split(", ");
    return parts.length > 2 ? `${shortPath(parts[0])} + ${parts.length - 1}` : parts.map(shortPath).join(", ");
  }
  const distinctive = distinctivePath(path);
  if (distinctive) return clip(distinctive, 56);
  const parts = path.split("/").filter(Boolean);
  return clip(parts.length > 2 ? parts.slice(-2).join("/") : path, 56);
}

function urlLabel(url: string): string {
  try {
    const parsed = new URL(url);
    return clip(parsed.host + (parsed.pathname === "/" ? "" : parsed.pathname), 56);
  } catch {
    return clip(url, 56);
  }
}

// --- Commands ----------------------------------------------------------------------------------------------------

/** A command's title: what the command is for when its program says so ("Check run 45768…"), else "Run <program>". */
export function commandTitle(command: string): string {
  const words = commandWords(command);
  if (!words.length) return "Run a command";
  const [program, sub, third] = [baseName(words[0]), words[1] ?? "", words[2] ?? ""];
  const arg = (index: number) => words.slice(index).find((word) => !word.startsWith("-")) ?? "";
  switch (program) {
    case "gh":
      if (sub === "run") {
        const runId = words.slice(3).find((word) => /^\d+$/.test(word)) ?? "";
        if (third === "view") return runId ? `Check run ${runId}` : "Check a run";
        if (third === "watch") return runId ? `Watch run ${runId}` : "Watch a run";
        if (third === "list") return "List runs";
        if (third === "rerun") return runId ? `Rerun run ${runId}` : "Rerun a run";
        if (third === "cancel") return runId ? `Cancel run ${runId}` : "Cancel a run";
      }
      if (sub === "pr") {
        const number = arg(3);
        const label: Record<string, string> = { view: "View PR", create: "Open a PR", merge: "Merge PR", checks: "Check PR", list: "List PRs", diff: "Diff PR", comment: "Comment on PR", edit: "Edit PR" };
        if (label[third]) return /^\d+$/.test(number) ? `${label[third]} #${number}` : label[third];
      }
      if (sub === "workflow" && third === "run") return `Start ${arg(3) || "a workflow"}`;
      if (sub === "release") return `${third === "create" ? "Create" : third === "view" ? "View" : "Use"} release ${arg(3)}`.trim();
      if (sub === "api") return "Call the GitHub API";
      return `Run gh ${sub}`.trim();
    case "git": {
      const verbs: Record<string, string> = {
        status: "Git status", diff: "Git diff", log: "Git log", show: "Git show", commit: "Commit", push: "Push", fetch: "Fetch",
        pull: "Pull", add: "Stage files", checkout: "Check out", switch: "Switch branch", rebase: "Rebase", merge: "Merge", worktree: "Worktree",
        tag: "Tag", stash: "Stash", branch: "Branches", reset: "Reset", clone: "Clone", "rev-parse": "Resolve a revision", blame: "Git blame", grep: "Git grep",
        "cherry-pick": "Cherry-pick", restore: "Restore files", rm: "Remove files", mv: "Move files", config: "Git config", remote: "Git remote",
      };
      return verbs[sub] ?? `Run git ${sub}`.trim();
    }
    case "go":
      if (sub === "test") return `Test ${goPackages(words.slice(2))}`;
      if (sub === "build") return `Build ${goPackages(words.slice(2))}`;
      if (sub === "vet") return `Vet ${goPackages(words.slice(2))}`;
      if (sub === "run") return `Run ${arg(2) || "go"}`;
      return `Run go ${sub}`.trim();
    case "bun":
    case "npm":
    case "pnpm":
    case "yarn":
      if (sub === "test") return `Test ${arg(2) || "the project"}`;
      if (sub === "install" || sub === "i" || sub === "add") return "Install packages";
      if (sub === "run") return `Run ${arg(2)}`.trim();
      if (sub === "x" || sub === "exec") return `Run ${arg(2)}`.trim();
      return `Run ${program} ${sub}`.trim();
    case "nsh":
      if (sub === "test") return `Test ${arg(2) || "the app"}`;
      if (sub === "check") return "Check the app";
      return `Run nsh ${sub}`.trim();
    case "pytest":
    case "jest":
    case "vitest":
      return `Test ${arg(1) || "the project"}`;
    case "cargo":
      return sub === "test" ? "Test the crate" : sub === "build" ? "Build the crate" : `Run cargo ${sub}`.trim();
    case "make":
      return `Make ${arg(1) || "the default target"}`;
    case "curl":
    case "wget": {
      const url = words.find((word) => /^https?:\/\//.test(word));
      return url ? `Fetch ${urlLabel(url)}` : `Run ${program}`;
    }
    case "ssh": {
      const host = sshHost(words);
      return host ? `Run on ${host}` : "Run over SSH";
    }
    case "scp":
    case "rsync":
      return "Copy files";
    case "mkdir":
      return arg(1) ? `Create folder ${shortPath(arg(1))}` : "Create a folder";
    // A shell keyword is no program: the command is a loop or a script of its own.
    case "for":
    case "while":
    case "until":
      return "Run a shell loop";
    case "if":
    case "case":
    case "{":
    case "(":
      return "Run a shell script";
    case "rm":
      return `Remove ${shortPath(arg(1)) || "files"}`;
    case "cp":
      return `Copy ${shortPath(arg(1)) || "files"}`;
    case "mv":
      return `Move ${shortPath(arg(1)) || "files"}`;
    case "chmod":
      return `Change mode of ${shortPath(arg(2)) || "files"}`;
    case "touch":
      return `Create ${shortPath(arg(1))}`;
    case "kill":
    case "pkill":
      return "Stop a process";
    case "ps":
    case "pgrep":
      return "List processes";
    case "sleep":
      return `Wait ${arg(1)}s`.replace(/ss$/, "s");
    case "echo":
    case "printf":
      return "Print text";
    case "date":
      return "Read the date";
    case "docker":
      return `Docker ${sub}`.trim();
    case "systemctl":
      return `${capitalize(arg(words[1] === "--user" ? 2 : 1) || "check")} a service`;
    case "tmux": {
      const rest = words.slice(1);
      for (let index = 0; index < rest.length; index += 1) {
        if (/^-[SLf]$/.test(rest[index])) index += 1;
        else if (!rest[index].startsWith("-")) return `tmux ${rest[index]}`;
      }
      return "Run tmux";
    }
    case "jq":
      return "Query JSON";
    case "python":
    case "python3":
    case "node":
    case "bash":
    case "sh":
    case "zsh":
      if (sub === "-c" || sub === "-e" || sub === "-" || sub === "-lc" || sub.startsWith("--input")) return `Run a ${program.startsWith("python") ? "Python" : program === "node" ? "Node" : "shell"} script`;
      return sub ? `Run ${shortPath(sub)}` : `Run ${program}`;
    case "mewla":
    case "mewla-dev":
      return mewlaTitle(words.slice(1));
  }
  if (words[0].includes("PROGRESS_CMD")) return "Report progress";
  const files = words.slice(1).filter((word) => !word.startsWith("-") && !/^\d/.test(word) && !word.includes(">"));
  if (["cat", "head", "tail", "less", "nl", "bat"].includes(program) && files.length) return `Read ${shortPath(files[0])}${files.length > 1 ? ` + ${files.length - 1}` : ""}`;
  if (program === "sed" && words.includes("-n") && files.length) return `Read ${shortPath(files[files.length - 1])}`;
  if (["rg", "grep", "ag", "ack"].includes(program) && files.length) return `Search ${quoted(files[0])}`;
  if (["ls", "tree", "find", "fd"].includes(program)) return files.length ? `List ${shortPath(files[0])}` : "List the folder";
  return `Run ${clip(program, 32)}`;
}

function mewlaTitle(args: string[]): string {
  const [group, action] = [args[0] ?? "", args[1] ?? ""];
  if (group === "worker") {
    const label: Record<string, string> = { send: "Message a Worker", spawn: "Start a Worker", capture: "Read a Worker's screen", status: "Check a Worker", progress: "Report progress", list: "List Workers", close: "Close a Worker", answer: "Answer a Worker", receipt: "Check a Worker's receipt" };
    return label[action] ?? `Mewla worker ${action}`.trim();
  }
  if (group === "brain") return `Brain ${action || "context"}`.trim();
  return `Mewla ${group}`.trim();
}

function sshHost(words: string[]): string {
  for (let index = 1; index < words.length; index += 1) {
    const word = words[index];
    if (word.startsWith("-")) {
      if (/^-[oiplFJ]$/.test(word)) index += 1;
      continue;
    }
    return word.split("@").pop() ?? word;
  }
  return "";
}

function goPackages(args: string[]): string {
  const packages = args.filter((word) => word.startsWith("./") || word.startsWith("github.com/"));
  if (!packages.length) return "the package";
  return packages.length > 1 ? `${packages[0]} + ${packages.length - 1}` : packages[0];
}

function capitalize(value: string) {
  return value ? value[0].toUpperCase() + value.slice(1) : value;
}

function baseName(word: string) {
  return word.split("/").pop() ?? word;
}

/** A command line's words up to its first ;, &&, || or |, past VAR=x, sudo, timeout 30 and the like; quotes removed. */
function commandWords(command: string): string[] {
  const words: string[] = [];
  let word = "";
  let quote = "";
  let inWord = false;
  for (let index = 0; index < command.length; index += 1) {
    const char = command[index];
    if (quote) {
      if (char === quote) quote = "";
      else word += char;
      continue;
    }
    if (char === "'" || char === '"') {
      quote = char;
      inWord = true;
      continue;
    }
    if (char === ";" || char === "|" || char === "&" || char === "\n") break;
    if (char === " " || char === "\t") {
      if (inWord) words.push(word);
      word = "";
      inWord = false;
      continue;
    }
    word += char;
    inWord = true;
  }
  if (inWord) words.push(word);
  while (words.length) {
    const head = words[0];
    if (/^[A-Za-z_][A-Za-z0-9_]*=/.test(head) || ["sudo", "env", "time", "command", "exec", "nohup", "setsid"].includes(head)) words.shift();
    else if (head === "timeout" && words.length > 1) words.splice(0, 2);
    else if (head === "nice" && words[1] === "-n") words.splice(0, 3);
    else break;
  }
  return words;
}

/** A call's output without its runner's framing (Codex: "Script completed", "Wall time 1.0 seconds", "Output:"). */
function cleanOutput(text: string): string {
  const lines = text.split("\n");
  while (lines.length && /^(Script completed|Script failed|Wall time[: ].*|Output:|Chunk ID:.*|Process exited with code \d+|Original token count:.*|Total output lines:.*|)$/.test(lines[0].trim())) lines.shift();
  return lines.join("\n").trim();
}

function head(text: string, lines: number): string {
  const all = text.split("\n");
  return all.length > lines ? `${all.slice(0, lines).join("\n")}\n…` : text;
}

function tail(text: string, lines: number): string {
  const all = text.split("\n");
  return all.length > lines ? `…\n${all.slice(-lines).join("\n")}` : text;
}
