import { effect, untracked } from "nsh";
import { linkPathText } from "../../core/link/directAddresses";
import { pairWith, readPairingLink } from "../../core/pairFlow";
import { addPairedServer, connection, current, currentTransport, transportPath } from "../session";
import { nav } from "../shell/nav";

/**
 * Mewla Dev's Mewla Relay proof (devScript.relay, macos-app.yml `relay-lan`; owner 11-23: did the desktop app ever
 * find the LAN on Mewla Relay?): pairs the Relay code as an opened link would (its confirmation is proved by the
 * links step), opens the computer's Settings page, whose Mewla Relay row shows the path, and logs each path the
 * connection is on. On each one it sends a reminder, again once the connection is back if it dropped under it, as
 * the outbox does: the run counts them on the computer, each exactly once.
 */
export async function relayProof(link: string): Promise<void> {
  const payload = readPairingLink(link);
  if (!payload) return console.log("dev script: relay: not a pairing link");
  console.log("dev script: relay pairing");
  try {
    await addPairedServer(await pairWith(payload));
  } catch (error) {
    return console.log(`dev script: relay pairing failed: ${error instanceof Error ? error.message : error}`);
  }
  const id = current()?.id;
  if (!id) return;
  nav.push({ name: "settings-computer", id });
  let told = "";
  let sentOn = "";
  effect(() => {
    const path = transportPath();
    const state = connection();
    const words = state === "connected" ? linkPathText(path) : state;
    untracked(() => {
      if (words === told) return;
      told = words;
      console.log(`dev script: relay path ${words}`);
      if (state !== "connected" || !path || path.path === sentOn) return;
      sentOn = path.path;
      void remind(path.path);
    });
  });
}

async function remind(path: string): Promise<void> {
  const title = `relay proof on ${path} ${Date.now()}`;
  const ask = () =>
    currentTransport()!.request("create_calendar_item", {
      calendar_item: { title, kind: "reminder", timezone: "UTC", recurrence: null, notify_at: new Date(Date.now() + 86_400_000).toISOString() },
    }, { reply: "calendar_item_created", timeoutMs: 20_000 });
  for (let attempt = 1; attempt <= 2; attempt++) {
    try {
      await ask();
      return console.log(`dev script: relay reminder "${title}" created`);
    } catch (error) {
      console.log(`dev script: relay reminder "${title}" refused (${error instanceof Error ? error.message : error})`);
      await new Promise<void>((resolve) => {
        const check = setInterval(() => {
          if (connection() === "connected") {
            clearInterval(check);
            resolve();
          }
        }, 200);
      });
    }
  }
}
