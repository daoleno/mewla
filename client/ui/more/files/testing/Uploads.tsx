import { mount, Text, View } from "nsh";
import { Button } from "nsh/base";
import { installPlatform } from "../../../../core/platform";
import { nshPlatform } from "../../../platform";
import { current, servers } from "../../../session";
import { testServer } from "../../testing/fakeDaemon";
import { AttachmentTray } from "../Attachments";
import { createAttachmentUploads } from "../upload";

/** A composer's attachments on their own, for `nsh test`: `files.*` handlers play the phone, `network` nothing (the host uploads). */
installPlatform(nshPlatform());
const server = testServer();
servers.set([server]);
current.set(server);

mount(() => {
  const uploads = createAttachmentUploads();
  return (
    <View class="flex-1 bg-background pt-10">
      <Button testID="attach" onPress={() => void uploads.pickAndUpload().catch(() => {})}><Text>Attach File</Text></Button>
      <Text testID="ready">{uploads.ready().map((file) => file.path).join(",")}</Text>
      <AttachmentTray uploads={uploads} />
    </View>
  );
});
