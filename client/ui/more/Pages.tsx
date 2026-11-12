import { onTransport } from "../session";
import { bindDaemon } from "./daemon";
import { ExtensionsPageView } from "./extensions/Pages";
import { FilesPageView } from "./files/Pages";
import { FriendsPageView } from "./friends/FriendsPages";
import { FriendsSettings } from "./friends/FriendsSettings";
import { GitPageView } from "./git/GitPages";
import { PetPage } from "./pet/Pet";
import { restorePet } from "./pet/pets";
import { Resources } from "./resources/Resources";
import { PhonePage } from "../phone/PhonePage";
import { ScreenPage } from "./screen/Screen";
import { VirtualScreenPage } from "./screen/VirtualScreen";
import type { MorePage, PageNav } from "./routes";
import { Computer } from "./settings/Computer";
import { IPhone } from "./settings/IPhone";
import { License, Licenses } from "./settings/Licenses";
import { OutsideAgents } from "./settings/OutsideAgents";
import { ProviderEditor, ProviderModels, Providers } from "./settings/Providers";
import { restoreAppearance, Settings } from "./settings/Settings";
import { Telegram } from "./settings/Telegram";
import { Stats, StatsDay, StatsModel } from "./stats/Stats";

restoreAppearance();
restorePet();

// The More pages follow the current computer: a switch binds the next one and drops the last.
onTransport((transport) => {
  bindDaemon(transport);
  return () => bindDaemon(null);
});

/** The page for a More route. */
export function MorePageView(props: { route: MorePage; nav: PageNav; onPair: () => void }) {
  const route = props.route;
  switch (route.name) {
    case "settings": return <Settings nav={props.nav} />;
    case "settings-computer": return <Computer id={route.id} nav={props.nav} onPair={props.onPair} />;
    case "settings-telegram": return <Telegram />;
    case "settings-providers": return <Providers nav={props.nav} />;
    case "settings-provider-editor": return <ProviderEditor client={route.client} connectionId={route.connectionId} retry={route.retry} nav={props.nav} />;
    case "settings-provider-models": return <ProviderModels connectionId={route.connectionId} />;
    case "settings-agents": return <OutsideAgents />;
    case "settings-friends": return <FriendsSettings nav={props.nav} />;
    case "friends":
    case "friends-post":
    case "friends-person":
    case "friends-drafts":
    case "friends-notifications":
    case "friends-bookmarks":
    case "friends-blocked":
      return <FriendsPageView route={route} nav={props.nav} />;
    case "settings-iphone": return <IPhone />;
    case "settings-phone": return <PhonePage />;
    case "settings-virtual-screen": return <VirtualScreenPage />;
    case "settings-pet": return <PetPage />;
    case "settings-licenses": return <Licenses nav={props.nav} />;
    case "settings-license": return <License id={route.id} nav={props.nav} />;
    case "stats": return <Stats nav={props.nav} />;
    case "stats-model": return <StatsModel model={route.model} pricing={route.pricing} nav={props.nav} />;
    case "stats-day": return <StatsDay day={route.day} nav={props.nav} />;
    case "resources": return <Resources nav={props.nav} />;
    case "screen": return <ScreenPage nav={props.nav} worker={route.worker} />;
    case "extensions":
    case "extensions-service":
    case "extensions-tools":
    case "extensions-agent":
      return <ExtensionsPageView route={route} nav={props.nav} />;
    case "git":
    case "git-diff":
    case "git-commit":
    case "git-commit-file":
    case "git-files":
    case "git-file":
      return <GitPageView route={route} nav={props.nav} />;
    case "files-preview":
    case "files-workspace":
      return <FilesPageView route={route} nav={props.nav} />;
  }
}
