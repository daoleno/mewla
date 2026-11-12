/**
 * A request's card in a thread (soft-final SPEC §1.1, §1.2, §11 Request card): the same object the
 * tray lists, drawn where it is answered. Brain's thread shows Brain's own requests and those of
 * the Sessions working for Brain's Work; a Session's thread shows its own. Pure: the screens
 * answer through `core/conversation/turn` with what a card names.
 */
import type { ConversationChoice } from "../conversation/conversation";
import { buildApprovalPrompt, buildMenuPrompt, type TerminalActionPrompt } from "../session/terminalActionPrompt";
import { promptLines } from "../sessions/activity";
import type { AgentKind, TerminalFlavor } from "../sessions/workerPresentation";
import type { Worker, WorkerPrompt } from "../sessions/workers";
import type { TrayRequest } from "../tray/model";
import { choiceFromPrompt } from "./claudeChoiceModel";

/** How a question's answers go back: Brain's chat question (`answer_choice`), or a terminal's choice prompt (`answer_prompt`). */
export type ChoiceRoute = { via: "choice"; workerId: string; callId: string } | { via: "prompt"; workerId: string; promptId: string };

export type RequestCardBody =
  /** A question: one step per question, then Submit. */
  | { kind: "choice"; choice: ConversationChoice; route: ChoiceRoute }
  /** An approval, or a menu the terminal shows: Deny and Approve, or the menu's rows. */
  | { kind: "action"; prompt: TerminalActionPrompt; workerId: string }
  /** Blocked on something the computer could not read: its only action is the terminal (SPEC §2.7). */
  | { kind: "attention"; workerId: string; detail: string };

export interface RequestCard {
  /** The tray request's key: the row and the card are one object. */
  key: string;
  /** The top line's kind: "Brain asks", "Approve", "Trust this folder?", "Needs you". */
  label: string;
  /** The question or what is asked: the stacked row and the sealed line read it. */
  title: string;
  /** Who asks, at the top line's right. */
  source: string;
  /** Brain's thread shows it (Brain's own, or a Session working for Brain's Work); a Session's thread always does. */
  inBrain: boolean;
  /** The Session the source opens; Brain's own chat question has none (it is Brain's thread). */
  sourceWorkerId?: string;
  /** Brain's own Session blocked on a prompt: the source reads "Terminal" and opens Brain's terminal. */
  terminal?: boolean;
  agent?: AgentKind;
  flavor?: TerminalFlavor;
  body: RequestCardBody;
}

/** A request answered, or passed, here: the thread keeps its quiet line (SPEC §1.2 "An answered request"). */
export interface SealedRequest {
  key: string;
  /** "Approved", "Denied", "You chose", "Answered", "Not answered". */
  verb: string;
  answer: string;
  /** A command's answer reads in mono. */
  mono?: boolean;
  title: string;
  source: string;
  /** The threads it shows in: Brain's, and the Session that asked. */
  brain: boolean;
  workerId?: string;
}

export interface RequestCardsInput {
  /** The open requests (the tray's Needs you), oldest first. */
  requests: readonly TrayRequest[];
  workers: readonly Worker[];
  brainHost?: { id: string; prompt?: WorkerPrompt } | null;
  /** Brain's pending chat question, read from its transcript. */
  brainChoice?: { callId: string; choice: ConversationChoice } | null;
  /** A Session's own pending question from its transcript, which fills a terminal choice prompt's options. */
  transcriptChoice?: { workerId: string; callId: string; choice: ConversationChoice } | null;
}

/** The thread a card list is for: Brain's, or one Session's. */
export type RequestThread = { brain: true } | { brain?: false; workerId: string };

const TRUST = /\btrust\b/i;

function promptLabel(prompt: WorkerPrompt, brain: boolean): string {
  if (brain) return "Brain asks";
  if (prompt.kind === "startup" && TRUST.test(`${prompt.title} ${prompt.request ?? ""}`)) return "Trust this folder?";
  if (prompt.kind === "approval" || prompt.kind === "permission") return "Approve";
  return "Needs you";
}

function cardOf(request: TrayRequest, input: RequestCardsInput): RequestCard | null {
  const hostId = input.brainHost?.id;
  const base = { key: request.key, title: request.title, source: request.source, inBrain: request.where === "brain", agent: request.agent, flavor: request.flavor };
  if (request.kind === "choice" && request.key.startsWith("brain-choice:")) {
    const pending = input.brainChoice;
    if (!pending || !hostId || `brain-choice:${pending.callId}` !== request.key) return null;
    return { ...base, label: "Brain asks", body: { kind: "choice", choice: pending.choice, route: { via: "choice", workerId: hostId, callId: pending.callId } } };
  }
  const workerId = request.workerId;
  if (!workerId) return null;
  const isHost = workerId === hostId;
  const worker = isHost ? undefined : input.workers.find((w) => w.id === workerId);
  const source = isHost ? { sourceWorkerId: workerId, terminal: true } : { sourceWorkerId: workerId };
  if (request.kind === "approval") {
    const approvalId = request.key.slice(`approval:${workerId}:`.length);
    const approval = worker?.approvals?.find((a) => a.id === approvalId);
    if (!approval) return null;
    return { ...base, ...source, label: "Approve", body: { kind: "action", prompt: buildApprovalPrompt(approval, workerId), workerId } };
  }
  if (request.kind === "prompt") {
    const prompt = isHost ? input.brainHost?.prompt : worker?.prompt;
    if (!prompt || request.key !== `prompt:${workerId}:${prompt.id}`) return null;
    const label = promptLabel(prompt, isHost);
    if (prompt.kind === "choice") {
      const transcript = input.transcriptChoice?.workerId === workerId ? input.transcriptChoice : null;
      const choice = choiceFromPrompt(prompt, transcript);
      // A question whose options only the transcript holds, read without it: the terminal answers it.
      if (!choice) return { ...base, ...source, label, body: { kind: "attention", workerId, detail: request.title } };
      return { ...base, ...source, label, body: { kind: "choice", choice, route: { via: "prompt", workerId, promptId: prompt.id } } };
    }
    const menu = buildMenuPrompt(prompt, workerId);
    if (!menu) return { ...base, ...source, label, body: { kind: "attention", workerId, detail: request.title } };
    return { ...base, ...source, label, body: { kind: "action", prompt: menu, workerId } };
  }
  if (request.kind === "attention") return { ...base, ...source, label: "Needs you", body: { kind: "attention", workerId, detail: request.title } };
  return null;
}

/** Whether a request shows in a thread: Brain's shows `where: "brain"`; a Session's shows its own. */
export function requestInThread(request: TrayRequest, thread: RequestThread): boolean {
  if (thread.brain) return request.where === "brain";
  return request.workerId === thread.workerId && (request.where === "session" || request.where === "brain");
}

/** The open request cards of one thread, oldest first; a request the app cannot draw yet is left to the tray. */
export function requestCardsOf(input: RequestCardsInput, thread: RequestThread): RequestCard[] {
  const out: RequestCard[] = [];
  for (const request of input.requests) {
    if (!requestInThread(request, thread)) continue;
    const card = cardOf(request, input);
    if (card) out.push(card);
  }
  return out;
}

/** The open card of a stack: the one the person picked while it is open, else the oldest. */
export function openCardKey(cards: readonly RequestCard[], picked: string | null | undefined): string {
  if (picked && cards.some((card) => card.key === picked)) return picked;
  return cards[0]?.key ?? "";
}

/** A line that does not start like a sentence (a capital, then a lowercase letter): a command, a path. */
export const looksLikeCommand = (line: string) => Boolean(line) && !/^[A-Z][a-z]/.test(line);

/**
 * A terminal prompt's request as a card shows it: the question (its last line that asks), the command, and the
 * agent's description. Claude prints the description above or below the command, so the command is the first line
 * that does not start like a sentence (a capital and a lowercase letter), else the first line. The agent's
 * "Tip:" advert and "This command requires approval" are dropped: the card already says Approve. The daemon
 * should send these as fields (its prompt parser has them as lines only).
 */
export function promptParts(text: string, known?: string): { question: string; command: string; notes: string } {
  const lines = promptLines(text);
  const question = [...lines].reverse().find((line) => line.endsWith("?")) ?? "";
  const rest = lines.filter((line) => line !== question);
  if (known) {
    // The daemon named the call: it is the command, and the lines that print it ("Read(/etc/hostname)", a
    // wrapped command's parts) go, compared without whitespace; the rest is the description.
    const bare = (line: string) => line.replace(/\s+/g, "");
    const call = bare(known);
    return { question, command: known, notes: rest.filter((line) => !bare(line).includes(call) && !call.includes(bare(line))).join(" ") };
  }
  const command = rest.find(looksLikeCommand) ?? rest[0] ?? "";
  return { question, command, notes: rest.filter((line) => line !== command).join(" ") };
}

/** A sealed line's words for an action card's answer. */
export function sealAction(card: RequestCard, optionLabel: string, deny: boolean): Pick<SealedRequest, "verb" | "answer" | "mono"> {
  if (card.body.kind !== "action") return { verb: "Answered", answer: optionLabel };
  const command = promptParts(card.body.prompt.requestText ?? "", card.body.prompt.commandText).command;
  if (card.label === "Approve") return deny ? { verb: "Denied", answer: command || optionLabel, mono: Boolean(command) } : { verb: "Approved", answer: command || optionLabel, mono: Boolean(command) };
  return { verb: "You chose", answer: optionLabel };
}

/** Whether a sealed line shows in a thread. */
export function sealedInThread(sealed: SealedRequest, thread: RequestThread): boolean {
  return thread.brain ? sealed.brain : sealed.workerId === thread.workerId;
}

/**
 * An option row's words (SPEC §11 Request card, "A row"): the label is the text up to the first " · ", the rest
 * (else the option's description) the soft line under it. The agent's own numbering ("2. ") and a trailing "›"
 * are its terminal's, not the option's.
 */
export function optionParts(label: string, description?: string): { label: string; detail: string } {
  const bare = label.replace(/^\s*\d+[.)]\s+/, "").replace(/\s*›\s*$/, "").trim();
  const at = bare.indexOf(" · ");
  if (at < 0) return { label: bare, detail: description?.trim() ?? "" };
  return { label: bare.slice(0, at).trim(), detail: bare.slice(at + 3).trim() || description?.trim() || "" };
}
