import type { PageNav } from "../routes";
import { Bookmarks, Feed } from "./Feed";
import { Drafts } from "./Drafts";
import { Notifications } from "./Notifications";
import { Blocked, Person } from "./Person";
import { Post } from "./Post";
import type { FriendsPage } from "./routes";

/** The page for a Friends route. */
export function FriendsPageView(props: { route: FriendsPage; nav: PageNav }) {
  const route = props.route;
  switch (route.name) {
    case "friends": return <Feed nav={props.nav} />;
    case "friends-post": return <Post id={route.id} nav={props.nav} />;
    case "friends-person": return <Person id={route.id} nav={props.nav} />;
    case "friends-drafts": return <Drafts nav={props.nav} />;
    case "friends-notifications": return <Notifications nav={props.nav} />;
    case "friends-bookmarks": return <Bookmarks nav={props.nav} />;
    case "friends-blocked": return <Blocked />;
  }
}
