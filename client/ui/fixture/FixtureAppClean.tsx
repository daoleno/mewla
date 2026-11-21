// The fixture app with a clean repository (no change to review): the Git button's other state, for `shots.sh --entry`.
import "./seed";
import { host } from "nsh";
import "../App";
import { installPlatform, platform } from "../../core/platform";
import { FakeDaemon } from "../testing/fakeDaemon";
import { scenario } from "./scenario";

const base = scenario();
const clean = { type: "git_diff_status", status: { available: true, repo_root: "/home/me/work/atlas-api", repo_name: "atlas-api", branch: "main", has_commits: true, clean: true, file_count: 0, staged_file_count: 0, unstaged_file_count: 0, untracked_file_count: 0, additions: 0, deletions: 0, files: [] } };
const daemon = new FakeDaemon({ ...base, replies: { ...base.replies, git_diff_status: clean }, admitsInput: { reply: "On it." } });
installPlatform({ ...platform(), openSocket: () => daemon.open() });
host.on<Record<string, unknown>>("app.daemonPush", (frame) => daemon.push(frame as { type: string }));
