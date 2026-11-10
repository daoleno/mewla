import { Text, View } from "nsh";
import { NavPage } from "nsh/base";
import { onTransport } from "../session";
import { bindDaemon } from "./daemon";
import { Resources } from "./resources/Resources";
import type { MorePage, PageNav } from "./routes";
import { Settings } from "./settings/Settings";
import { Stats, StatsDay, StatsModel } from "./stats/Stats";

// The More pages follow the current computer: a switch binds the next one and drops the last.
onTransport((transport) => {
  bindDaemon(transport);
  return () => bindDaemon(null);
});

/** The page for a More route. */
export function MorePageView(props: { route: MorePage; nav: PageNav; onPair: () => void }) {
  const route = props.route;
  switch (route.name) {
    case "settings": return <Settings nav={props.nav} onPair={props.onPair} />;
    case "stats": return <Stats nav={props.nav} />;
    case "stats-model": return <StatsModel model={route.model} pricing={route.pricing} />;
    case "stats-day": return <StatsDay day={route.day} />;
    case "resources": return <Resources />;
    case "screen": return <Soon title="Screen" />;
    case "extensions": return <Soon title="Extensions" />;
  }
}

function Soon(props: { title: string }) {
  return (
    <NavPage title={props.title}>
      <View class="flex-1 items-center justify-center bg-background"><Text class="text-base text-muted-foreground">{`${props.title} arrives in a later preview.`}</Text></View>
    </NavPage>
  );
}
