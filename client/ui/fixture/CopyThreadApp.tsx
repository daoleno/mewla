// The app on Brain's thread made for copying text out of a chat (fixture/copyThread.ts), on a device:
//   nsh dev --release --server-only --port <port> ui/fixture/CopyThreadApp.tsx   (from client/), then the dev start link.
// A reply streams in, word by word, over a minute and a half after the app starts. Never bundled.
import "./seed";
import { host } from "nsh";
import "../App";
import { installPlatform, platform } from "../../core/platform";
import { FakeDaemon } from "../testing/fakeDaemon";
import { scenario } from "./scenario";
import { copyThread, STREAM_LENGTH, streamingReply } from "./copyThread";

const base = scenario();
const daemon = new FakeDaemon({ ...base, conversations: { ...base.conversations, "brain-host": [...(base.conversations?.["brain-host"] as Record<string, unknown>[]), ...copyThread()] }, admitsInput: { reply: "On it." } });
installPlatform({ ...platform(), openSocket: () => daemon.open() });
host.on<Record<string, unknown>>("app.daemonPush", (frame) => daemon.push(frame as { type: string }));

let words = 0;
setTimeout(function stream() {
  words++;
  daemon.conversation({ worker: "brain-host", upserts: [streamingReply(words)] });
  if (words < STREAM_LENGTH) setTimeout(stream, 1500);
}, 8000);
