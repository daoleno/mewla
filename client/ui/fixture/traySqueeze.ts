// The tray's counts under a squeeze (TraySqueezeApp for the web, MacFixtureApp's `traySqueeze` dev script for the Mac's
// captures): the Sessions and Brain's Work with that many needing you, new and running, in several projects, one with
// a long name. Never imported by the app.
import { Storage } from "nsh";
import { scenario } from "./scenario";

export type SqueezeCounts = { needs: number; fresh: number; running: number };
/** The owner's capture (11-23), and every count at once with two digits. */
export const SQUEEZE_SETS: Record<string, SqueezeCounts> = { owner: { needs: 1, fresh: 1, running: 3 }, busy: { needs: 12, fresh: 14, running: 17 } };

const PROJECTS = ["data-platform", "mewla-computer-use-and-screen-sharing-for-everyone", "0G app store", "atlas-api", "landing-site"];
const agoIso = (ms: number) => new Date(Date.now() - ms).toISOString();

/** The wide pane's width: 280 on the web, 240 on the Mac, the least each handle gives (core/web/places.ts). */
export function setPaneWidth(width: number): void {
  void Storage.setItem("mewla.wide.paneWidth", String(width));
}

/** The scenario's Sessions and Brain's Work with `counts` in place of its own. */
export function squeezeState({ needs, fresh, running }: SqueezeCounts) {
  const base = scenario();
  const sessions: Record<string, unknown>[] = [];
  const work: Record<string, unknown>[] = [];
  const common = { delegated: true, summary: "", last_output_lines: [], command: "claude", provider: "claude", started_at: agoIso(3_600_000), updated_at: agoIso(60_000) };
  for (let i = 0; i < needs; i++) {
    sessions.push({ ...common, id: `n${i}`, name: `Needs ${i + 1}`, status: "blocked", project: PROJECTS[i % PROJECTS.length], cwd: "/home/me/work/x",
      prompt: { id: `q${i}`, kind: "startup", provider: "claude", title: "Trust this folder?", request: "~/work/x", options: [{ number: 1, label: "Yes, trust this folder" }, { number: 2, label: "No, exit", selected: true }] } });
  }
  for (let i = 0; i < running; i++) {
    const project = PROJECTS[i % PROJECTS.length]!;
    sessions.push({ ...common, id: `r${i}`, name: `Running ${i + 1}`, status: "running", project, cwd: "/home/me/work/x", activity: "Working", running_since: agoIso((i + 2) * 60_000) });
    work.push({ work_id: `wr${i}`, revision: 1, title: `Running ${i + 1}`, status: "running", unread_result: false, attempt_session_id: `r${i}`, attempt_delegated: true,
      shown: { title: `Running ${i + 1}`, line: "Working", project, changed_at: agoIso(60_000) } });
  }
  for (let i = 0; i < fresh; i++) {
    work.push({ work_id: `wf${i}`, revision: 1, title: `Finished ${i + 1}`, status: "done", unread_result: true,
      shown: { title: `Finished ${i + 1}`, line: "Merged", project: PROJECTS[i % PROJECTS.length], changed_at: agoIso(120_000) } });
  }
  return { ...base, worker_sessions: sessions, brain: { ...base.brain, current_work: work, open_work: [] } };
}
