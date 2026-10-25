import React, { useCallback, useEffect, useRef } from "react";
import { View } from "react-native";
import { useRouter } from "expo-router";
import { catStateScript, parseCatBridgeMessage } from "../../services/catPark";
import { useCatPark } from "./useCatPark";

/**
 * The same page the phone opens, framed from the daemon's own /cat/. The page
 * talks to this window with postMessage; only same-origin messages from the
 * frame are heard, and state goes back the same way.
 */
export function CatParkScreen() {
  const router = useRouter();
  const { view, act } = useCatPark();
  const frame = useRef<HTMLIFrameElement | null>(null);
  const ready = useRef(false);
  const latest = useRef(view);
  latest.current = view;

  const leave = useCallback(() => {
    if (router.canGoBack()) router.back();
    else router.replace("/");
  }, [router]);

  const push = useCallback(() => {
    const current = latest.current;
    const target = frame.current?.contentWindow;
    if (!ready.current || !current || !target) return;
    // The state script's payload is the message itself: same shape as native.
    const script = catStateScript(current);
    const start = script.indexOf("(") + 1;
    const message = JSON.parse(script.slice(start, script.lastIndexOf(");true;")));
    target.postMessage(message, window.location.origin);
  }, []);
  useEffect(push, [view, push]);

  useEffect(() => {
    const onWindowMessage = (event: MessageEvent) => {
      if (event.origin !== window.location.origin || event.source !== frame.current?.contentWindow) return;
      const message = parseCatBridgeMessage(event.data ? JSON.stringify(event.data) : "");
      if (!message) return;
      if (message.type === "ready") {
        ready.current = true;
        push();
      } else if (message.type === "back" || message.type === "open_brain") {
        leave();
      } else {
        act(message);
      }
    };
    window.addEventListener("message", onWindowMessage);
    return () => window.removeEventListener("message", onWindowMessage);
  }, [act, leave, push]);

  return (
    <View style={{ flex: 1, backgroundColor: "#2a211a" }}>
      {React.createElement("iframe", {
        ref: frame,
        src: "/cat/",
        title: "Cat",
        style: { border: 0, width: "100%", height: "100%", display: "block" },
      })}
    </View>
  );
}
