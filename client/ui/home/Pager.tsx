import { AnimatedValue, batch, cleanup, effect, platform, signal, untracked, View } from "nsh";
import { BackHandler } from "nsh/mobile";
import type { JSX } from "nsh";
import type { PageNav } from "../more/routes";
import { homeCalls, homeShown, pageNav, trayOpen } from "../shell/nav";
import { refreshOpened } from "../tray/state";

/**
 * Trial: three pages (SPEC 1, under trial since 11-13; owner-0113/PAGER-FEASIBILITY.md). Brain's home is the
 * middle of three pages the person swipes between, with no bottom bar: a swipe right from Brain shows the
 * computer page on its left (what the cat block opens), a swipe left the running things on its right (what
 * the state control opens, as a page instead of a sheet).
 *
 * The pages follow the finger on the UI thread (`pan` bound to an `AnimatedValue`, activated past 12 pt
 * sideways, failing past 12 pt vertically so the thread keeps its vertical scroll) and settle like a native
 * pager: past half a page, or a flick, goes on; anything less springs back. The system's edge Back is the
 * system's; text selection and a code block's own sideways scroll (`kit/SideScroll`) keep their touches.
 * All three stay mounted side by side, so the thread's scroll, the draft and the keyboard survive a visit.
 */

/** 0 the computer page, 1 Brain, 2 the running things. */
export const homePage = signal(1);

/** Opens a page beside Brain, as the cat block and the state control do by tap. */
export function showHomePage(page: number): void {
  homePage.set(page);
  trayOpen.set(page === 2);
}

/**
 * The computer page's nav while it sits left of Brain. The page asks every computer again whenever it is
 * on top (its route last); beside Brain it is on top only while it is the page in view, not all the time
 * home is: it reads as a `computer` route under one more route except while page 0 shows.
 */
export const besideBrainNav: PageNav = {
  ...pageNav,
  routes: () => {
    const routes = [...(pageNav.routes?.() ?? []), { name: "computer" }];
    return homePage() === 0 ? routes : [...routes, { name: "beside-brain" }];
  },
};

export function HomePager(props: { computer: JSX.Element; brain: JSX.Element; running: JSX.Element }) {
  const width = signal(0);
  // Where the pager rests is layout: the row of three is laid out with the page it rests on in view (Brain
  // before the first layout). `x` is only how far it is moved from there while a finger or a spring moves
  // it; once the spring lands, the new page becomes the layout and `x` goes back to 0 in the same step. A
  // host that draws no transforms (the test engine) still shows the page the pager rests on.
  const rest = signal(1);
  const x = new AnimatedValue(0);
  // The page it is going to (or rests on), and which move is the latest: a spring that a finger or a newer
  // move interrupted never commits.
  let target = 1;
  let moving = 0;

  // Springs to `page` from wherever it is (a spring to where it already is ends at once), then makes that page the layout.
  const goTo = (page: number, velocity = 0) => {
    target = page;
    const run = ++moving;
    const w = untracked(width);
    if (!w) {
      rest.set(page);
      x.set(0);
      return;
    }
    void x.spring((untracked(rest) - page) * w, { velocity, stiffness: 400, damping: 40 }).then((done) => {
      if (!done.finished || run !== moving) return;
      batch(() => {
        rest.set(page);
        x.set(0);
      });
    });
  };

  // The state control (and every caller of `trayOpen`, a page that pops back to an open tray too) moves the pager.
  effect(() => {
    const open = trayOpen();
    untracked(() => {
      if (open && homePage() !== 2) homePage.set(2);
      if (!open && homePage() === 2) homePage.set(1);
    });
  });

  // Whatever goes home (a notification, a link, a server switch) means Brain, or the running things when it asks for the tray.
  effect(() => {
    if (!homeCalls()) return;
    untracked(() => homePage.set(trayOpen() ? 2 : 1));
  });

  // A tap (the cat block, the state control, Back) springs to the page it names.
  effect(() => {
    const page = homePage();
    untracked(() => {
      if (page !== target) goTo(page);
    });
  });

  const settle = (velocity: number, value: number) => {
    const w = untracked(width) || 1;
    // Where the finger left it plus a flick's worth: past half a page, or a fast flick, goes on, one page at most.
    const from = target;
    const projected = Math.round(untracked(rest) - (value + velocity * 0.15) / w);
    const next = Math.max(0, Math.min(2, Math.max(from - 1, Math.min(from + 1, projected))));
    if (next === 2 && from !== 2) refreshOpened();
    goTo(next, velocity);
    homePage.set(next);
    trayOpen.set(next === 2);
  };

  const measured = (w: number) => {
    if (w === untracked(width)) return;
    // A new width (a rotation): it rests on its page at once.
    moving++;
    void x.stop();
    batch(() => {
      width.set(w);
      rest.set(target);
      x.set(0);
    });
  };

  // Android's Back from either side page returns to Brain (while home is the page shown).
  if (platform.os === "android") {
    cleanup(BackHandler.on("press", () => {
      if (!untracked(homeShown) || untracked(homePage) === 1) return false;
      showHomePage(1);
      return true;
    }));
  }

  // The pages beside Brain are made once the pager has its width (after the first frame), not with Brain:
  // starting stays as quick as Brain alone, and they are there, kept, before the first swipe reaches them.
  const sides = signal(false);
  const page = (child: () => JSX.Element, testID: string) => <View testID={testID} class="h-full overflow-hidden" style={{ width: width() }}>{child()}</View>;
  return (
    <View testID="home-pager" class="flex-1 overflow-hidden" onLayout={(e) => { measured(e.bounds.size.width); sides.set(true); }}>
      <View class="flex-1 flex-row" style={{ width: width() * 3, marginLeft: -rest() * width(), transform: [{ translateX: x }] }}
        pan={{ x }} panActivation={{ activeOffsetX: 12, failOffsetY: 12 }}
        onPanStart={() => { moving++; void x.stop(); }}
        onPanEnd={(e) => settle(e.velocity.x, e.value?.x ?? e.translation.x)}>
        {page(() => (sides() ? props.computer : null), "home-page-computer")}
        {page(() => props.brain, "home-page-brain")}
        {page(() => (sides() ? props.running : null), "home-page-running")}
      </View>
    </View>
  );
}
