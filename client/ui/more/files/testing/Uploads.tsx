import { host, mount, signal, Text, View } from "nsh";
import { Button, Input } from "nsh/base";
import { installPlatform } from "../../../../core/platform";
import { nshPlatform } from "../../../platform";
import { connection, current, servers } from "../../../session";
import { testServer } from "../../testing/fakeDaemon";
import { AttachmentTray } from "../Attachments";
import { createAttachmentUploads } from "../upload";

/**
 * A composer's attachments on their own, for `nsh test`: `files.*` handlers play the host,
 * `network` the computer for pasted files (the app uploads those itself).
 */
installPlatform(nshPlatform());
const server = testServer();
servers.set([server]);
current.set(server);
connection.set((host.params as { state?: "connected" | "waiting" }).state ?? "connected");

mount(() => {
  const uploads = createAttachmentUploads();
  const failure = signal("");
  const fail = (error: unknown) => failure.set(error instanceof Error ? error.message : String(error));
  return (
    <View class="flex-1 bg-background pt-10">
      <Button testID="attach" disabled={!uploads.canAttach()} onPress={() => void uploads.pickAndUpload().catch(fail)}><Text>Attach File</Text></Button>
      <Input
        testID="composer"
        pasteBehavior="report"
        onPaste={(event) => {
          try {
            uploads.add(event.files);
          } catch (error) {
            fail(error);
          }
        }}
      />
      <Text testID="failure">{failure()}</Text>
      <Text testID="ready">{uploads.ready().map((file) => file.path).join(",")}</Text>
      <AttachmentTray uploads={uploads} />
    </View>
  );
});
