// Brain asks the owner's 11-24 two-question card and the computer says the question passed
// (client/scripts/shots/choice-answer-kept.js, with --entry): where the typed answer goes.
// Never bundled: App.tsx is the app's entry and does not import this.
import "./seed";
import "../App";
import { installPlatform, platform } from "../../core/platform";
import { FakeDaemon } from "../testing/fakeDaemon";
import { scenario } from "./scenario";

const base = scenario();
const options = (...labels: string[]) => labels.map((label) => ({ label, description: `${label}，再说明一句` }));
const asked = {
  id: "claude-tool:toolu_1", seq: 999, timestamp: new Date().toISOString(), kind: "tool", tool_name: "AskUserQuestion", call_id: "toolu_1",
  choice: { kind: "AskUserQuestion", state: "pending", questions: [
    { header: "黄金方向", question: "黄金策略的本地复现已经和原报告对上了。接下来往哪个方向改进？", options: options("换信号源（推荐）", "做稳健性检验", "先停在这里") },
    { header: "价值因子", question: "华泰价值因子已经部分复现，成本参数解析有 bug。接下来做什么？", options: options("本地重跑并补齐（推荐）", "扩展到改进型因子", "写成研报流程") },
  ] },
};
const brainThread = (base.conversations?.["brain-host"] ?? []) as Record<string, unknown>[];
const daemon = new FakeDaemon({
  ...base,
  conversations: { ...base.conversations, "brain-host": [...brainThread, asked] },
  replies: { ...base.replies, answer_choice: { type: "error", code: "answer_choice_gone", message: "This question has already passed." } },
});
installPlatform({ ...platform(), openSocket: () => daemon.open() });
