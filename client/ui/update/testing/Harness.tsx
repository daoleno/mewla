import { host, mount, signal, Text, View } from "nsh";
import type { AppUpdateState } from "../../../core/update/appUpdate";
import nacl from "../../../core/vendor/nacl";
import { Section } from "../../kit/kit";
import { AboutUpdateRow } from "../AboutUpdateRow";
import { type AppUpdate, createAppUpdate } from "../update";

/**
 * The About update row on its own, for `nsh test`, on the app's real `update.*` host
 * calls. The test plays the release server as the host call `test.fetch` {url} →
 * base64 (a test network stub carries no binary signature), signed with the key of
 * seed 3 (32 bytes of 3) in place of the release key. With `host.params.state` the
 * row shows that state instead (the event `test.state` replaces it) (a step cannot run while a download's host call
 * is open), and a press posts `test.pressed`.
 */
const keys = nacl.sign.keyPair.fromSeed(new Uint8Array(32).fill(3));

function bytes(base64: string): Uint8Array {
  const binary = atob(base64);
  const out = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) out[index] = binary.charCodeAt(index);
  return out;
}

const initial = (host.params as { state?: AppUpdateState }).state;
// The fixed state moves on with the event `test.state` (a download's next tick).
const fixed = signal<AppUpdateState>(initial ?? { kind: "hidden" });
host.on<AppUpdateState>("test.state", (next) => fixed.set(next));
const update: AppUpdate = initial
  ? { state: fixed, pending: () => "update" in fixed(), start: () => {}, press: () => host.post("test.pressed") }
  : createAppUpdate({
      publicKey: keys.publicKey,
      fetchBytes: async (url) => bytes(await host.call<string>("test.fetch", { url })),
    });
update.start();

mount(() => (
  <View class="flex-1 bg-background px-4">
    <Section title="About">
      <AboutUpdateRow update={update} />
    </Section>
    {update.pending() ? <Text>Update pending</Text> : null}
  </View>
));
