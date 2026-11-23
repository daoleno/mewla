/**
 * Brain's thread for copying text out of a chat (fixture/CopyThreadApp.tsx): the kinds of text a reply carries
 * (headings, paragraphs, a numbered list, inline code, a code block wider than a phone, a table, a link, Chinese and
 * English mixed), a long message of the person's own that folds, and a reply that streams in above it.
 */
let seq = 9000;
const MIN = 60_000;
const at = (minutesAgo: number) => new Date(Date.now() - minutesAgo * MIN).toISOString();
const ev = (e: Record<string, unknown>, minutesAgo: number) => ({ id: `copy${++seq}`, seq, timestamp: at(minutesAgo), ...e });

export const LONG_MINE = [
  "Before tonight's deploy, three things:",
  "1. the rate-limit window in atlas-api still drops the last request of each minute;",
  "2. the pricing table must show the annual price under the monthly one;",
  "3. the docs quickstart still says `npm start`, it is `bun run dev` now.",
  "",
  "Also check the staging logs for the 502s we saw at 13:40 and tell me whether it is the proxy or the app.",
  "If it is the proxy, roll back to the previous config (it is in ops/proxy/previous.yaml) and keep the new one aside.",
  "And please write the summary in both English and 中文, the team in Shenzhen reads it first thing.",
].join("\n");

export const LONG_REPLY = [
  "## Deploy check",
  "",
  "The rate-limit fix is in. The window was counted from the first request, so the last request of each minute fell into the next window. 窗口现在按整分钟计算，测试全部通过。",
  "",
  "### What changed",
  "",
  "1. `SlidingWindow.allow()` counts from the minute's start.",
  "2. The test `window_edges_test.go` covers 59.999 s and 60.000 s.",
  "3. 文档里的启动命令改成了 `bun run dev`。",
  "",
  "Run it yourself:",
  "",
  "```bash",
  "cd ~/work/atlas-api && go test ./ratelimit/... -run TestWindowEdges -count=5 -v 2>&1 | tee /tmp/window-edges.log",
  "    grep -c PASS /tmp/window-edges.log",
  "```",
  "",
  "| Check | Before | After |",
  "| --- | --- | --- |",
  "| Requests kept | 59 / 60 | 60 / 60 |",
  "| p95 latency | 41 ms | 39 ms |",
  "| 502s (13:40) | 17 | 0 |",
  "",
  "The 502s were the proxy, not the app: see [the proxy log](https://example.com/logs/proxy-1340) for 13:40. I rolled back to `ops/proxy/previous.yaml` and kept the new config aside. 👍",
].join("\n");

const STREAM_WORDS = "I am reading the staging logs now. The first 502 came at 13:40:07 from the proxy's upstream pool, then sixteen more within forty seconds while the app's own log shows every request answered in under 50 ms. 代理的上游连接池在那段时间被占满了。 I will check the pool size next and compare it with yesterday's config before I change anything.".split(" ");

/**
 * The thread, newest last: the long reply is the newest message, so the thread opens on it (its end on screen, the
 * same place every time); the streaming reply grows above it.
 */
export function copyThread(): Record<string, unknown>[] {
  return [
    ev({ kind: "user_message", role: "user", body: LONG_MINE }, 12),
    ev({ kind: "user_message", role: "user", body: "Thanks. What about the 502s, is staging fine now? 现在还有错误吗？" }, 3),
    { id: "copy-reply", seq: 10_000, timestamp: at(0.2), kind: "assistant_message", role: "assistant", body: LONG_REPLY },
  ];
}

/** The streaming reply after `words` of its words: partial until the last. */
export function streamingReply(words: number): Record<string, unknown> {
  const done = words >= STREAM_WORDS.length;
  return { id: "copy-stream", seq: 9999, timestamp: at(0.5), kind: "assistant_message", role: "assistant", body: STREAM_WORDS.slice(0, words).join(" "), partial: !done };
}
export const STREAM_LENGTH = STREAM_WORDS.length;
