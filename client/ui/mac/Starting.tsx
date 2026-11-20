import { Button } from "nsh/base";
import { Text, View } from "nsh";
import { PetSprite } from "../pets/PetSprite";
import { joinLocalDaemon, localProblem } from "./local";

/** The Mac app before its own daemon answers: the cat, and why it has not, with Try Again. */
export function MacStarting() {
  return (
    <View testID="mac-starting" class="flex-1 items-center justify-center gap-4 bg-background p-6 text-foreground">
      <PetSprite testID="mac-starting-cat" state={() => (localProblem() ? "offline" : "waking")} size={104} />
      <If when={localProblem()} else={<Text class="text-sub text-muted-foreground">Brain is starting…</Text>}>
        <Text class="text-sub text-muted-foreground">{localProblem()}</Text>
        <Button onPress={() => void joinLocalDaemon()}><Text class="text-body font-semibold">Try Again</Text></Button>
      </If>
    </View>
  );
}
