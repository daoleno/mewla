import { host, mount, signal, Text, View } from "nsh";
import { installPlatform } from "../../../core/platform";
import { nshPlatform } from "../../platform";
import { connection, current, servers } from "../../session";
import { AttachmentTray } from "../../more/files/Pages";
import { createAttachmentUploads } from "../../more/files/upload";
import { testServer } from "../../more/testing/fakeDaemon";
import { Composer, createComposerText } from "../Composer";

/**
 * A chat composer with its attachments, as home and a Session wire it (`onFiles` → `uploads.add`,
 * a failure as the page's note), for `nsh test`. `network` plays the computer for pasted files
 * (the app uploads those itself). What Send sends shows in `sent`.
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
  return (
    <View class="flex-1 justify-end bg-background">
      <Text testID="sent">{sent()}</Text>
      <Composer text={text} placeholder="Tell Brain" canSend={() => true} attached={() => uploads.ready().length > 0} running={() => false} stopping={() => false} canStop={() => false} onStop={() => {}}
        onSend={(value) => sent.set(`${value} ${uploads.ready().map((file) => file.path).join(",")}`)}
        onFiles={(files) => {
          try {
            uploads.add(files);
          } catch (error) {
            notice.set(error instanceof Error ? error.message : "Could not attach the file.");
          }
        }}>
        {uploads.items().length > 0 ? <AttachmentTray uploads={uploads} /> : null}
        {notice() ? <Text testID="notice">{notice()}</Text> : null}
      </Composer>
    </View>
  );
});
