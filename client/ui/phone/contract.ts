import { nativeComponent } from "nsh";

/**
 * `mewla.phoneLive`: the agent's screen on this phone, live (Android host:
 * phone/PhoneLiveComponent.kt over the phone module's PhoneLiveView). Every
 * mounted view shares one frame stream, run at the highest `fps` and
 * `maxWidth` any of them asks for; with none mounted the helper sends nothing.
 *
 * - `interactive`: touches drive the agent's screen (Take over) instead of being ignored (watch).
 * - `fps`: frames per second this view needs (default 12).
 * - `maxWidth`: frame width in pixels this view needs (default 540).
 *
 * Where nothing is registered (iOS, the Play build, the web, `nsh test`) it is an empty view.
 */
export interface PhoneLiveProps {
  interactive?: boolean;
  fps?: number;
  maxWidth?: number;
}

export const PhoneLiveView = nativeComponent<PhoneLiveProps>("mewla.phoneLive", {
  props: ["interactive", "fps", "maxWidth"],
});
