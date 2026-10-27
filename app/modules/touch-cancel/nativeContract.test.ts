import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const source = (relativePath: string) =>
  readFileSync(join(import.meta.dir, relativePath), "utf8");

describe("Touch cancel below the gesture-handler root", () => {
  test("Android cancels the root's children, never the root itself", () => {
    const android = source(
      "android/src/main/java/expo/modules/touchcancel/TouchCancelModule.kt",
    );

    expect(android).toContain(
      '"com.swmansion.gesturehandler.react.RNGestureHandlerRootView"',
    );
    expect(android).toContain("MotionEvent.ACTION_CANCEL");
    expect(android).toContain("view.getChildAt(index).dispatchTouchEvent(cancel)");
    expect(android).toContain("decor.post");
    expect(android).not.toContain("view.dispatchTouchEvent(cancel)");
  });

  test("the module is Android-only and optional in JS", () => {
    const config = JSON.parse(source("expo-module.config.json"));
    expect(config.platforms).toEqual(["android"]);
    expect(source("src/index.ts")).toContain("requireOptionalNativeModule");
  });

  test("the menu cancels the page's touch when its pan takes over", () => {
    const drawer = source("../../components/navigation/PrimaryDrawer.tsx");
    const beginDrag = drawer.slice(drawer.indexOf("const beginDrag"));
    expect(beginDrag.slice(0, 400)).toContain("cancelTouchesBelowGestureRoot();");
    expect(drawer).toContain("runOnJS(beginDrag)()");
  });
});
