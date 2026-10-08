import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { Platform, StyleSheet, View } from "react-native";
import { useNavigationContainerRef, usePathname, useRouter } from "expo-router";
import { useAppColors } from "../../constants/tokens";
import { useCurrentServer } from "../../store/currentServer";
import { useWorkers } from "../../store/workers";
import { useBrain } from "../../store/brain";
import { terminalRouteParams } from "../../services/workerRouteId";
import { brainWorkSurface } from "../brain/brainWorkSurface";
import { PrimaryDrawerPanel } from "./PrimaryDrawerPanel";
import { DesktopWebShellContext, useDesktopWeb } from "./useDesktopWeb";
import {
  desktopDocumentTitle,
  desktopShellExcluded,
  desktopSidebarKey,
  desktopSidebarNavigation,
  desktopSidebarPath,
  desktopScreenRetained,
  type DesktopSidebarKey,
} from "./desktopWeb";
import { resolveGlobalKey, type DesktopCommand, type KeyTarget } from "./desktopShortcuts";
import { buildPaletteItems, type PaletteAction } from "./desktopPalette";
import { DesktopCommandPalette, DesktopShortcutsDialog } from "./DesktopCommandPalette";
import { ResizeHandle, useResizableWidth } from "./ResizeHandle";
import type { PrimaryDrawerPathname } from "./primaryDrawerDestinations";

const SIDEBAR_DEFAULT_WIDTH = 264;
const SIDEBAR_MIN_WIDTH = 216;
const SIDEBAR_MAX_WIDTH = 400;

interface NavigationStateLike {
  routes: Array<{ name: string; state?: unknown }>;
}

/**
 * Route names of the app's root Stack: the navigator holding `(primary)`,
 * found under whatever wrappers expo-router puts at the top.
 */
function rootStackRouteNames(state: NavigationStateLike | undefined): string[] {
  let current: NavigationStateLike | undefined = state;
  while (current) {
    if (current.routes.some((route) => route.name === "(primary)")) {
      return current.routes.map((route) => route.name);
    }
    current = current.routes.length === 1
      ? (current.routes[0].state as NavigationStateLike | undefined)
      : undefined;
  }
  return [];
}

interface ScreenLayoutProps {
  route: { key: string };
  navigation: { getState(): { routes: Array<{ key: string }> } };
  children: React.ReactElement;
}

function DesktopRetainedScreen({ route, navigation, children }: ScreenLayoutProps) {
  const desktop = useContext(DesktopWebShellContext);
  if (!desktop) return children;
  const routes = navigation.getState().routes;
  const index = routes.findIndex((candidate) => candidate.key === route.key);
  return desktopScreenRetained(index, routes.length) ? children : null;
}

/** The root Stack's `screenLayout`: drops pages deep under the newest ones. */
export function desktopScreenLayout(props: ScreenLayoutProps) {
  return <DesktopRetainedScreen {...props} />;
}

/** A page names itself in the tab title (a Session its name). */
const PageTitleContext = createContext<((title: string | null) => void) | null>(null);

export function useDesktopPageTitle(title: string | null | undefined, active = true) {
  const setTitle = useContext(PageTitleContext);
  useEffect(() => {
    if (!setTitle || !active) return;
    setTitle(title?.trim() || null);
    return () => setTitle(null);
  }, [active, setTitle, title]);
}

function keyTarget(target: EventTarget | null): KeyTarget | null {
  const element = target as HTMLElement | null;
  if (!element || typeof element.closest !== "function") return null;
  return {
    tagName: element.tagName,
    isContentEditable: element.isContentEditable,
    inTerminal: Boolean(element.closest(".xterm")),
  };
}

/** The visible chat composer, if this page has one. */
function focusVisibleComposer() {
  const inputs = Array.from(document.querySelectorAll<HTMLElement>("[id='mewla-composer']"));
  const visible = inputs.find((input) => input.offsetParent !== null);
  visible?.focus();
}

/** Global desktop CSS: visible focus rings and selectable chats. */
function useDesktopWebStyles(enabled: boolean, focusRing: string) {
  useEffect(() => {
    if (!enabled || typeof document === "undefined") return;
    const style = document.createElement("style");
    style.setAttribute("data-mewla", "desktop-web");
    style.textContent = [
      `[tabindex]:focus-visible,button:focus-visible,a:focus-visible,[role=button]:focus-visible,[role=tab]:focus-visible,[role=link]:focus-visible,[role=separator]:focus-visible{outline:2px solid ${focusRing};outline-offset:2px}`,
    ].join("\n");
    document.head.appendChild(style);
    return () => style.remove();
  }, [enabled, focusRing]);
}

/**
 * Desktop web: one sidebar beside every page. Below 1024 pt, on native, and
 * while pairing, it renders its children alone; the tree keeps its shape so
 * crossing the breakpoint never remounts the navigator.
 */
export function DesktopWebShell({ children }: { children: ReactNode }) {
  const desktopWidth = useDesktopWeb();
  const pathname = usePathname();
  const router = useRouter();
  const colors = useAppColors();
  const desktop = desktopWidth && !desktopShellExcluded(pathname);
  const selectedKey = desktopSidebarKey(pathname);
  const [pageTitle, setPageTitle] = useState<string | null>(null);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [shortcutsOpen, setShortcutsOpen] = useState(false);
  const sidebar = useResizableWidth(
    "mewla.desktop.sidebarWidth",
    SIDEBAR_DEFAULT_WIDTH,
    SIDEBAR_MIN_WIDTH,
    SIDEBAR_MAX_WIDTH,
  );

  useDesktopWebStyles(desktop, colors.focusRing);

  useEffect(() => {
    if (Platform.OS !== "web" || typeof document === "undefined") return;
    document.title = desktopDocumentTitle(pathname, pageTitle ?? undefined);
  }, [pageTitle, pathname]);

  const { currentServer } = useCurrentServer();
  const { state: workerState } = useWorkers();
  const { state: brainState } = useBrain();
  const serverId = currentServer?.id ?? null;
  const paletteItems = useMemo(() => {
    if (!paletteOpen) return [];
    const sessions = workerState.workers
      .filter((worker) => worker.serverId === serverId)
      .sort((a, b) => (b.updated_at ?? 0) - (a.updated_at ?? 0))
      .map((worker) => ({
        id: worker.id,
        name: worker.name || worker.id,
        detail: worker.project || worker.cwd || undefined,
      }));
    const brain = serverId ? brainState.byServer[serverId] : null;
    const work = brainWorkSurface(brain?.current_work, brain?.workers).slips.map((slip) => ({
      workId: slip.workId,
      title: slip.title,
      statusLabel: slip.statusLabel,
      sessionId: slip.sessionId,
    }));
    return buildPaletteItems({ sessions, work });
  }, [brainState.byServer, paletteOpen, serverId, workerState.workers]);

  const navigationRef = useNavigationContainerRef();
  const go = useCallback(
    (key: DesktopSidebarKey) => {
      const path = desktopSidebarPath(key) as never;
      const stack = rootStackRouteNames(navigationRef.getRootState());
      if (desktopSidebarNavigation(stack, key) === "home") router.dismissTo(path);
      else router.navigate(path);
    },
    [navigationRef, router],
  );

  const runAction = useCallback(
    (action: PaletteAction | DesktopCommand) => {
      setPaletteOpen(false);
      switch (action.kind) {
        case "go":
          go(action.key);
          return;
        case "palette":
          setPaletteOpen(true);
          return;
        case "shortcuts":
          setShortcutsOpen(true);
          return;
        case "new-session":
          router.navigate({ pathname: "/list", params: { newSession: String(Date.now()) } });
          return;
        case "new-brain-chat":
          router.navigate({ pathname: "/", params: { newChat: String(Date.now()) } });
          return;
        case "focus-composer":
          focusVisibleComposer();
          return;
        case "session":
          if (serverId) {
            router.navigate({
              pathname: "/terminal/[id]",
              params: terminalRouteParams(action.workerId, serverId),
            });
          }
          return;
        case "work":
          if (action.sessionId && serverId) {
            router.navigate({
              pathname: "/terminal/[id]",
              params: terminalRouteParams(action.sessionId, serverId),
            });
          } else {
            router.navigate({ pathname: "/", params: { work: action.workId } });
          }
          return;
      }
    },
    [go, router, serverId],
  );

  const pendingGoRef = useRef(false);
  const overlayOpen = paletteOpen || shortcutsOpen;
  useEffect(() => {
    if (!desktop) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented) return;
      if (overlayOpen) return;
      const result = resolveGlobalKey(event, {
        pendingGo: pendingGoRef.current,
        target: keyTarget(event.target),
      });
      pendingGoRef.current = result.pendingGo;
      if (!result.command && !result.pendingGo) return;
      event.preventDefault();
      if (result.command) runAction(result.command);
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [desktop, overlayOpen, runAction]);

  const openPath = useCallback(
    (pathname: PrimaryDrawerPathname) => {
      const key = desktopSidebarKey(pathname);
      if (key) go(key);
    },
    [go],
  );
  const selectPrimary = useCallback(
    (route: "brain" | "list") => go(route === "brain" ? "brain" : "sessions"),
    [go],
  );
  const noop = useCallback(() => undefined, []);
  const closeButtonRef = useRef<View>(null);

  return (
    <DesktopWebShellContext.Provider value={desktop}>
      <PageTitleContext.Provider value={setPageTitle}>
        <View style={[styles.root, { backgroundColor: colors.bgPrimary }]}>
          {desktop ? (
            <View
              role="navigation"
              accessibilityLabel="Navigation"
              style={[
                styles.sidebar,
                { width: sidebar.width, borderRightColor: colors.borderSubtle },
              ]}
            >
              <PrimaryDrawerPanel
                closeButtonRef={closeButtonRef}
                drawerVisible
                activePrimaryRoute={selectedKey === "sessions" ? "list" : "brain"}
                onSelectPrimaryRoute={selectPrimary}
                onClose={noop}
                onClosePressIn={noop}
                onNavigateAway={noop}
                onOpenPath={openPath}
                selectedKey={selectedKey}
                docked
              />
              <ResizeHandle
                edge="right"
                width={sidebar.width}
                label="Resize sidebar"
                onResize={sidebar.update}
                onCommit={sidebar.commit}
                onReset={sidebar.reset}
              />
            </View>
          ) : null}
          <View style={styles.main}>{children}</View>
        </View>
        {desktop ? (
          <>
            <DesktopCommandPalette
              visible={paletteOpen}
              items={paletteItems}
              onRun={runAction}
              onClose={() => setPaletteOpen(false)}
            />
            <DesktopShortcutsDialog
              visible={shortcutsOpen}
              onClose={() => setShortcutsOpen(false)}
            />
          </>
        ) : null}
      </PageTitleContext.Provider>
    </DesktopWebShellContext.Provider>
  );
}

const styles = StyleSheet.create({
  root: {
    flex: 1,
    flexDirection: "row",
  },
  sidebar: {
    borderRightWidth: StyleSheet.hairlineWidth,
    position: "relative",
  },
  main: {
    flex: 1,
    minWidth: 0,
  },
});
