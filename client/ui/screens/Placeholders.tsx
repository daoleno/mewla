import { Text, View } from "nsh";
import { NavPage } from "nsh/base";
import { connection, current } from "../session";

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

export const Brain = () => (
  <Stub title="Brain" detail={current() ? `${current()!.name || "Your computer"} · ${connection() === "connected" ? "connected" : connection()}` : "No computer"} />
);
export const Sessions = () => <Stub title="Sessions" detail="Sessions arrive in the next preview." />;
export const Calendar = () => <Stub title="Calendar" detail="Calendar arrives in a later preview." />;
