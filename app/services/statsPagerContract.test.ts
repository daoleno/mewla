// @ts-nocheck
import { describe, expect, it } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

const source = readFileSync(join(import.meta.dir, '../app/stats.tsx'), 'utf8');

describe('Stats pager integration contract', () => {
  it('uses the shared native tab-view architecture with bounded platform behavior', () => {
    expect(source).toContain("import { TabView } from 'react-native-tab-view'");
    expect(source).toContain('onIndexChange={selectRangeIndex}');
    expect(source).toContain('onChange={jumpTo}');
    expect(source).toContain('animationEnabled={!reducedMotion}');
    expect(source).toContain('overScrollMode="never"');
  });

  it('drives tab feedback from the pager position throughout a swipe or rollback', () => {
    expect(source).toContain('renderTabBar={({ position, jumpTo }) => (');
    expect(source).toContain('<StatsRangeTabBar');
    // The thumb and ink follow `position` inside the shared SegmentedControl, not a local copy.
    expect(source).toContain('<SegmentedControl');
    expect(source).toContain('position={position}');
    expect(source).not.toContain('rangeTabIndicator');
  });

  it('keeps the Activity chart scrub in control of its touches', () => {
    expect(source).toContain('swipeEnabled={!nestedHorizontalScrollActive}');
    expect(source).toContain('onGestureStart={onNestedHorizontalGestureStart}');
    expect(source).toContain('onGestureEnd={onNestedHorizontalGestureEnd}');
    // Pausing the pager on touch-down re-rendered every range at the start of a vertical
    // scroll and held the readout up; only an activated scrub pauses it.
    expect(source).toContain('.onStart(() => onGestureStart())');
    expect(source).not.toContain('.onTouchesDown(() => onGestureStart())');
    // onFinalize runs on release, cancel and failure alike, so the pager always gets its swipe back.
    expect(source).toMatch(/\.onFinalize\(\(\) => \{\s*setFocusIndex\(null\);\s*onGestureEnd\(\);/);
  });

  it('hides mounted off-screen scenes from native accessibility traversal', () => {
    expect(source).toContain('accessibilityElementsHidden={!active}');
    expect(source).toContain("importantForAccessibility={active ? 'auto' : 'no-hide-descendants'}");
  });
});
