import type { PageNav } from "../routes";
import { Bookmarks, Feed } from "./Feed";
import { Drafts } from "./Drafts";
import { Notifications } from "./Notifications";
import { Blocked, Person } from "./Person";
import { Post } from "./Post";
import type { FriendsPage } from "./routes";

/**
 * Whether the page of `route` is the top of its stack (reactive). A stack's routes
 * are the values pushed, so the page finds its own by identity; a stack that does
 * not say its routes counts every page as on top.
 */
export function onTop(nav: PageNav, route: unknown): () => boolean {
  return () => {
    const routes = nav.routes?.();
    return !routes || routes.length === 0 || routes[routes.length - 1] === route;
  };
}

/** The page for a Friends route. */
export function FriendsPageView(props: { route: FriendsPage; nav: PageNav }) {
  const route = props.route;
  const top = onTop(props.nav, route);
  switch (route.name) {
    case "friends": return <Feed nav={props.nav} top={top} />;
    case "friends-post": return <Post id={route.id} nav={props.nav} top={top} />;
    case "friends-person": return <Person id={route.id} nav={props.nav} top={top} />;
    case "friends-drafts": return <Drafts nav={props.nav} top={top} />;
    case "friends-notifications": return <Notifications nav={props.nav} top={top} />;
    case "friends-bookmarks": return <Bookmarks nav={props.nav} top={top} />;
    case "friends-blocked": return <Blocked top={top} />;
  }
}
