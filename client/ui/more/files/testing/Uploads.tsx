import { host, mount, signal, Text, View } from "nsh";
import { Button } from "nsh/base";
import { installPlatform } from "../../../../core/platform";
import { nshPlatform } from "../../../platform";
import { connection, current, servers } from "../../../session";
import { testServer } from "../../testing/fakeDaemon";
import { AttachmentTray } from "../Attachments";
import { createAttachmentUploads } from "../upload";

/** A composer's attachments on their own, for `nsh test`: `files.*` handlers play the phone, `network` nothing (the host uploads). */
installPlatform(nshPlatform());
const server = testServer();
servers.set([server]);
current.set(server);
connection.set((host.params as { state?: "connected" | "waiting" }).state ?? "connected");

mount(() => {
  const uploads = createAttachmentUploads();
  const failure = signal("");
  return (
    <View class="flex-1 bg-background pt-10">
      <Button testID="attach" disabled={!uploads.canAttach()} onPress={() => void uploads.pickAndUpload().catch((error: Error) => failure.set(error.message))}><Text>Attach File</Text></Button>
      <Text testID="failure">{failure()}</Text>
      <Text testID="ready">{uploads.ready().map((file) => file.path).join(",")}</Text>
      <AttachmentTray uploads={uploads} />
    </View>
  );
});
