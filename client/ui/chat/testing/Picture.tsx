import { host, mount, View } from "nsh";
import { ChatImage } from "../ChatImage";

/**
 * A chat picture on its own, for `nsh test`: the host's `app.picture` call plays the computer
 * reading it (a data URL, or null when it could not).
 */
mount(() => (
  <View class="flex-1 items-start bg-background p-4 pt-10">
    <ChatImage frame="sent" src="/state/uploads/shot.png" alt="shot.png" load={(path) => host.call("app.picture", { path }) as Promise<string | null>} />
  </View>
));
