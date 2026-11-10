import { Text, View } from "nsh";
import { NavPage } from "nsh/base";

function Stub(props: { title: string; detail: string }) {
  return (
    <NavPage title={props.title}>
      <View class="flex-1 items-center justify-center gap-2 bg-background p-6">
        <Text class="text-lg font-semibold">{props.title}</Text>
        <Text class="text-center text-base text-muted-foreground">{props.detail}</Text>
      </View>
    </NavPage>
  );
}

export const Sessions = () => <Stub title="Sessions" detail="Sessions arrive in the next preview." />;
