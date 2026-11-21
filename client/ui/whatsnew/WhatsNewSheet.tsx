import { Scroll, signal, Text, untracked, View } from "nsh";
import type { JSX } from "nsh";
import { Button } from "nsh/base";
import type { WhatsNewPage } from "../../core/whatsnew/whatsNew";
import { Icon } from "../kit/Icon";
import { PillButton } from "../kit/kit";
import { Sheet, SheetRoom } from "../kit/Sheet";
import { createTabPager, swipeable, TabPages } from "../kit/TabPager";
import { scrollBesidePinned } from "../../core/shell/sheet";
import { closeWhatsNew, sheet, tryIt, type WhatsNewSheetState } from "./state";
import { Scene } from "./scenes";

/**
 * What's New (Brain's SPEC 11-16): a bottom sheet on a phone, a centred card on wide and the web. One to three
 * pages, each the feature in use, a title and one line, with Try it; the last also has Done. Swipe, the arrows or the
 * dots move between them; a swipe down, the scrim, Esc and Done close it. A release with fixes only lists them.
 *
 * Whatever it shows scrolls when it is taller than the room the sheet leaves (a small phone, a phone held sideways, a
 * short browser window, a large text size), and the dots, Try it and Done stay pinned under the scroll, so Done is
 * always on screen. The sheet itself never scrolls (kit/Sheet.tsx): 0.4.19's nine fixes ran past its 88 % cap on a
 * phone, clipped, with Done under the screen's edge and no way to reach it (owner, 11-22).
 */
export function WhatsNewSheet() {
  return (
    <Sheet testID="whats-new" open={() => Boolean(sheet())} onClose={closeWhatsNew}>
      <Each of={sheet() ? [sheet()!] : []} key={(state) => state}>{(state) => (
        <If when={state().pages.length > 0} else={<Fixes state={state()} />}><Pages pages={state().pages} /></If>
      )}</Each>
    </Sheet>
  );
}

function Pages(props: { pages: WhatsNewPage[] }) {
  const pages = props.pages;
  const keys = pages.map((_, index) => `p${index}`);
  const index = signal(0);
  const pager = createTabPager({ keys, value: () => keys[index()], onChange: (key) => index.set(Math.max(0, keys.indexOf(key))) });
  const page = () => pages[index()] ?? pages[0];
  const last = () => index() === pages.length - 1;
  const go = (to: number) => pager.choose(keys[Math.min(pages.length - 1, Math.max(0, to))]);
  const views = Object.fromEntries(pages.map((p, i) => [keys[i], () => <PageView page={p} index={i} />]));
  return (
    <Fitted pinned={<View class="gap-4 pt-4">
      <View class="flex-row items-center justify-center gap-3 px-5">
        <If when={!swipeable && pages.length > 1}><Arrow name="chevron-left" label="Previous" disabled={() => index() === 0} onPress={() => go(untracked(index) - 1)} /></If>
        <If when={pages.length > 1}><View testID="whats-new-dots" class="flex-row items-center gap-2">
            <Each of={keys} key={(key) => key}>{(key) => {
              const at = untracked(() => keys.indexOf(key()));
              return (
                <Button testID={`whats-new-dot-${at}`} accessibilityLabel={`Page ${at + 1} of ${pages.length}`} onPress={() => go(at)} class="size-6 items-center justify-center">
                  <View class={`size-[7px] rounded-full ${index() === at ? "bg-foreground" : "bg-track"}`} />
                </Button>
              );
            }}</Each>
          </View></If>
        <If when={!swipeable && pages.length > 1}><Arrow name="chevron-right" label="Next" disabled={last} onPress={() => go(untracked(index) + 1)} /></If>
      </View>
      <View class="gap-1 px-5">
        <If when={page().try}><PillButton testID="whats-new-try" title="Try it" filled onPress={() => tryIt(untracked(page).try!)} /></If>
        <If when={last()}><PillButton testID="whats-new-done" title="Done" kind={page().try ? "ghost" : "ink"} onCanvas onPress={closeWhatsNew} /></If>
      </View>
    </View>}>
      <TabPages pager={pager} testID="whats-new-pages" pages={views} />
    </Fitted>
  );
}

function Arrow(props: { name: "chevron-left" | "chevron-right"; label: string; disabled: () => boolean; onPress: () => void }) {
  return (
    <Button testID={`whats-new-${props.label.toLowerCase()}`} accessibilityLabel={props.label} disabled={props.disabled()} onPress={props.onPress}
      class="size-9 items-center justify-center rounded-full bg-card active:opacity-70 disabled:opacity-30">
      <Icon name={props.name} size={18} tone="ink" />
    </Button>
  );
}

function PageView(props: { page: WhatsNewPage; index: number }) {
  const page = props.page;
  return (
    <View testID={`whats-new-page-${props.index}`} class="gap-3 px-5 pt-1">
      <Scene name={page.media.scene} version={page.version} target={page.try?.target} />
      <View class="gap-1 pt-1">
        <Text testID="whats-new-version" class="text-caption font-semibold text-muted-foreground">{`Mewla ${page.version}`}</Text>
        <Text testID="whats-new-title" role="header" class="text-title font-bold tracking-title text-foreground">{page.title}</Text>
        <Text testID="whats-new-line" class="text-body text-muted-foreground">{page.line}</Text>
      </View>
    </View>
  );
}

/** A release with fixes only (its chat line, About's What's new): what it fixed. */
function Fixes(props: { state: WhatsNewSheetState }) {
  return (
    <View testID="whats-new-fixes">
      <Fitted pinned={<View class="px-5 pt-3"><PillButton testID="whats-new-done" title="Done" filled onPress={closeWhatsNew} /></View>}>
        <View class="gap-3 px-5">
          <View class="gap-1">
            <Text class="text-caption font-semibold text-muted-foreground">{`Mewla ${props.state.version}`}</Text>
            <Text role="header" class="text-title font-bold tracking-title text-foreground">Fixed</Text>
          </View>
          <View class="gap-2.5 rounded-(--radius-group) bg-sheet-surface p-4">
            <Each of={props.state.fixes} key={(fix) => fix}>{(fix) => (
              <View class="flex-row gap-2.5">
                <View class="pt-[3px]"><Icon name="check" size={15} tone="soft" /></View>
                <Text class="flex-1 text-body text-foreground">{fix()}</Text>
              </View>
            )}</Each>
          </View>
        </View>
      </Fitted>
    </View>
  );
}

/** The body's own foot under its pinned controls (`pb-2`). */
const BODY_FOOT = 8;

/**
 * A scroll in the room the sheet leaves (kit/Sheet.tsx `SheetRoom`, core/shell/sheet.ts), with `pinned` under it as
 * laid out: the scroll takes what the pinned controls leave, so they are on screen however tall what scrolls is.
 * Made under the sheet, so it reads its room.
 */
function Fitted(props: { pinned: JSX.Element; children: JSX.Element }) {
  const room = SheetRoom();
  const pinned = signal(0);
  return (
    <View>
      <Scroll testID="whats-new-scroll" style={{ maxHeight: scrollBesidePinned(room(), pinned(), Number.MAX_SAFE_INTEGER) }}>{props.children}</Scroll>
      <View style={{ paddingBottom: BODY_FOOT }} onLayout={({ bounds }) => pinned.set(bounds.size.height)}>{props.pinned}</View>
    </View>
  );
}
