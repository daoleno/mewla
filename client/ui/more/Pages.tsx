import { Text, View } from "nsh";
import { NavPage } from "nsh/base";
import { onTransport } from "../session";
import { bindDaemon } from "./daemon";
import { Resources } from "./resources/Resources";
import type { MorePage, PageNav } from "./routes";
import { Computer } from "./settings/Computer";
import { OutsideAgents } from "./settings/OutsideAgents";
import { ProviderEditor, ProviderModels, Providers } from "./settings/Providers";
import { restoreAppearance, Settings } from "./settings/Settings";
import { Telegram } from "./settings/Telegram";
import { Stats, StatsDay, StatsModel } from "./stats/Stats";

restoreAppearance();

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
    case "settings-computer": return <Computer id={route.id} nav={props.nav} onPair={props.onPair} />;
    case "settings-telegram": return <Telegram />;
    case "settings-providers": return <Providers nav={props.nav} />;
    case "settings-provider-editor": return <ProviderEditor client={route.client} connectionId={route.connectionId} retry={route.retry} nav={props.nav} />;
    case "settings-provider-models": return <ProviderModels connectionId={route.connectionId} />;
    case "settings-agents": return <OutsideAgents />;
    case "settings-friends": return <Soon title="Friends" />;
    case "settings-iphone": return <Soon title="iPhone use" />;
    case "settings-pet": return <Soon title="Pet" />;
    case "settings-licenses": return <Soon title="Open-source Licenses" />;
    case "settings-license": return <Soon title="License" />;
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
