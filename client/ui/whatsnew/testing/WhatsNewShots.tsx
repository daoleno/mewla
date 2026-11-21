// What's New on the fixture computer, for client/scripts/shots/whatsnew.js (`shots.sh --entry`). Once connected it
// opens the newest notes' sheet with every page, as Android shows them (a browser is not offered a page of
// Android's only: this draws it anyway, for the look). Brain's chat carries 0.4.19's "Updated to" line, whose
// What's new opens its nine fixes. Never bundled.
import "../../fixture/seed";
import { effect, host, untracked } from "nsh";
import "../../App";
import { installPlatform, platform } from "../../../core/platform";
import { parseReleases, releasePages } from "../../../core/whatsnew/whatsNew";
import { FakeDaemon } from "../../testing/fakeDaemon";
import { scenario } from "../../fixture/scenario";
import { connection } from "../../session";
import notes from "../notes.json";
import { sheet } from "../state";

const daemon = new FakeDaemon({ ...scenario(), whats_new: { baseline: "", seen: [], updates: [{ version: "0.4.19", at: Date.now() - 60_000 }] } });
installPlatform({ ...platform(), openSocket: () => daemon.open() });
host.on<Record<string, unknown>>("app.daemonPush", (frame) => daemon.push(frame as { type: string }));

const newest = parseReleases(notes)[0];
let opened = false;
effect(() => {
  if (connection() !== "connected" || opened) return;
  opened = true;
  setTimeout(() => untracked(() => sheet.set({ version: newest.version, pages: releasePages(newest), fixes: [] })), 1_500);
});
