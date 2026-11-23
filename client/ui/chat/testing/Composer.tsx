import { host, mount, signal, Text, View } from "nsh";
import { installPlatform } from "../../../core/platform";
import { nshPlatform } from "../../platform";
import { connection, current, servers } from "../../session";
import { AttachmentTray } from "../../more/files/Pages";
import { createAttachmentUploads } from "../../more/files/upload";
import { testServer } from "../../more/testing/fakeDaemon";
import { Composer, createComposerText } from "../Composer";
import { FileDrop } from "../FileDrop";
import { attachIncoming, type IncomingFiles } from "../../more/files/incoming";

/**
 * A chat composer with its attachments, as home and a Session wire it (`onFiles` and a `FileDrop` around it →
 * `attachIncoming`, a failure as the page's note) and "+" (`attach`, its sheet on a phone), for `nsh test`. `network`
 * plays the computer for pasted files and picked photos (the app uploads those itself), the host handlers for the
 * host's own (`files.pick`, `files.capture`, `files.upload`). What Send sends shows in `sent`.
 */
installPlatform(nshPlatform());
const server = testServer();
servers.set([server]);
current.set(server);
connection.set((host.params as { state?: "connected" | "waiting" }).state ?? "connected");

mount(() => {
  const text = createComposerText(() => null);
  const uploads = createAttachmentUploads();
  const notice = signal("");
  const sent = signal("");
  const attach = (incoming: IncomingFiles) => void attachIncoming(uploads, incoming).catch((error) => notice.set(error instanceof Error ? error.message : "Could not attach the file."));
  return (
    <FileDrop testID="file-drop-chat" class="flex-1 justify-end bg-background" onFiles={() => attach}>
      <Text testID="sent">{sent()}</Text>
      <Composer text={text} placeholder="Tell Brain" canSend={() => true} attached={() => uploads.ready().length > 0} running={() => false} stopping={() => false} canStop={() => false} onStop={() => {}}
        onSend={(value) => sent.set(`${value} ${uploads.ready().map((file) => file.path).join(",")}`)}
        onFiles={attach}
        attach={{ uploads, enabled: () => connection() === "connected", onError: (message) => notice.set(message) }}>
        {uploads.items().length > 0 ? <AttachmentTray uploads={uploads} /> : null}
        {notice() ? <Text testID="notice">{notice()}</Text> : null}
      </Composer>
    </FileDrop>
  );
});
